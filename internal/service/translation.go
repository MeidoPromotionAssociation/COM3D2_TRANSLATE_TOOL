package service

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/translation"
)

const translationFetchPageSize = 2000

type translationBatch struct {
	group   []model.Entry
	start   int
	entries []model.Entry
}

type translationBatchTask struct {
	batch   translationBatch
	attempt int
}

type indexedTranslationEntry struct {
	entry      model.Entry
	groupIndex int
}

type preparedTranslationBatch struct {
	translateEntries []indexedTranslationEntry
	immediateUpdates []model.UpdateEntryInput
	duplicateEntries map[string][]model.Entry
}

type executedTranslationBatch struct {
	processed           int
	updated             int
	skipped             int
	duplicateReuseCount int
}

type translationReuseCache struct {
	mu     sync.RWMutex
	values map[string]string
}

func newTranslationReuseCache(seed map[string]string) *translationReuseCache {
	values := make(map[string]string, len(seed))
	for key, value := range seed {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		values[key] = value
	}
	return &translationReuseCache{values: values}
}

func (c *translationReuseCache) Get(key string) (string, bool) {
	if c == nil || strings.TrimSpace(key) == "" {
		return "", false
	}
	c.mu.RLock()
	value, ok := c.values[key]
	c.mu.RUnlock()
	return value, ok
}

func (c *translationReuseCache) Put(key, value string) {
	if c == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return
	}
	c.mu.Lock()
	if c.values == nil {
		c.values = map[string]string{}
	}
	c.values[key] = value
	c.mu.Unlock()
}

func (s *Service) ListTranslators() []string {
	return sortedKeys(s.translators)
}

func (s *Service) RunTranslation(ctx context.Context, req model.TranslateRequest) (model.TranslateResult, error) {
	settings, err := s.GetSettings()
	if err != nil {
		return model.TranslateResult{}, err
	}

	name := strings.TrimSpace(req.Translator)
	if name == "" {
		name = settings.Translation.ActiveTranslator
	}
	if name == "" {
		name = "manual"
	}

	targetField := translation.NormalizeTargetField(req.TargetField)
	impl := s.translators[name]
	if impl == nil {
		return model.TranslateResult{}, fmt.Errorf("unknown translator: %s", name)
	}

	query := translateEntryQuery(req)
	total, err := s.store.CountEntriesForTranslation(query, targetField, req.AllowOverwrite)
	if err != nil {
		return model.TranslateResult{}, err
	}

	result := model.TranslateResult{
		Translator:  name,
		TargetField: targetField,
		Total:       total,
		Messages:    []string{},
	}
	runtime := translation.NewRuntime(ctx, name, targetField, total)

	if total == 0 {
		result.Messages = append(result.Messages, "No entries matched the current translation filters.")
		runtime.Complete()
		return result, nil
	}

	entries, err := s.loadTranslationEntries(query, targetField, req.AllowOverwrite)
	if err != nil {
		runtime.MarkFailed("load entries", total)
		return result, err
	}
	if ctx.Err() != nil {
		result.Messages = append(result.Messages, "Translation stopped.")
		runtime.Stopped()
		return result, nil
	}

	reuseCache, entries, reusedCount, err := s.reuseExistingTranslations(ctx, entries, targetField, runtime, &result)
	if err != nil {
		if ctx.Err() != nil {
			result.Messages = append(result.Messages, "Translation stopped.")
			runtime.Stopped()
			return result, nil
		}
		runtime.MarkFailed("reuse existing translations", total)
		return result, err
	}
	if reusedCount > 0 {
		result.Messages = append(result.Messages, fmt.Sprintf("Reused %d existing database matches before sending remaining entries to the translator.", reusedCount))
	}
	if len(entries) == 0 {
		runtime.Complete()
		return result, nil
	}

	batchSize := translation.BatchSize(settings.Translation, name)
	batches := buildTranslationBatches(entries, batchSize, translation.IsLLMTranslator(name))
	concurrency := translation.Concurrency(settings.Translation, name)
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(batches) {
		concurrency = len(batches)
	}
	if concurrency < 1 {
		concurrency = 1
	}

	var resultMu sync.Mutex
	duplicateReuseCount := 0
	retryCount := settings.Translation.RetryCount
	tasksForRound := make([]translationBatchTask, 0, len(batches))
	for _, batch := range batches {
		tasksForRound = append(tasksForRound, translationBatchTask{batch: batch})
	}

	for len(tasksForRound) > 0 {
		tasks := make(chan translationBatchTask, concurrency)
		nextRound := make([]translationBatchTask, 0)
		var nextRoundMu sync.Mutex
		var workers sync.WaitGroup

		for worker := 0; worker < concurrency; worker++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case task, ok := <-tasks:
						if !ok {
							return
						}

						stats, err := s.executeTranslationBatch(
							ctx,
							impl,
							settings.Translation,
							targetField,
							task.batch,
							translation.IsLLMTranslator(name),
							reuseCache,
							runtime,
						)
						if err != nil {
							if ctx.Err() != nil {
								return
							}

							batchPreview := previewEntry(task.batch.entries[0])
							if task.attempt < retryCount {
								emitTranslationRetryLog(ctx, name, task, retryCount, err)
								nextRoundMu.Lock()
								nextRound = append(nextRound, translationBatchTask{
									batch:   task.batch,
									attempt: task.attempt + 1,
								})
								nextRoundMu.Unlock()
								continue
							}

							runtime.MarkFailed(batchPreview, len(task.batch.entries))
							resultMu.Lock()
							result.Failed += len(task.batch.entries)
							result.Messages = append(result.Messages, fmt.Sprintf("%s: %v (after %d attempt(s))", batchPreview, err, task.attempt+1))
							resultMu.Unlock()
							continue
						}

						resultMu.Lock()
						duplicateReuseCount += stats.duplicateReuseCount
						result.Processed += stats.processed
						result.Updated += stats.updated
						result.Skipped += stats.skipped
						resultMu.Unlock()
					}
				}
			}()
		}

	dispatchLoop:
		for _, task := range tasksForRound {
			select {
			case <-ctx.Done():
				break dispatchLoop
			case tasks <- task:
			}
		}
		close(tasks)
		workers.Wait()

		if ctx.Err() != nil {
			break
		}
		tasksForRound = nextRound
	}

	if ctx.Err() != nil {
		result.Messages = append(result.Messages, "Translation stopped.")
		runtime.Stopped()
		return result, nil
	}
	if duplicateReuseCount > 0 {
		result.Messages = append(result.Messages, fmt.Sprintf("Avoided %d duplicate translator requests by reusing identical texts within this run.", duplicateReuseCount))
	}

	runtime.Complete()
	return result, nil
}

func translateEntryQuery(req model.TranslateRequest) model.EntryQuery {
	return model.EntryQuery{
		Search:           req.Search,
		SourceArc:        req.SourceArc,
		SourceFile:       req.SourceFile,
		Type:             req.Type,
		Status:           req.Status,
		UntranslatedOnly: req.UntranslatedOnly,
	}
}

func (s *Service) loadTranslationEntries(query model.EntryQuery, targetField string, allowOverwrite bool) ([]model.Entry, error) {
	items := make([]model.Entry, 0)
	for offset := 0; ; offset += translationFetchPageSize {
		page, err := s.store.ListEntriesForTranslation(query, targetField, allowOverwrite, translationFetchPageSize, offset)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		items = append(items, page...)
		if len(page) < translationFetchPageSize {
			break
		}
	}
	return items, nil
}

func buildTranslationBatches(entries []model.Entry, batchSize int, keepByFile bool) []translationBatch {
	if len(entries) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(entries)
	}

	if !keepByFile {
		batches := make([]translationBatch, 0, (len(entries)+batchSize-1)/batchSize)
		for start := 0; start < len(entries); start += batchSize {
			end := start + batchSize
			if end > len(entries) {
				end = len(entries)
			}
			part := append([]model.Entry(nil), entries[start:end]...)
			batches = append(batches, translationBatch{
				group:   part,
				start:   0,
				entries: part,
			})
		}
		return batches
	}

	batches := make([]translationBatch, 0)
	for groupStart := 0; groupStart < len(entries); {
		groupEnd := groupStart + 1
		for groupEnd < len(entries) &&
			entries[groupEnd].SourceArc == entries[groupStart].SourceArc &&
			entries[groupEnd].SourceFile == entries[groupStart].SourceFile {
			groupEnd++
		}

		group := append([]model.Entry(nil), entries[groupStart:groupEnd]...)
		for start := 0; start < len(group); start += batchSize {
			end := start + batchSize
			if end > len(group) {
				end = len(group)
			}
			batches = append(batches, translationBatch{
				group:   group,
				start:   start,
				entries: append([]model.Entry(nil), group[start:end]...),
			})
		}
		groupStart = groupEnd
	}
	return batches
}

func buildTranslationItems(batch translationBatch, includeContext bool) []translation.Item {
	items := make([]translation.Item, 0, len(batch.entries))
	if !includeContext {
		for _, entry := range batch.entries {
			items = append(items, translation.Item{
				ID:             entry.ID,
				Type:           entry.Type,
				VoiceID:        entry.VoiceID,
				Role:           entry.Role,
				SourceArc:      entry.SourceArc,
				SourceFile:     entry.SourceFile,
				SourceText:     entry.SourceText,
				TranslatedText: entry.TranslatedText,
				PolishedText:   entry.PolishedText,
			})
		}
		return items
	}

	for index, entry := range batch.entries {
		groupIndex := batch.start + index
		previousText := ""
		if groupIndex > 0 {
			previousText = batch.group[groupIndex-1].SourceText
		}
		nextText := ""
		if groupIndex+1 < len(batch.group) {
			nextText = batch.group[groupIndex+1].SourceText
		}

		items = append(items, translation.Item{
			ID:                 entry.ID,
			Type:               entry.Type,
			VoiceID:            entry.VoiceID,
			Role:               entry.Role,
			SourceArc:          entry.SourceArc,
			SourceFile:         entry.SourceFile,
			SourceText:         entry.SourceText,
			TranslatedText:     entry.TranslatedText,
			PolishedText:       entry.PolishedText,
			PreviousSourceText: previousText,
			NextSourceText:     nextText,
		})
	}
	return items
}

func buildTranslationItemsFromIndexed(group []model.Entry, entries []indexedTranslationEntry, includeContext bool) []translation.Item {
	items := make([]translation.Item, 0, len(entries))
	for _, indexed := range entries {
		entry := indexed.entry
		item := translation.Item{
			ID:             entry.ID,
			Type:           entry.Type,
			VoiceID:        entry.VoiceID,
			Role:           entry.Role,
			SourceArc:      entry.SourceArc,
			SourceFile:     entry.SourceFile,
			SourceText:     entry.SourceText,
			TranslatedText: entry.TranslatedText,
			PolishedText:   entry.PolishedText,
		}
		if includeContext {
			if indexed.groupIndex > 0 {
				item.PreviousSourceText = group[indexed.groupIndex-1].SourceText
			}
			if indexed.groupIndex+1 < len(group) {
				item.NextSourceText = group[indexed.groupIndex+1].SourceText
			}
		}
		items = append(items, item)
	}
	return items
}

func (s *Service) executeTranslationBatch(
	ctx context.Context,
	impl translation.Translator,
	settings model.TranslationSettings,
	targetField string,
	batch translationBatch,
	includeContext bool,
	reuseCache *translationReuseCache,
	runtime *translation.Runtime,
) (executedTranslationBatch, error) {
	if len(batch.entries) == 0 {
		return executedTranslationBatch{}, nil
	}
	if ctx.Err() != nil {
		return executedTranslationBatch{}, ctx.Err()
	}

	batchPreview := previewEntry(batch.entries[0])
	runtime.MarkRunning(batchPreview)

	plan := prepareTranslationBatch(batch, targetField, reuseCache)
	updateInputs := append([]model.UpdateEntryInput(nil), plan.immediateUpdates...)
	skippedIDs := make(map[int64]bool)
	cacheUpdates := make(map[string]string)

	if len(plan.translateEntries) > 0 {
		items := buildTranslationItemsFromIndexed(batch.group, plan.translateEntries, includeContext)
		translations, err := impl.Translate(ctx, translation.Request{
			Settings:    settings,
			Items:       items,
			TargetField: targetField,
		})
		if err != nil {
			return executedTranslationBatch{}, err
		}

		baseEntries := make([]model.Entry, 0, len(plan.translateEntries))
		for _, indexed := range plan.translateEntries {
			baseEntries = append(baseEntries, indexed.entry)
		}

		translatedUpdates, translatedSkipped, err := buildTranslationUpdates(baseEntries, translations, targetField)
		if err != nil {
			return executedTranslationBatch{}, err
		}
		updateInputs = append(updateInputs, translatedUpdates...)
		for id := range translatedSkipped {
			skippedIDs[id] = true
		}

		textByID := make(map[int64]string, len(translations))
		for _, translated := range translations {
			textByID[translated.ID] = translated.Text
		}

		for _, indexed := range plan.translateEntries {
			key := translationReuseKey(indexed.entry, targetField)
			if key == "" {
				continue
			}

			text := textByID[indexed.entry.ID]
			if strings.TrimSpace(text) != "" && !translatedSkipped[indexed.entry.ID] {
				cacheUpdates[key] = text
				for _, duplicate := range plan.duplicateEntries[key] {
					updateInputs = append(updateInputs, buildTranslationUpdateInput(duplicate, targetField, text))
				}
				continue
			}

			for _, duplicate := range plan.duplicateEntries[key] {
				skippedIDs[duplicate.ID] = true
			}
		}
	}

	if ctx.Err() != nil {
		return executedTranslationBatch{}, ctx.Err()
	}
	if _, err := s.store.ApplyEntryUpdates(updateInputs); err != nil {
		return executedTranslationBatch{}, err
	}
	for key, value := range cacheUpdates {
		reuseCache.Put(key, value)
	}

	stats := executedTranslationBatch{
		duplicateReuseCount: len(plan.immediateUpdates) + duplicateEntryCount(plan.duplicateEntries),
	}
	for _, entry := range batch.entries {
		stats.processed++
		if skippedIDs[entry.ID] {
			stats.skipped++
			runtime.MarkSkipped(previewEntry(entry))
			continue
		}

		stats.updated++
		runtime.MarkUpdated(previewEntry(entry))
	}
	return stats, nil
}

func (s *Service) reuseExistingTranslations(
	ctx context.Context,
	entries []model.Entry,
	targetField string,
	runtime *translation.Runtime,
	result *model.TranslateResult,
) (*translationReuseCache, []model.Entry, int, error) {
	cacheSeed, err := s.store.FindReusableTargetTexts(entries, targetField)
	if err != nil {
		return nil, nil, 0, err
	}
	reuseCache := newTranslationReuseCache(cacheSeed)

	updateInputs := make([]model.UpdateEntryInput, 0)
	reusedEntries := make([]model.Entry, 0)
	remaining := make([]model.Entry, 0, len(entries))

	for _, entry := range entries {
		if ctx.Err() != nil {
			return reuseCache, nil, 0, ctx.Err()
		}
		if !canReuseExistingTranslation(entry, targetField) {
			remaining = append(remaining, entry)
			continue
		}

		key := translationReuseKey(entry, targetField)
		text, ok := reuseCache.Get(key)
		if !ok || strings.TrimSpace(text) == "" {
			remaining = append(remaining, entry)
			continue
		}

		updateInputs = append(updateInputs, buildTranslationUpdateInput(entry, targetField, text))
		reusedEntries = append(reusedEntries, entry)
	}

	if len(updateInputs) == 0 {
		return reuseCache, entries, 0, nil
	}
	if _, err := s.store.ApplyEntryUpdates(updateInputs); err != nil {
		return reuseCache, nil, 0, err
	}

	for _, entry := range reusedEntries {
		runtime.MarkUpdated(previewEntry(entry))
	}
	result.Processed += len(reusedEntries)
	result.Updated += len(reusedEntries)
	return reuseCache, remaining, len(reusedEntries), nil
}

func prepareTranslationBatch(batch translationBatch, targetField string, reuseCache *translationReuseCache) preparedTranslationBatch {
	plan := preparedTranslationBatch{
		translateEntries: make([]indexedTranslationEntry, 0, len(batch.entries)),
		immediateUpdates: make([]model.UpdateEntryInput, 0),
		duplicateEntries: make(map[string][]model.Entry),
	}
	firstByKey := make(map[string]int)

	for index, entry := range batch.entries {
		key := translationReuseKey(entry, targetField)
		if !canReuseExistingTranslation(entry, targetField) || key == "" {
			plan.translateEntries = append(plan.translateEntries, indexedTranslationEntry{
				entry:      entry,
				groupIndex: batch.start + index,
			})
			continue
		}

		if cached, ok := reuseCache.Get(key); ok && strings.TrimSpace(cached) != "" {
			plan.immediateUpdates = append(plan.immediateUpdates, buildTranslationUpdateInput(entry, targetField, cached))
			continue
		}

		if _, exists := firstByKey[key]; exists {
			plan.duplicateEntries[key] = append(plan.duplicateEntries[key], entry)
			continue
		}

		firstByKey[key] = len(plan.translateEntries)
		plan.translateEntries = append(plan.translateEntries, indexedTranslationEntry{
			entry:      entry,
			groupIndex: batch.start + index,
		})
	}

	return plan
}

func duplicateEntryCount(items map[string][]model.Entry) int {
	total := 0
	for _, group := range items {
		total += len(group)
	}
	return total
}

func buildTranslationUpdateInput(entry model.Entry, targetField, text string) model.UpdateEntryInput {
	translated := entry.TranslatedText
	polished := entry.PolishedText
	if targetField == "polished" {
		polished = text
	} else {
		translated = text
	}

	return model.UpdateEntryInput{
		ID:               entry.ID,
		TranslatedText:   translated,
		PolishedText:     polished,
		TranslatorStatus: nextAutomaticStatus(entry, targetField, translated, polished),
	}
}

func canReuseExistingTranslation(entry model.Entry, targetField string) bool {
	if targetField == "polished" {
		return strings.TrimSpace(entry.PolishedText) == "" && strings.TrimSpace(entry.TranslatedText) != ""
	}
	return strings.TrimSpace(entry.TranslatedText) == ""
}

func translationReuseKey(entry model.Entry, targetField string) string {
	if strings.TrimSpace(entry.SourceText) == "" {
		return ""
	}
	if targetField == "polished" {
		if strings.TrimSpace(entry.TranslatedText) == "" {
			return ""
		}
		return entry.SourceText + "\x00" + entry.TranslatedText
	}
	return entry.SourceText
}

func buildTranslationUpdates(entries []model.Entry, results []translation.Result, targetField string) ([]model.UpdateEntryInput, map[int64]bool, error) {
	byID := make(map[int64]string, len(results))
	for _, result := range results {
		byID[result.ID] = result.Text
	}

	updateInputs := make([]model.UpdateEntryInput, 0, len(entries))
	skipped := make(map[int64]bool)

	for _, entry := range entries {
		text, ok := byID[entry.ID]
		if !ok {
			return nil, nil, fmt.Errorf("translator did not return a result for entry %d", entry.ID)
		}
		if strings.TrimSpace(text) == "" {
			skipped[entry.ID] = true
			continue
		}

		translated := entry.TranslatedText
		polished := entry.PolishedText
		if targetField == "polished" {
			polished = text
		} else {
			translated = text
		}

		updateInputs = append(updateInputs, model.UpdateEntryInput{
			ID:               entry.ID,
			TranslatedText:   translated,
			PolishedText:     polished,
			TranslatorStatus: nextAutomaticStatus(entry, targetField, translated, polished),
		})
	}
	return updateInputs, skipped, nil
}

func nextAutomaticStatus(entry model.Entry, targetField, translated, polished string) string {
	if targetField == "translated" && entry.PolishedText != "" {
		if entry.TranslatorStatus == "reviewed" {
			return "reviewed"
		}
		return "polished"
	}
	if polished != "" {
		return "polished"
	}
	if translated != "" {
		return "translated"
	}
	return "new"
}

func previewEntry(entry model.Entry) string {
	text := strings.TrimSpace(entry.SourceText)
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 72 {
		text = text[:72] + "..."
	}
	if entry.Role != "" {
		return entry.SourceFile + " [" + entry.Role + "] " + text
	}
	return entry.SourceFile + " " + text
}

func emitTranslationRetryLog(ctx context.Context, translatorName string, task translationBatchTask, retryCount int, err error) {
	if err == nil || len(task.batch.entries) == 0 {
		return
	}

	content := strings.TrimSpace(fmt.Sprintf(
		"Batch: %s (%d entries)\nAttempt: %d/%d\nQueued Retry: %d/%d\nError: %v",
		previewEntry(task.batch.entries[0]),
		len(task.batch.entries),
		task.attempt+1,
		retryCount+1,
		task.attempt+1,
		retryCount,
		err,
	))

	translation.EmitLog(ctx, model.TranslateLog{
		Translator: translatorName,
		Kind:       "retry",
		Title:      "Translation Retry",
		Content:    content,
	})
}
