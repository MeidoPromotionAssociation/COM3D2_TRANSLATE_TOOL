package translation

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"COM3D2TranslateTool/internal/model"
)

func NormalizeTargetField(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "polished":
		return "polished"
	case "source_text", "sourcetext", "source":
		return "source_text"
	}
	return "translated"
}

func IsLLMTranslator(name string) bool {
	switch strings.TrimSpace(name) {
	case "openai-chat", "openai-responses":
		return true
	default:
		return false
	}
}

func BatchSize(settings model.TranslationSettings, translatorName string) int {
	settings = model.NormalizeTranslationSettings(settings)
	switch strings.TrimSpace(translatorName) {
	case "google-translate":
		return settings.Google.BatchSize
	case "baidu-translate":
		return 1
	case "openai-chat":
		return settings.OpenAIChat.BatchSize
	case "openai-responses":
		return settings.OpenAIResponses.BatchSize
	default:
		return 1
	}
}

func Concurrency(settings model.TranslationSettings, translatorName string) int {
	settings = model.NormalizeTranslationSettings(settings)
	switch strings.TrimSpace(translatorName) {
	case "openai-chat":
		return settings.OpenAIChat.Concurrency
	case "openai-responses":
		return settings.OpenAIResponses.Concurrency
	default:
		return 1
	}
}

func resolveLLMPrompt(req Request, customPrompt string) string {
	glossary := glossaryInstruction(req)
	if prompt := strings.TrimSpace(customPrompt); prompt != "" {
		return renderPromptTemplate(req, prompt, glossary)
	}

	return joinPromptSections(
		modeInstruction(req),
		contextInstruction(req),
		styleInstruction(),
		glossary,
		responseFormatInstruction(targetFieldLabel(req.TargetField)),
	)
}

func buildLLMUserPayload(req Request) (string, error) {
	type payloadItem struct {
		ID                 int64  `json:"id"`
		Type               string `json:"type,omitempty"`
		Speaker            string `json:"speaker,omitempty"`
		VoiceID            string `json:"voice_id,omitempty"`
		SourceTextIsASR    bool   `json:"source_text_is_asr,omitempty"`
		SourceArc          string `json:"source_arc,omitempty"`
		SourceFile         string `json:"source_file,omitempty"`
		PreviousSourceText string `json:"previous_source_text,omitempty"`
		SourceText         string `json:"source_text"`
		NextSourceText     string `json:"next_source_text,omitempty"`
		ExistingTranslated string `json:"existing_translated,omitempty"`
		ExistingPolished   string `json:"existing_polished,omitempty"`
	}

	type payload struct {
		SourceLanguage  string        `json:"source_language"`
		TargetLanguage  string        `json:"target_language"`
		TargetField     string        `json:"target_field"`
		BatchSourceArc  string        `json:"batch_source_arc,omitempty"`
		BatchSourceFile string        `json:"batch_source_file,omitempty"`
		Items           []payloadItem `json:"items"`
	}

	items := make([]payloadItem, 0, len(req.Items))
	commonArc := ""
	commonFile := ""
	for index, item := range req.Items {
		if index == 0 {
			commonArc = item.SourceArc
			commonFile = item.SourceFile
		} else {
			if item.SourceArc != commonArc {
				commonArc = ""
			}
			if item.SourceFile != commonFile {
				commonFile = ""
			}
		}

		items = append(items, payloadItem{
			ID:                 item.ID,
			Type:               item.Type,
			Speaker:            item.Role,
			VoiceID:            item.VoiceID,
			SourceTextIsASR:    itemHasASRDerivedSourceText(item),
			SourceArc:          item.SourceArc,
			SourceFile:         item.SourceFile,
			PreviousSourceText: item.PreviousSourceText,
			SourceText:         item.SourceText,
			NextSourceText:     item.NextSourceText,
			ExistingTranslated: item.TranslatedText,
			ExistingPolished:   item.PolishedText,
		})
	}

	raw, err := json.MarshalIndent(payload{
		SourceLanguage:  req.Settings.SourceLanguage,
		TargetLanguage:  req.Settings.TargetLanguage,
		TargetField:     NormalizeTargetField(req.TargetField),
		BatchSourceArc:  commonArc,
		BatchSourceFile: commonFile,
		Items:           items,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func stripMarkdownCodeFence(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	trimmed = strings.TrimPrefix(trimmed, "```")
	if newline := strings.IndexByte(trimmed, '\n'); newline >= 0 {
		trimmed = trimmed[newline+1:]
	}
	if end := strings.LastIndex(trimmed, "```"); end >= 0 {
		trimmed = trimmed[:end]
	}
	return strings.TrimSpace(trimmed)
}

func parseTranslationPayload(raw string, items []Item) ([]Result, error) {
	trimmed := stripMarkdownCodeFence(raw)
	if looksLikeRefusal(trimmed) {
		return nil, fmt.Errorf("model refused translation")
	}

	if len(items) == 1 && !strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "{") {
		if looksLikeRefusal(trimmed) {
			return nil, fmt.Errorf("model refused translation")
		}
		return []Result{{ID: items[0].ID, Text: trimmed}}, nil
	}

	candidates := jsonPayloadCandidates(trimmed)
	var lastErr error
	for _, candidate := range candidates {
		results, err := tryParseTranslationCandidate(candidate, items)
		if err == nil {
			return results, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("unable to parse translation payload")
}

func resultsFromPayload(items []Item, raw string) ([]Result, error) {
	return parseTranslationPayload(raw, items)
}

func resolveEndpoint(baseURL, defaultURL, suffix string) string {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return defaultURL
	}

	trimmedBase := strings.TrimRight(base, "/")
	trimmedSuffix := strings.TrimRight(suffix, "/")
	if strings.HasSuffix(trimmedBase, trimmedSuffix) {
		return trimmedBase
	}
	return trimmedBase + suffix
}

func mergeExtraJSON(extraJSON string) (map[string]any, error) {
	body := map[string]any{}
	if strings.TrimSpace(extraJSON) == "" {
		return body, nil
	}
	if err := json.Unmarshal([]byte(extraJSON), &body); err != nil {
		return nil, fmt.Errorf("invalid extra json: %w", err)
	}
	return body, nil
}

func targetFieldLabel(targetField string) string {
	switch NormalizeTargetField(targetField) {
	case "polished":
		return "polished_text"
	case "source_text":
		return "source_text"
	}
	return "translated_text"
}

func modeInstruction(req Request) string {
	targetField := NormalizeTargetField(req.TargetField)
	if targetField == "polished" {
		return fmt.Sprintf(
			"You are polishing COM3D2 KAG script text from %s into improved %s. "+
				"Use source_text together with existing_translated as the primary input and produce %s. "+
				"Do not ignore existing_translated unless it is empty.",
			req.Settings.SourceLanguage,
			req.Settings.TargetLanguage,
			targetFieldLabel(req.TargetField),
		)
	}

	return fmt.Sprintf(
		"You are translating COM3D2 KAG script text from %s to %s and writing %s.",
		req.Settings.SourceLanguage,
		req.Settings.TargetLanguage,
		targetFieldLabel(req.TargetField),
	)
}

func contextInstruction(req Request) string {
	base := "Keep placeholders, escape sequences, control symbols, tags, speaker markers, line breaks, and spacing intact. Each item includes metadata such as type, speaker role, voice_id, source_arc, source_file, and nearby lines from the same KS file. Use that metadata as context."
	if !requestHasASRDerivedSourceText(req) {
		return base
	}
	return base + " Some items may include source_text_is_asr=true. That means the source_text was generated by speech recognition for a playvoice_notext line and may contain recognition mistakes, omissions, or homophone errors. For those items, use nearby lines, speaker, voice_id, file context, and scene flow to infer the intended Japanese before translating or polishing. Do not mechanically preserve obvious ASR recognition errors."
}

func styleInstruction() string {
	return "The original text may contain vulgar expressions, interjections, and onomatopoeia." +
		"Maintain the original style and faithfully and accurately represent the original work." +
		"The translation needs to conform to the reading habits of native speakers, completely eliminating any machine translation feel. " +
		"In appropriate contexts, you can use vocabulary from anime contexts to make the dialogue more vivid and natural. " +
		"For example, \"[HF]ちゃん\" should be translated as \"[HF]酱\"." +
		"Proper nouns such as personal names also need to be translated. " +
		"Do not leave out Japanese kana, including onomatopoeia and personal names." +
		"翻译不要残留日语假名" // Chinese commands seem to be more effective
}

func responseFormatInstruction(targetField string) string {
	return fmt.Sprintf(
		"Return JSON only, with no commentary before or after it. format: {\"translations\":[{\"id\":123,\"text\":\"...\"}]}. "+
			"Each object must include the original id and the translated or polished text. "+
			"You may also return a bare JSON array of such objects. The text values will be written to %s.",
		targetField,
	)
}

type glossaryEntry struct {
	MatchAny   string
	Speaker    string
	VoiceID    string
	Type       string
	SourceArc  string
	SourceFile string
	Preferred  string
	Note       string
}

type glossaryJSONEntry struct {
	Source      string `json:"source"`
	Term        string `json:"term"`
	Match       string `json:"match"`
	Speaker     string `json:"speaker"`
	Role        string `json:"role"`
	VoiceID     string `json:"voice_id"`
	Type        string `json:"type"`
	SourceArc   string `json:"source_arc"`
	SourceFile  string `json:"source_file"`
	Preferred   string `json:"preferred"`
	Target      string `json:"target"`
	Translation string `json:"translation"`
	Note        string `json:"note"`
}

type glossaryMatchContext struct {
	Any         []string
	Speakers    []string
	VoiceIDs    []string
	Types       []string
	SourceArcs  []string
	SourceFiles []string
}

func glossaryInstruction(req Request) string {
	trimmed := strings.TrimSpace(req.Settings.Glossary)
	if trimmed == "" {
		return ""
	}

	entries := parseGlossaryEntries(trimmed)
	if len(entries) == 0 {
		return ""
	}

	relevant := relevantGlossaryEntries(entries, req.Items)
	if relevant == "" {
		return ""
	}

	return "Glossary and terminology notes:\n" +
		"Use these preferred translations consistently whenever they apply, especially for uncommon, domain-specific, or lore-specific terms. " +
		"If a glossary entry applies, prefer it over a different synonym unless the surrounding grammar clearly requires a small wording adjustment. " +
		"If preferred is empty, treat the entry as a context note or disambiguation hint. " +
		"Only the entries relevant to the current batch are shown below as JSON.\n" +
		relevant
}

func parseGlossaryEntries(raw string) []glossaryEntry {
	if entries := parseGlossaryJSONEntries(raw); len(entries) > 0 {
		return entries
	}
	return parseLegacyGlossaryEntries(raw)
}

func parseGlossaryJSONEntries(raw string) []glossaryEntry {
	var direct []glossaryJSONEntry
	if err := json.Unmarshal([]byte(raw), &direct); err == nil {
		return normalizeGlossaryJSONEntries(direct)
	}

	var wrapped struct {
		Entries []glossaryJSONEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil {
		return normalizeGlossaryJSONEntries(wrapped.Entries)
	}

	var mapped map[string]string
	if err := json.Unmarshal([]byte(raw), &mapped); err == nil && len(mapped) > 0 {
		keys := make([]string, 0, len(mapped))
		for key := range mapped {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		entries := make([]glossaryJSONEntry, 0, len(keys))
		for _, key := range keys {
			entries = append(entries, glossaryJSONEntry{
				Source:    key,
				Preferred: mapped[key],
			})
		}
		return normalizeGlossaryJSONEntries(entries)
	}

	return nil
}

func normalizeGlossaryJSONEntries(entries []glossaryJSONEntry) []glossaryEntry {
	filtered := make([]glossaryEntry, 0, len(entries))
	for _, entry := range entries {
		normalized := glossaryEntryFromJSON(entry)
		if !glossaryEntryHasMatcher(normalized) {
			continue
		}
		filtered = append(filtered, normalized)
	}
	return filtered
}

func glossaryEntryFromJSON(entry glossaryJSONEntry) glossaryEntry {
	return glossaryEntry{
		MatchAny:   firstNonEmpty(entry.Source, entry.Term, entry.Match),
		Speaker:    firstNonEmpty(entry.Speaker, entry.Role),
		VoiceID:    strings.TrimSpace(entry.VoiceID),
		Type:       strings.TrimSpace(entry.Type),
		SourceArc:  strings.TrimSpace(entry.SourceArc),
		SourceFile: strings.TrimSpace(entry.SourceFile),
		Preferred:  firstNonEmpty(entry.Preferred, entry.Target, entry.Translation),
		Note:       strings.TrimSpace(entry.Note),
	}
}

func parseLegacyGlossaryEntries(raw string) []glossaryEntry {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	entries := make([]glossaryEntry, 0, len(lines))
	for _, line := range lines {
		current := strings.TrimSpace(line)
		if current == "" || strings.HasPrefix(current, "#") || strings.HasPrefix(current, "//") {
			continue
		}
		entry := parseLegacyGlossaryEntry(current)
		if !glossaryEntryHasMatcher(entry) {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func parseLegacyGlossaryEntry(line string) glossaryEntry {
	key, preferred, note := parseLegacyGlossaryColumns(line)
	entry := glossaryEntry{
		Preferred: preferred,
		Note:      note,
	}

	lower := strings.ToLower(key)
	switch {
	case strings.HasPrefix(lower, "speaker:"):
		entry.Speaker = strings.TrimSpace(key[len("speaker:"):])
	case strings.HasPrefix(lower, "role:"):
		entry.Speaker = strings.TrimSpace(key[len("role:"):])
	case strings.HasPrefix(lower, "voice_id:"):
		entry.VoiceID = strings.TrimSpace(key[len("voice_id:"):])
	case strings.HasPrefix(lower, "voice:"):
		entry.VoiceID = strings.TrimSpace(key[len("voice:"):])
	case strings.HasPrefix(lower, "type:"):
		entry.Type = strings.TrimSpace(key[len("type:"):])
	case strings.HasPrefix(lower, "source_arc:"):
		entry.SourceArc = strings.TrimSpace(key[len("source_arc:"):])
	case strings.HasPrefix(lower, "arc:"):
		entry.SourceArc = strings.TrimSpace(key[len("arc:"):])
	case strings.HasPrefix(lower, "source_file:"):
		entry.SourceFile = strings.TrimSpace(key[len("source_file:"):])
	case strings.HasPrefix(lower, "file:"):
		entry.SourceFile = strings.TrimSpace(key[len("file:"):])
	default:
		entry.MatchAny = key
	}
	return entry
}

func parseLegacyGlossaryColumns(line string) (string, string, string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", "", ""
	}

	if strings.Contains(trimmed, "\t") {
		parts := strings.Split(trimmed, "\t")
		key := sanitizeGlossaryKey(parts[0])
		preferred := ""
		note := ""
		if len(parts) > 1 {
			preferred = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			note = strings.TrimSpace(strings.Join(parts[2:], "\t"))
		}
		return key, preferred, note
	}

	if before, after, ok := strings.Cut(trimmed, "=>"); ok {
		return parseLegacyGlossaryArrow(before, after)
	}
	if before, after, ok := strings.Cut(trimmed, "->"); ok {
		return parseLegacyGlossaryArrow(before, after)
	}
	if before, after, ok := strings.Cut(trimmed, "|"); ok {
		return sanitizeGlossaryKey(before), "", strings.TrimSpace(after)
	}

	return sanitizeGlossaryKey(trimmed), "", ""
}

func parseLegacyGlossaryArrow(before, after string) (string, string, string) {
	key := sanitizeGlossaryKey(before)
	preferred := strings.TrimSpace(after)
	note := ""
	if target, detail, ok := strings.Cut(preferred, "|"); ok {
		preferred = strings.TrimSpace(target)
		note = strings.TrimSpace(detail)
	}
	return key, preferred, note
}

func sanitizeGlossaryKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.Trim(trimmed, "`\"'")
	return strings.TrimSpace(trimmed)
}

func glossaryEntryHasMatcher(entry glossaryEntry) bool {
	return entry.MatchAny != "" ||
		entry.Speaker != "" ||
		entry.VoiceID != "" ||
		entry.Type != "" ||
		entry.SourceArc != "" ||
		entry.SourceFile != ""
}

func relevantGlossaryEntries(entries []glossaryEntry, items []Item) string {
	if len(entries) == 0 || len(items) == 0 {
		return ""
	}

	contexts := glossaryMatchContexts(items)
	filtered := make([]map[string]string, 0, len(entries))
	for _, entry := range entries {
		if glossaryEntryMatches(entry, contexts) {
			filtered = append(filtered, glossaryPromptEntry(entry))
		}
	}
	if len(filtered) == 0 {
		return ""
	}

	raw, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return ""
	}
	return string(raw)
}

func glossaryMatchContexts(items []Item) glossaryMatchContext {
	contexts := glossaryMatchContext{
		Any:         make([]string, 0, len(items)*8),
		Speakers:    make([]string, 0, len(items)),
		VoiceIDs:    make([]string, 0, len(items)),
		Types:       make([]string, 0, len(items)),
		SourceArcs:  make([]string, 0, len(items)),
		SourceFiles: make([]string, 0, len(items)),
	}
	for _, item := range items {
		for _, value := range []string{
			item.SourceText,
			item.PreviousSourceText,
			item.NextSourceText,
			item.Role,
			item.Type,
			item.VoiceID,
			item.SourceArc,
			item.SourceFile,
		} {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				contexts.Any = append(contexts.Any, trimmed)
			}
		}

		if trimmed := strings.TrimSpace(item.Role); trimmed != "" {
			contexts.Speakers = append(contexts.Speakers, trimmed)
		}
		if trimmed := strings.TrimSpace(item.VoiceID); trimmed != "" {
			contexts.VoiceIDs = append(contexts.VoiceIDs, trimmed)
		}
		if trimmed := strings.TrimSpace(item.Type); trimmed != "" {
			contexts.Types = append(contexts.Types, trimmed)
		}
		if trimmed := strings.TrimSpace(item.SourceArc); trimmed != "" {
			contexts.SourceArcs = append(contexts.SourceArcs, trimmed)
		}
		if trimmed := strings.TrimSpace(item.SourceFile); trimmed != "" {
			contexts.SourceFiles = append(contexts.SourceFiles, trimmed)
		}
	}
	return contexts
}

func glossaryEntryMatches(entry glossaryEntry, contexts glossaryMatchContext) bool {
	if entry.MatchAny != "" && !glossaryContextContains(contexts.Any, entry.MatchAny) {
		return false
	}
	if entry.Speaker != "" && !glossaryContextContains(contexts.Speakers, entry.Speaker) {
		return false
	}
	if entry.VoiceID != "" && !glossaryContextContains(contexts.VoiceIDs, entry.VoiceID) {
		return false
	}
	if entry.Type != "" && !glossaryContextContains(contexts.Types, entry.Type) {
		return false
	}
	if entry.SourceArc != "" && !glossaryContextContains(contexts.SourceArcs, entry.SourceArc) {
		return false
	}
	if entry.SourceFile != "" && !glossaryContextContains(contexts.SourceFiles, entry.SourceFile) {
		return false
	}
	return true
}

func glossaryContextContains(contexts []string, needle string) bool {
	lowerNeedle := strings.ToLower(strings.TrimSpace(needle))
	if lowerNeedle == "" {
		return false
	}
	for _, context := range contexts {
		if strings.Contains(strings.ToLower(context), lowerNeedle) {
			return true
		}
	}
	return false
}

func glossaryPromptEntry(entry glossaryEntry) map[string]string {
	body := map[string]string{}
	if entry.MatchAny != "" {
		body["source"] = entry.MatchAny
	}
	if entry.Speaker != "" {
		body["speaker"] = entry.Speaker
	}
	if entry.VoiceID != "" {
		body["voice_id"] = entry.VoiceID
	}
	if entry.Type != "" {
		body["type"] = entry.Type
	}
	if entry.SourceArc != "" {
		body["source_arc"] = entry.SourceArc
	}
	if entry.SourceFile != "" {
		body["source_file"] = entry.SourceFile
	}
	if entry.Preferred != "" {
		body["preferred"] = entry.Preferred
	}
	if entry.Note != "" {
		body["note"] = entry.Note
	}
	return body
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func joinPromptSections(parts ...string) string {
	sections := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		sections = append(sections, trimmed)
	}
	return strings.Join(sections, "\n\n")
}

func renderPromptTemplate(req Request, prompt string, glossary string) string {
	replacements := []string{
		"{{source_language}}", req.Settings.SourceLanguage,
		"{{target_language}}", req.Settings.TargetLanguage,
		"{{target_field}}", targetFieldLabel(req.TargetField),
		"{{mode_instruction}}", modeInstruction(req),
		"{{context_instruction}}", contextInstruction(req),
		"{{style_instruction}}", styleInstruction(),
		"{{glossary}}", glossary,
		"{{response_format}}", responseFormatInstruction(targetFieldLabel(req.TargetField)),
	}
	rendered := strings.NewReplacer(replacements...).Replace(strings.TrimSpace(prompt))

	if !strings.Contains(prompt, "{{mode_instruction}}") {
		rendered += "\n\n" + modeInstruction(req)
	}
	if !strings.Contains(prompt, "{{context_instruction}}") {
		rendered += "\n\n" + contextInstruction(req)
	}
	if !strings.Contains(prompt, "{{style_instruction}}") {
		rendered += "\n\n" + styleInstruction()
	}
	if glossary != "" && !strings.Contains(prompt, "{{glossary}}") {
		rendered += "\n\n" + glossary
	}
	if !strings.Contains(prompt, "{{response_format}}") {
		rendered += "\n\n" + responseFormatInstruction(targetFieldLabel(req.TargetField))
	}
	return strings.TrimSpace(rendered)
}

func requestHasASRDerivedSourceText(req Request) bool {
	for _, item := range req.Items {
		if itemHasASRDerivedSourceText(item) {
			return true
		}
	}
	return false
}

func itemHasASRDerivedSourceText(item Item) bool {
	return strings.TrimSpace(item.Type) == "playvoice_notext" && strings.TrimSpace(item.SourceText) != ""
}

func jsonPayloadCandidates(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	candidates := []string{trimmed}
	firstArray := strings.IndexByte(trimmed, '[')
	lastArray := strings.LastIndexByte(trimmed, ']')
	if firstArray >= 0 && lastArray > firstArray {
		candidates = append(candidates, strings.TrimSpace(trimmed[firstArray:lastArray+1]))
	}

	firstObject := strings.IndexByte(trimmed, '{')
	lastObject := strings.LastIndexByte(trimmed, '}')
	if firstObject >= 0 && lastObject > firstObject {
		candidates = append(candidates, strings.TrimSpace(trimmed[firstObject:lastObject+1]))
	}

	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

func tryParseTranslationCandidate(candidate string, items []Item) ([]Result, error) {
	if results, err := parseSequentialStringArray(candidate, items); err == nil {
		return results, nil
	}
	if results, err := parseResultObjectArray(candidate, items); err == nil {
		return results, nil
	}
	if results, err := parseNamedResultsObject(candidate, items); err == nil {
		return results, nil
	}
	if results, err := parseIDMappedObject(candidate, items); err == nil {
		return results, nil
	}
	return nil, fmt.Errorf("unable to parse translation payload")
}

func parseSequentialStringArray(candidate string, items []Item) ([]Result, error) {
	var direct []string
	if err := json.Unmarshal([]byte(candidate), &direct); err != nil {
		return nil, err
	}
	if len(direct) != len(items) {
		return nil, fmt.Errorf("translation count mismatch: expected %d, got %d", len(items), len(direct))
	}

	results := make([]Result, 0, len(items))
	for index, item := range items {
		if looksLikeRefusal(direct[index]) {
			return nil, fmt.Errorf("model refused translation for id %d", item.ID)
		}
		results = append(results, Result{ID: item.ID, Text: direct[index]})
	}
	return results, nil
}

func parseResultObjectArray(candidate string, items []Item) ([]Result, error) {
	var objects []map[string]any
	if err := json.Unmarshal([]byte(candidate), &objects); err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		return nil, fmt.Errorf("translation payload array is empty")
	}

	results := make([]Result, 0, len(objects))
	for index, object := range objects {
		text, ok := translationTextFromObject(object)
		if !ok {
			return nil, fmt.Errorf("translation payload array item %d does not include a text field", index)
		}
		if looksLikeRefusal(text) {
			return nil, fmt.Errorf("model refused translation for payload item %d", index)
		}

		id, ok := int64FromAny(object["id"])
		if !ok {
			if index >= len(items) {
				return nil, fmt.Errorf("translation count mismatch: expected %d, got %d", len(items), len(objects))
			}
			id = items[index].ID
		}
		results = append(results, Result{ID: id, Text: text})
	}
	return validateResultIDs(items, results)
}

func parseNamedResultsObject(candidate string, items []Item) ([]Result, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(candidate), &body); err != nil {
		return nil, err
	}

	for _, key := range []string{"translations", "items", "results"} {
		raw, ok := body[key]
		if !ok {
			continue
		}

		if results, err := parseResultObjectArray(string(raw), items); err == nil {
			return results, nil
		}
		if results, err := parseSequentialStringArray(string(raw), items); err == nil {
			return results, nil
		}
	}
	return nil, fmt.Errorf("translation payload does not include a supported translations field")
}

func parseIDMappedObject(candidate string, items []Item) ([]Result, error) {
	var body map[string]string
	if err := json.Unmarshal([]byte(candidate), &body); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(body))
	for key, text := range body {
		id, ok := int64FromString(key)
		if !ok {
			continue
		}
		if looksLikeRefusal(text) {
			return nil, fmt.Errorf("model refused translation for id %d", id)
		}
		results = append(results, Result{ID: id, Text: text})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("translation payload object does not include id->text entries")
	}
	return validateResultIDs(items, results)
}

func validateResultIDs(items []Item, results []Result) ([]Result, error) {
	expected := make(map[int64]struct{}, len(items))
	for _, item := range items {
		expected[item.ID] = struct{}{}
	}

	resultMap := make(map[int64]string, len(results))
	for _, result := range results {
		if _, ok := expected[result.ID]; !ok {
			return nil, fmt.Errorf("translator returned unexpected id %d", result.ID)
		}
		resultMap[result.ID] = result.Text
	}
	if len(resultMap) != len(items) {
		return nil, fmt.Errorf("translation count mismatch: expected %d, got %d", len(items), len(resultMap))
	}

	ordered := make([]Result, 0, len(items))
	for _, item := range items {
		text, ok := resultMap[item.ID]
		if !ok {
			return nil, fmt.Errorf("translator did not return a result for id %d", item.ID)
		}
		ordered = append(ordered, Result{ID: item.ID, Text: text})
	}
	return ordered, nil
}

func translationTextFromObject(object map[string]any) (string, bool) {
	for _, key := range []string{"text", "translation", "translated_text", "output"} {
		if value, ok := object[key].(string); ok {
			return value, true
		}
	}
	return "", false
}

func int64FromAny(value any) (int64, bool) {
	switch current := value.(type) {
	case float64:
		return int64(current), true
	case int64:
		return current, true
	case int:
		return int64(current), true
	case string:
		return int64FromString(current)
	default:
		return 0, false
	}
}

func int64FromString(value string) (int64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	var id int64
	if _, err := fmt.Sscan(trimmed, &id); err != nil {
		return 0, false
	}
	return id, true
}

func looksLikeRefusal(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)

	englishPhrases := []string{
		"sorry, i can't translate",
		"sorry, i cannot translate",
		"i can't translate",
		"i cannot translate",
		"can't assist with that",
		"cannot assist with that",
		"can't help with that",
		"cannot help with that",
		"i'm sorry, but i can't",
		"i am sorry, but i can't",
	}
	for _, phrase := range englishPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}

	chinesePhrases := []string{
		"抱歉，我不能翻译",
		"抱歉，我无法翻译",
		"对不起，我不能翻译",
		"对不起，我无法翻译",
		"无法翻译",
		"不能翻译",
		"拒绝翻译",
		"抱歉，我不能帮助",
		"对不起，我不能帮助",
		"无法协助",
		"译文",
		"直译",
	}
	for _, phrase := range chinesePhrases {
		if strings.Contains(trimmed, phrase) {
			return true
		}
	}

	if (strings.Contains(lower, "sorry") || strings.Contains(lower, "apolog")) &&
		(strings.Contains(lower, "can't") || strings.Contains(lower, "cannot") || strings.Contains(lower, "unable")) &&
		(strings.Contains(lower, "translate") || strings.Contains(lower, "help") || strings.Contains(lower, "assist")) {
		return true
	}
	if (strings.Contains(trimmed, "抱歉") || strings.Contains(trimmed, "对不起")) &&
		(strings.Contains(trimmed, "不能") || strings.Contains(trimmed, "无法")) &&
		(strings.Contains(trimmed, "翻译") || strings.Contains(trimmed, "帮助") || strings.Contains(trimmed, "协助")) {
		return true
	}
	return false
}
