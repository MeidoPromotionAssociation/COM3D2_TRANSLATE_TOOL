package service

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/textutil"
	"COM3D2TranslateTool/internal/translation"
)

const sourceRecognitionMessageLimit = 100

type preparedSourceRecognitionEntry struct {
	entry     model.Entry
	preview   string
	audioPath string
	cleanup   func()
}

func (s *Service) RunSourceRecognition(ctx context.Context, req model.SourceRecognitionRequest) (model.SourceRecognitionResult, error) {
	settings, err := s.GetSettings()
	if err != nil {
		return model.SourceRecognitionResult{}, err
	}

	query := model.EntryQuery{
		Search:           req.Search,
		SourceArc:        req.SourceArc,
		SourceFile:       req.SourceFile,
		Type:             req.Type,
		Status:           req.Status,
		UntranslatedOnly: req.UntranslatedOnly,
	}

	total, err := s.store.CountEntriesForSourceRecognition(query, req.AllowOverwrite)
	if err != nil {
		return model.SourceRecognitionResult{}, err
	}

	provider := translation.ASRTranslatorName()
	result := model.SourceRecognitionResult{
		Provider: provider,
		Total:    total,
		Messages: []string{},
	}
	runtime := translation.NewRuntime(ctx, provider, "source_text", total)

	if total == 0 {
		result.Messages = append(result.Messages, "No playvoice_notext entries matched the current filters.")
		runtime.Complete()
		return result, nil
	}

	entries, err := s.loadSourceRecognitionEntries(query, req.AllowOverwrite)
	if err != nil {
		runtime.MarkFailed("load entries", total)
		return result, err
	}
	if ctx.Err() != nil {
		result.Messages = append(result.Messages, "Source recognition stopped.")
		runtime.Stopped()
		return result, nil
	}

	batchSize := settings.Translation.ASR.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	batches := buildSourceRecognitionBatches(entries, batchSize)

	concurrency := settings.Translation.ASR.Concurrency
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
	var arcCacheMu sync.Mutex
	var batchModeMu sync.RWMutex
	var batchModeDisabledOnce sync.Once
	arcCache := map[string]model.ArcFile{}
	tasks := make(chan []model.Entry, concurrency)
	var workers sync.WaitGroup

	resolveArc := func(filename string) (model.ArcFile, error) {
		arcCacheMu.Lock()
		cached, ok := arcCache[filename]
		arcCacheMu.Unlock()
		if ok {
			return cached, nil
		}

		record, err := s.store.GetArcByFilename(filename)
		if err != nil {
			return model.ArcFile{}, err
		}

		arcCacheMu.Lock()
		arcCache[filename] = record
		arcCacheMu.Unlock()
		return record, nil
	}

	batchModeEnabled := batchSize > 1
	canUseBatch := func() bool {
		batchModeMu.RLock()
		enabled := batchModeEnabled
		batchModeMu.RUnlock()
		return enabled
	}
	disableBatchMode := func(err error) {
		batchModeDisabledOnce.Do(func() {
			batchModeMu.Lock()
			batchModeEnabled = false
			batchModeMu.Unlock()

			if err == nil {
				return
			}
			resultMu.Lock()
			appendSourceRecognitionMessage(&result, fmt.Sprintf("ASR batch mode disabled for the rest of this run after a batch request failed: %v", err))
			resultMu.Unlock()
		})
	}

	for worker := 0; worker < concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case batch, ok := <-tasks:
					if !ok {
						return
					}
					s.processSourceRecognitionBatch(ctx, settings, batch, resolveArc, canUseBatch, disableBatchMode, runtime, &result, &resultMu)
				}
			}
		}()
	}

dispatchLoop:
	for _, batch := range batches {
		select {
		case <-ctx.Done():
			break dispatchLoop
		case tasks <- batch:
		}
	}
	close(tasks)
	workers.Wait()

	if ctx.Err() != nil {
		result.Messages = append(result.Messages, "Source recognition stopped.")
		runtime.Stopped()
		return result, nil
	}

	runtime.Complete()
	return result, nil
}

func buildSourceRecognitionBatches(entries []model.Entry, batchSize int) [][]model.Entry {
	if len(entries) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 1
	}

	batches := make([][]model.Entry, 0, (len(entries)+batchSize-1)/batchSize)
	for start := 0; start < len(entries); start += batchSize {
		end := start + batchSize
		if end > len(entries) {
			end = len(entries)
		}
		batch := append([]model.Entry(nil), entries[start:end]...)
		batches = append(batches, batch)
	}
	return batches
}

func (s *Service) processSourceRecognitionBatch(
	ctx context.Context,
	settings model.Settings,
	batch []model.Entry,
	resolveArc func(filename string) (model.ArcFile, error),
	canUseBatch func() bool,
	disableBatchMode func(err error),
	runtime *translation.Runtime,
	result *model.SourceRecognitionResult,
	resultMu *sync.Mutex,
) {
	if len(batch) == 0 || ctx.Err() != nil {
		return
	}

	runtime.MarkRunning(previewSourceRecognitionBatch(batch))

	prepared := make([]preparedSourceRecognitionEntry, 0, len(batch))
	for _, entry := range batch {
		if ctx.Err() != nil {
			cleanupPreparedSourceRecognitionEntries(prepared)
			return
		}

		preview := previewSourceRecognitionEntry(entry)
		record, err := resolveArc(entry.SourceArc)
		if err != nil {
			translation.EmitASRErrorLog(ctx, entry, "resolve arc", err)
			runtime.MarkFailed(preview, 1)
			resultMu.Lock()
			result.Failed++
			appendSourceRecognitionMessage(result, fmt.Sprintf("%s: %v", preview, err))
			resultMu.Unlock()
			continue
		}

		audioPath, cleanup, err := s.arcManager.ExtractVoiceFile(settings.WorkDir, record, entry.VoiceID)
		if err != nil {
			translation.EmitASRErrorLog(ctx, entry, "extract voice file", err)
			runtime.MarkFailed(preview, 1)
			resultMu.Lock()
			result.Failed++
			appendSourceRecognitionMessage(result, fmt.Sprintf("%s: %v", preview, err))
			resultMu.Unlock()
			continue
		}

		prepared = append(prepared, preparedSourceRecognitionEntry{
			entry:     entry,
			preview:   preview,
			audioPath: audioPath,
			cleanup:   cleanup,
		})
	}
	defer cleanupPreparedSourceRecognitionEntries(prepared)

	if len(prepared) == 0 {
		return
	}

	if len(prepared) == 1 || settings.Translation.ASR.BatchSize <= 1 || (canUseBatch != nil && !canUseBatch()) {
		s.processPreparedSourceRecognitionEntriesIndividually(ctx, settings, prepared, runtime, result, resultMu)
		return
	}

	audioPaths := make([]string, 0, len(prepared))
	batchEntries := make([]model.Entry, 0, len(prepared))
	for _, item := range prepared {
		audioPaths = append(audioPaths, item.audioPath)
		batchEntries = append(batchEntries, item.entry)
	}

	translation.EmitASRBatchRequestLog(ctx, batchEndpoint(settings.Translation.ASR.BaseURL), batchEntries, audioPaths, settings.Translation.ASR)
	results, err := translation.TranscribeAudioFiles(ctx, settings.Translation.Proxy, settings.Translation.ASR, audioPaths)
	if err != nil {
		if disableBatchMode != nil {
			disableBatchMode(err)
		}
		translation.EmitASRBatchErrorLog(ctx, batchEntries, "transcribe audio batch", err)
		resultMu.Lock()
		appendSourceRecognitionMessage(result, fmt.Sprintf("%s: batch request failed, falling back to single-file ASR: %v", previewSourceRecognitionBatch(batchEntries), err))
		resultMu.Unlock()
		s.processPreparedSourceRecognitionEntriesIndividually(ctx, settings, prepared, runtime, result, resultMu)
		return
	}

	translation.EmitASRBatchResponseLog(ctx, batchEntries, results)
	for index, item := range prepared {
		if ctx.Err() != nil {
			return
		}
		s.applySourceRecognitionResult(ctx, item.entry, item.preview, results[index], runtime, result, resultMu)
	}
}

func (s *Service) processPreparedSourceRecognitionEntriesIndividually(
	ctx context.Context,
	settings model.Settings,
	prepared []preparedSourceRecognitionEntry,
	runtime *translation.Runtime,
	result *model.SourceRecognitionResult,
	resultMu *sync.Mutex,
) {
	for _, item := range prepared {
		if ctx.Err() != nil {
			return
		}

		translation.EmitASRRequestLog(ctx, singleEndpoint(settings.Translation.ASR.BaseURL), item.entry, item.audioPath, settings.Translation.ASR)
		asrResult, err := translation.TranscribeAudioFile(ctx, settings.Translation.Proxy, settings.Translation.ASR, item.audioPath)
		if err != nil {
			translation.EmitASRErrorLog(ctx, item.entry, "transcribe audio", err)
			if ctx.Err() == nil {
				runtime.MarkFailed(item.preview, 1)
				resultMu.Lock()
				result.Failed++
				appendSourceRecognitionMessage(result, fmt.Sprintf("%s: %v", item.preview, err))
				resultMu.Unlock()
			}
			continue
		}

		translation.EmitASRResponseLog(ctx, item.entry, asrResult)
		s.applySourceRecognitionResult(ctx, item.entry, item.preview, asrResult, runtime, result, resultMu)
	}
}

func (s *Service) applySourceRecognitionResult(
	ctx context.Context,
	entry model.Entry,
	preview string,
	asrResult translation.AudioTranscriptionResult,
	runtime *translation.Runtime,
	result *model.SourceRecognitionResult,
	resultMu *sync.Mutex,
) {
	sourceText := textutil.NormalizeSourceText(asrResult.Text)
	if sourceText == "" {
		runtime.MarkSkipped(preview)
		resultMu.Lock()
		result.Processed++
		result.Skipped++
		appendSourceRecognitionMessage(result, fmt.Sprintf("%s: ASR returned an empty transcription.", preview))
		resultMu.Unlock()
		return
	}

	if err := s.store.UpdateEntrySourceText(entry.ID, sourceText); err != nil {
		translation.EmitASRErrorLog(ctx, entry, "save source text", err)
		runtime.MarkFailed(preview, 1)
		resultMu.Lock()
		result.Failed++
		appendSourceRecognitionMessage(result, fmt.Sprintf("%s: %v", preview, err))
		resultMu.Unlock()
		return
	}

	runtime.MarkUpdated(preview)
	resultMu.Lock()
	result.Processed++
	result.Updated++
	resultMu.Unlock()
}

func cleanupPreparedSourceRecognitionEntries(prepared []preparedSourceRecognitionEntry) {
	for _, item := range prepared {
		if item.cleanup != nil {
			item.cleanup()
		}
	}
}

func (s *Service) loadSourceRecognitionEntries(query model.EntryQuery, allowOverwrite bool) ([]model.Entry, error) {
	items := make([]model.Entry, 0)
	for offset := 0; ; offset += translationFetchPageSize {
		page, err := s.store.ListEntriesForSourceRecognition(query, allowOverwrite, translationFetchPageSize, offset)
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

func previewSourceRecognitionEntry(entry model.Entry) string {
	parts := []string{entry.SourceFile}
	if trimmed := strings.TrimSpace(entry.VoiceID); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if trimmed := strings.TrimSpace(entry.Role); trimmed != "" {
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, " / ")
}

func previewSourceRecognitionBatch(entries []model.Entry) string {
	if len(entries) == 0 {
		return "source recognition batch"
	}
	if len(entries) == 1 {
		return previewSourceRecognitionEntry(entries[0])
	}
	return fmt.Sprintf("%s (+%d more)", previewSourceRecognitionEntry(entries[0]), len(entries)-1)
}

func appendSourceRecognitionMessage(result *model.SourceRecognitionResult, message string) {
	if result == nil || strings.TrimSpace(message) == "" {
		return
	}
	if len(result.Messages) >= sourceRecognitionMessageLimit {
		return
	}
	result.Messages = append(result.Messages, message)
}

func singleEndpoint(raw string) string {
	endpoint := strings.TrimSpace(raw)
	endpoint = strings.TrimSuffix(endpoint, "/batch")
	return endpoint
}

func batchEndpoint(raw string) string {
	base := singleEndpoint(raw)
	if base == "" {
		return ""
	}
	return strings.TrimSuffix(base, "/") + "/batch"
}
