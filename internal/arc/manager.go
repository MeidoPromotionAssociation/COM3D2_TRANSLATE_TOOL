package arc

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	msarc "github.com/MeidoPromotionAssociation/MeidoSerialization/serialization/COM3D2/arc"
	mscom "github.com/MeidoPromotionAssociation/MeidoSerialization/service/COM3D2"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/kag"
	"COM3D2TranslateTool/internal/model"
)

type Manager struct {
	store      *db.Store
	arcService *mscom.ArcService

	voiceIndexMu    sync.RWMutex
	voiceIndex      map[string][]voiceLocation
	voiceIndexReady bool
}

type voiceLocation struct {
	ArcFile   model.ArcFile
	EntryPath string
	Ext       string
}

var supportedVoiceAudioExtensions = map[string]struct{}{
	".ogg":  {},
	".wav":  {},
	".mp3":  {},
	".m4a":  {},
	".flac": {},
	".opus": {},
	".aac":  {},
	".wma":  {},
}

func deduplicateEntries(entries []model.Entry) ([]model.Entry, int) {
	if len(entries) == 0 {
		return nil, 0
	}

	seen := make(map[string]struct{}, len(entries))
	deduped := make([]model.Entry, 0, len(entries))
	duplicates := 0

	for _, entry := range entries {
		key := strings.Join([]string{
			entry.Type,
			entry.VoiceID,
			entry.Role,
			entry.SourceArc,
			entry.SourceFile,
			entry.SourceText,
		}, "\x00")
		if _, exists := seen[key]; exists {
			duplicates++
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, entry)
	}

	return deduped, duplicates
}

func NewManager(store *db.Store) *Manager {
	return &Manager{
		store:      store,
		arcService: &mscom.ArcService{},
	}
}

func findVoiceFilePath(fileList []string, voiceID string) (string, error) {
	key := normalizeVoiceLookupKey(voiceID)
	if key == "" {
		return "", fmt.Errorf("voice id is required")
	}

	matches := make([]voiceLocation, 0, 1)
	for _, entryPath := range fileList {
		base := filepath.Base(entryPath)
		ext := strings.ToLower(filepath.Ext(base))

		if _, ok := supportedVoiceAudioExtensions[ext]; !ok {
			continue
		}
		if normalizeVoiceLookupKey(base) != key {
			continue
		}
		matches = append(matches, voiceLocation{
			EntryPath: entryPath,
			Ext:       ext,
		})
	}

	match, err := selectPreferredVoiceLocation(matches, voiceID)
	if err != nil {
		return "", err
	}
	return match.EntryPath, nil
}

func (m *Manager) ExtractVoiceFile(workDir string, record model.ArcFile, voiceID string) (string, func(), error) {
	arcFS, closer, err := m.arcService.ReadArcLazy(record.Path)
	if err != nil {
		return "", nil, err
	}

	matchPath, err := findVoiceFilePath(m.arcService.GetFileList(arcFS), voiceID)
	if err == nil {
		return m.extractMatchedVoiceFile(workDir, arcFS, matchPath, closer)
	}
	if !isVoiceNotFoundError(err) {
		_ = closer.Close()
		return "", nil, err
	}

	_ = closer.Close()

	fallbackRecord, fallbackPath, fallbackErr := m.findVoiceFileInAllArcs(record, voiceID)
	if fallbackErr != nil {
		return "", nil, fallbackErr
	}

	fallbackFS, fallbackCloser, fallbackErr := m.arcService.ReadArcLazy(fallbackRecord.Path)
	if fallbackErr != nil {
		return "", nil, fallbackErr
	}
	return m.extractMatchedVoiceFile(workDir, fallbackFS, fallbackPath, fallbackCloser)
}

func (m *Manager) extractMatchedVoiceFile(workDir string, arcFS *msarc.Arc, matchPath string, closer io.Closer) (string, func(), error) {
	extractRoot := filepath.Join(workDir, "voice_extract")
	if err := os.MkdirAll(extractRoot, 0o755); err != nil {
		_ = closer.Close()
		return "", nil, err
	}

	tempDir, err := os.MkdirTemp(extractRoot, "voice-*")
	if err != nil {
		_ = closer.Close()
		return "", nil, err
	}

	outputPath := filepath.Join(tempDir, filepath.Base(matchPath))
	if err := m.arcService.ExtractFile(arcFS, matchPath, outputPath); err != nil {
		_ = os.RemoveAll(tempDir)
		_ = closer.Close()
		return "", nil, err
	}

	cleanup := func() {
		_ = closer.Close()
		_ = os.RemoveAll(tempDir)
	}
	return outputPath, cleanup, nil
}

func normalizeVoiceLookupKey(value string) string {
	trimmed := strings.Trim(strings.TrimSpace(value), `"'`)
	if trimmed == "" {
		return ""
	}

	trimmed = strings.ReplaceAll(trimmed, "/", string(filepath.Separator))
	trimmed = strings.ReplaceAll(trimmed, "\\", string(filepath.Separator))
	trimmed = filepath.Base(trimmed)

	ext := strings.ToLower(filepath.Ext(trimmed))
	if _, ok := supportedVoiceAudioExtensions[ext]; ok {
		trimmed = strings.TrimSuffix(trimmed, filepath.Ext(trimmed))
	}

	return strings.ToLower(strings.TrimSpace(trimmed))
}

func isVoiceNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "audio file not found for voice id")
}

func selectPreferredVoiceLocation(matches []voiceLocation, voiceID string) (voiceLocation, error) {
	if len(matches) == 0 {
		return voiceLocation{}, fmt.Errorf("audio file not found for voice id %s", strings.TrimSpace(voiceID))
	}

	unique := make([]voiceLocation, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		key := match.ArcFile.Filename + "\x00" + match.EntryPath
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, match)
	}

	oggMatches := make([]voiceLocation, 0, len(unique))
	for _, match := range unique {
		if strings.EqualFold(match.Ext, ".ogg") {
			oggMatches = append(oggMatches, match)
		}
	}
	if len(oggMatches) == 1 {
		return oggMatches[0], nil
	}
	if len(oggMatches) > 1 {
		return voiceLocation{}, fmt.Errorf("multiple audio files matched voice id %s", strings.TrimSpace(voiceID))
	}
	if len(unique) == 1 {
		return unique[0], nil
	}
	return voiceLocation{}, fmt.Errorf("multiple audio files matched voice id %s", strings.TrimSpace(voiceID))
}

func (m *Manager) findVoiceFileInAllArcs(sourceRecord model.ArcFile, voiceID string) (model.ArcFile, string, error) {
	locations, err := m.ensureVoiceIndex()
	if err != nil {
		return model.ArcFile{}, "", err
	}

	key := normalizeVoiceLookupKey(voiceID)
	if key == "" {
		return model.ArcFile{}, "", fmt.Errorf("voice id is required")
	}

	matches := make([]voiceLocation, 0, 1)
	for _, location := range locations[key] {
		if sourceRecord.ID != 0 && location.ArcFile.ID == sourceRecord.ID {
			continue
		}
		if sourceRecord.Filename != "" && strings.EqualFold(location.ArcFile.Filename, sourceRecord.Filename) {
			continue
		}
		matches = append(matches, location)
	}

	match, err := selectPreferredVoiceLocation(matches, voiceID)
	if err != nil {
		return model.ArcFile{}, "", err
	}
	return match.ArcFile, match.EntryPath, nil
}

func (m *Manager) ensureVoiceIndex() (map[string][]voiceLocation, error) {
	m.voiceIndexMu.RLock()
	if m.voiceIndexReady {
		index := m.voiceIndex
		m.voiceIndexMu.RUnlock()
		return index, nil
	}
	m.voiceIndexMu.RUnlock()

	m.voiceIndexMu.Lock()
	defer m.voiceIndexMu.Unlock()
	if m.voiceIndexReady {
		return m.voiceIndex, nil
	}

	index, err := m.buildVoiceIndex()
	if err != nil {
		return nil, err
	}
	m.voiceIndex = index
	m.voiceIndexReady = true
	return m.voiceIndex, nil
}

func (m *Manager) buildVoiceIndex() (map[string][]voiceLocation, error) {
	arcs, err := m.store.ListArcs()
	if err != nil {
		return nil, err
	}

	index := make(map[string][]voiceLocation)
	for _, record := range arcs {
		if strings.TrimSpace(record.Path) == "" {
			continue
		}

		arcFS, closer, err := m.arcService.ReadArcLazy(record.Path)
		if err != nil {
			continue
		}

		for _, entryPath := range m.arcService.GetFileList(arcFS) {
			base := filepath.Base(entryPath)
			ext := strings.ToLower(filepath.Ext(base))
			if _, ok := supportedVoiceAudioExtensions[ext]; !ok {
				continue
			}

			key := normalizeVoiceLookupKey(base)
			if key == "" {
				continue
			}

			index[key] = append(index[key], voiceLocation{
				ArcFile:   record,
				EntryPath: entryPath,
				Ext:       ext,
			})
		}

		_ = closer.Close()
	}

	return index, nil
}

func (m *Manager) invalidateVoiceIndex() {
	m.voiceIndexMu.Lock()
	defer m.voiceIndexMu.Unlock()
	m.voiceIndex = nil
	m.voiceIndexReady = false
}

func (m *Manager) ScanAndParse(ctx context.Context, settings model.Settings) (model.ScanResult, error) {
	scanDir := strings.TrimSpace(settings.ArcScanDir)
	if scanDir == "" {
		return model.ScanResult{}, fmt.Errorf("arc scan directory is not configured")
	}
	if settings.WorkDir == "" {
		return model.ScanResult{}, fmt.Errorf("work directory is not configured")
	}

	m.invalidateVoiceIndex()

	result := model.ScanResult{
		Messages: make([]string, 0),
	}
	err := filepath.WalkDir(scanDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".arc") {
			return nil
		}

		result.Scanned++
		record, isNew, err := m.store.TouchArc(filepath.Base(path), path)
		if err != nil {
			return err
		}
		if !isNew {
			return nil
		}

		result.NewArcCount++
		parseResult, err := m.parseArc(ctx, settings.WorkDir, record)
		if err != nil {
			result.FailedCount++
			result.Messages = append(result.Messages, fmt.Sprintf("%s: %v", record.Filename, err))
			return nil
		}
		result.ParsedCount++
		result.Messages = append(result.Messages, parseResult.Message)
		return nil
	})
	return result, err
}

func (m *Manager) Reparse(ctx context.Context, settings model.Settings, arcID int64) (model.ParseResult, error) {
	if settings.WorkDir == "" {
		return model.ParseResult{}, fmt.Errorf("work directory is not configured")
	}
	m.invalidateVoiceIndex()
	record, err := m.store.GetArcByID(arcID)
	if err != nil {
		return model.ParseResult{}, err
	}
	return m.parseArc(ctx, settings.WorkDir, record)
}

func (m *Manager) ReparseFailed(ctx context.Context, settings model.Settings) (model.ReparseFailedResult, error) {
	if settings.WorkDir == "" {
		return model.ReparseFailedResult{}, fmt.Errorf("work directory is not configured")
	}

	m.invalidateVoiceIndex()

	records, err := m.store.ListArcsByStatus("failed")
	if err != nil {
		return model.ReparseFailedResult{}, err
	}

	result := model.ReparseFailedResult{
		TotalFailed: len(records),
		Messages:    make([]string, 0, len(records)),
	}
	for _, record := range records {
		parseResult, err := m.parseArc(ctx, settings.WorkDir, record)
		if err != nil {
			result.FailedCount++
			result.Messages = append(result.Messages, fmt.Sprintf("%s: %v", record.Filename, err))
			continue
		}

		result.ReparsedCount++
		if parseResult.Message != "" {
			result.Messages = append(result.Messages, parseResult.Message)
		}
	}

	return result, nil
}

func (m *Manager) ReparseAll(ctx context.Context, settings model.Settings) (model.ReparseAllResult, error) {
	if settings.WorkDir == "" {
		return model.ReparseAllResult{}, fmt.Errorf("work directory is not configured")
	}

	m.invalidateVoiceIndex()

	records, err := m.store.ListArcs()
	if err != nil {
		return model.ReparseAllResult{}, err
	}

	result := model.ReparseAllResult{
		TotalArcs: len(records),
		Messages:  make([]string, 0, len(records)),
	}

	for _, record := range records {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if strings.TrimSpace(record.Path) == "" {
			result.SkippedCount++
			result.Messages = append(result.Messages, fmt.Sprintf("%s: skipped because the arc path is empty", record.Filename))
			continue
		}

		parseResult, err := m.parseArc(ctx, settings.WorkDir, record)
		if err != nil {
			result.FailedCount++
			result.Messages = append(result.Messages, fmt.Sprintf("%s: %v", record.Filename, err))
			continue
		}

		result.ReparsedCount++
		if parseResult.Message != "" {
			result.Messages = append(result.Messages, parseResult.Message)
		}
	}

	return result, nil
}

func (m *Manager) parseArc(ctx context.Context, workDir string, record model.ArcFile) (model.ParseResult, error) {
	if err := m.store.UpdateArcStatus(record.ID, "parsing", "", false); err != nil {
		return model.ParseResult{}, err
	}

	arcFS, closer, err := m.arcService.ReadArcLazy(record.Path)
	if err != nil {
		_ = m.store.UpdateArcStatus(record.ID, "failed", err.Error(), false)
		return model.ParseResult{}, err
	}
	defer closer.Close()

	var ksPaths []string
	for _, entryPath := range m.arcService.GetFileList(arcFS) {
		if strings.EqualFold(filepath.Ext(entryPath), ".ks") {
			ksPaths = append(ksPaths, entryPath)
		}
	}

	extractRoot := filepath.Join(workDir, "extract")
	if err := os.MkdirAll(extractRoot, 0o755); err != nil {
		_ = m.store.UpdateArcStatus(record.ID, "failed", err.Error(), false)
		return model.ParseResult{}, err
	}

	tempDir, err := os.MkdirTemp(extractRoot, "arc-*")
	if err != nil {
		_ = m.store.UpdateArcStatus(record.ID, "failed", err.Error(), false)
		return model.ParseResult{}, err
	}
	defer os.RemoveAll(tempDir)

	if len(ksPaths) > 0 {
		if err := m.arcService.ExtractFiles(arcFS, ksPaths, tempDir); err != nil {
			_ = m.store.UpdateArcStatus(record.ID, "failed", err.Error(), false)
			return model.ParseResult{}, err
		}
	}

	parsedEntries, err := kag.ParseKSDir(tempDir, true, record.Filename)
	if err != nil {
		_ = m.store.UpdateArcStatus(record.ID, "failed", err.Error(), false)
		return model.ParseResult{}, err
	}

	entries := make([]model.Entry, 0, len(parsedEntries))
	for _, entry := range parsedEntries {
		entries = append(entries, model.Entry{
			Type:       entry.Type,
			VoiceID:    entry.VoiceID,
			Role:       entry.Role,
			SourceArc:  record.Filename,
			SourceFile: entry.SourceFile,
			SourceText: entry.SourceText,
		})
	}

	entries, duplicateCount := deduplicateEntries(entries)

	if err := m.store.ReplaceEntriesForArc(ctx, record.Filename, entries); err != nil {
		_ = m.store.UpdateArcStatus(record.ID, "failed", err.Error(), false)
		return model.ParseResult{}, err
	}
	if err := m.store.UpdateArcStatus(record.ID, "parsed", "", true); err != nil {
		return model.ParseResult{}, err
	}

	message := fmt.Sprintf("%s parsed, %d entries stored", record.Filename, len(entries))
	if duplicateCount > 0 {
		message = fmt.Sprintf("%s, %d duplicates skipped", message, duplicateCount)
	}

	return model.ParseResult{
		ArcFilename: record.Filename,
		EntryCount:  len(entries),
		Message:     message,
	}, nil
}
