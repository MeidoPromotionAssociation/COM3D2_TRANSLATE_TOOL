package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"COM3D2TranslateTool/internal/arc"
	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/exporter"
	"COM3D2TranslateTool/internal/importer"
	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/translation"
)

type Service struct {
	baseDir     string
	store       *db.Store
	arcManager  *arc.Manager
	importers   map[string]importer.Importer
	exporters   map[string]exporter.Exporter
	translators map[string]translation.Translator
}

func New(baseDir string) (*Service, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, err
	}

	store, err := db.Open(filepath.Join(baseDir, "data", "app.sqlite"))
	if err != nil {
		return nil, err
	}

	svc := &Service{
		baseDir:    baseDir,
		store:      store,
		arcManager: arc.NewManager(store),
		importers:  map[string]importer.Importer{},
		exporters:  map[string]exporter.Exporter{},
		translators: map[string]translation.Translator{
			"manual":           translation.ManualTranslator{},
			"google-translate": translation.GoogleTranslator{},
			"baidu-translate":  translation.BaiduTranslator{},
			"openai-chat":      translation.OpenAIChatTranslator{},
			"openai-responses": translation.OpenAIResponsesTranslator{},
		},
	}

	folderImporter := importer.NewArcKSFolderTextImporter(store)
	arcSourceTextImporter := importer.NewArcSourceTextFileImporter(store)
	entryJSONLImporter := importer.NewEntryJSONLImporter(store)
	ksExtractCSVImporter := importer.NewKSExtractCSVImporter(store)
	translatedCSVImporter := importer.NewTranslatedCSVImporter(store)
	tabExporter := exporter.NewTabTextExporter(store)
	entryJSONLExporter := exporter.NewEntryJSONLExporter(store)

	svc.importers[folderImporter.Name()] = folderImporter
	svc.importers[arcSourceTextImporter.Name()] = arcSourceTextImporter
	svc.importers[entryJSONLImporter.Name()] = entryJSONLImporter
	svc.importers[ksExtractCSVImporter.Name()] = ksExtractCSVImporter
	svc.importers[translatedCSVImporter.Name()] = translatedCSVImporter
	svc.exporters[tabExporter.Name()] = tabExporter
	svc.exporters[entryJSONLExporter.Name()] = entryJSONLExporter

	settings, err := svc.GetSettings()
	if err != nil {
		store.Close()
		return nil, err
	}
	if err := svc.ensureDirectories(settings); err != nil {
		store.Close()
		return nil, err
	}

	return svc, nil
}

func (s *Service) Close() error {
	return s.store.Close()
}

func (s *Service) withDefaults(settings model.Settings) model.Settings {
	if settings.WorkDir == "" {
		settings.WorkDir = filepath.Join(s.baseDir, "work")
	}
	if settings.ExportDir == "" {
		settings.ExportDir = filepath.Join(s.baseDir, "exports")
	}
	if settings.ImportDir == "" {
		settings.ImportDir = filepath.Join(s.baseDir, "imports")
	}
	settings.Translation = model.NormalizeTranslationSettings(settings.Translation)
	return settings
}

func (s *Service) ensureDirectories(settings model.Settings) error {
	for _, dir := range []string{settings.WorkDir, settings.ImportDir, settings.ExportDir} {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) GetSettings() (model.Settings, error) {
	settings, err := s.store.GetSettings()
	if err != nil {
		return model.Settings{}, err
	}
	return s.withDefaults(settings), nil
}

func (s *Service) SaveSettings(settings model.Settings) error {
	settings = s.withDefaults(settings)
	if err := s.ensureDirectories(settings); err != nil {
		return err
	}
	return s.store.SaveSettings(settings)
}

func (s *Service) RunMaintenance() (model.MaintenanceResult, error) {
	deleted, err := s.store.CleanupInvisibleBlankSourceEntries()
	if err != nil {
		return model.MaintenanceResult{}, err
	}
	return model.MaintenanceResult{
		DeletedInvisibleBlankEntries: deleted,
	}, nil
}

func (s *Service) ScanArcs(ctx context.Context) (model.ScanResult, error) {
	settings, err := s.GetSettings()
	if err != nil {
		return model.ScanResult{}, err
	}
	return s.arcManager.ScanAndParse(ctx, settings)
}

func (s *Service) ListArcs() ([]model.ArcFile, error) {
	return s.store.ListArcs()
}

func (s *Service) ReparseArc(ctx context.Context, arcID int64) (model.ParseResult, error) {
	settings, err := s.GetSettings()
	if err != nil {
		return model.ParseResult{}, err
	}
	return s.arcManager.Reparse(ctx, settings, arcID)
}

func (s *Service) ReparseFailedArcs(ctx context.Context) (model.ReparseFailedResult, error) {
	settings, err := s.GetSettings()
	if err != nil {
		return model.ReparseFailedResult{}, err
	}
	return s.arcManager.ReparseFailed(ctx, settings)
}

func (s *Service) ReparseAllArcs(ctx context.Context) (model.ReparseAllResult, error) {
	settings, err := s.GetSettings()
	if err != nil {
		return model.ReparseAllResult{}, err
	}
	return s.arcManager.ReparseAll(ctx, settings)
}

func (s *Service) ListEntries(query model.EntryQuery) (model.EntryList, error) {
	return s.store.ListEntries(query)
}

func (s *Service) GetFilterOptions() (model.FilterOptions, error) {
	return s.store.GetFilterOptions()
}

func (s *Service) UpdateEntry(input model.UpdateEntryInput) error {
	return s.store.UpdateEntry(input)
}

func (s *Service) BatchUpdateEntries(input model.BatchUpdateInput) (model.BatchUpdateResult, error) {
	return s.store.BatchUpdateEntries(input)
}

func (s *Service) BatchUpdateEntryStatusByQuery(input model.FilterBatchStatusInput) (model.BatchUpdateResult, error) {
	return s.store.BatchUpdateEntryStatusByQuery(input)
}

func (s *Service) DeleteEntriesByQuery(query model.EntryQuery) (model.BatchDeleteResult, error) {
	return s.store.DeleteEntriesByQuery(query)
}

func (s *Service) ClearEntryTranslationsByQuery(query model.EntryQuery) (model.BatchUpdateResult, error) {
	return s.store.ClearEntryTranslationsByQuery(query)
}

func (s *Service) ListImporters() []string {
	return sortedKeys(s.importers)
}

func (s *Service) RunImport(ctx context.Context, req model.ImportRequest) (model.ImportResult, error) {
	settings, err := s.GetSettings()
	if err != nil {
		return model.ImportResult{}, err
	}
	if req.RootDir == "" {
		req.RootDir = settings.ImportDir
	}

	name := req.Importer
	if name == "" {
		name = "arc-ks-folder-text"
	}
	impl := s.importers[name]
	if impl == nil {
		return model.ImportResult{}, fmt.Errorf("unknown importer: %s", name)
	}
	return impl.Import(ctx, req)
}

func (s *Service) ListExporters() []string {
	return sortedKeys(s.exporters)
}

func (s *Service) RunExport(ctx context.Context, req model.ExportRequest) (model.ExportResult, error) {
	settings, err := s.GetSettings()
	if err != nil {
		return model.ExportResult{}, err
	}
	if req.OutputPath == "" {
		req.OutputPath = filepath.Join(settings.ExportDir, "translations.txt")
	}
	if req.Exporter == "" {
		req.Exporter = "tab-text"
	}

	impl := s.exporters[req.Exporter]
	if impl == nil {
		return model.ExportResult{}, fmt.Errorf("unknown exporter: %s", req.Exporter)
	}
	return impl.Export(ctx, req)
}

func (s *Service) TestTranslator(ctx context.Context, req model.TranslatorTestRequest) (model.TranslatorTestResult, error) {
	name := strings.TrimSpace(req.Translator)
	if name == "" {
		name = model.NormalizeTranslationSettings(req.Settings).ActiveTranslator
	}
	if name == "" {
		name = "manual"
	}

	impl := s.translators[name]
	if impl == nil {
		return model.TranslatorTestResult{}, fmt.Errorf("unknown translator: %s", name)
	}

	targetField := translation.NormalizeTargetField(req.TargetField)
	settings := model.NormalizeTranslationSettings(req.Settings)
	item := translation.Item{
		ID:             1,
		Type:           "dialogue",
		Role:           "[TEST]",
		SourceArc:      "__translator_test__.arc",
		SourceFile:     "__translator_test__.ks",
		SourceText:     "おはようございます。",
		TranslatedText: "早上好。",
	}

	startedAt := time.Now()
	results, err := impl.Translate(ctx, translation.Request{
		Settings:    settings,
		Items:       []translation.Item{item},
		TargetField: targetField,
	})
	if err != nil {
		return model.TranslatorTestResult{}, err
	}
	if len(results) == 0 {
		return model.TranslatorTestResult{}, fmt.Errorf("translator returned no results")
	}

	outputText := strings.TrimSpace(results[0].Text)
	if outputText == "" {
		return model.TranslatorTestResult{}, fmt.Errorf("translator returned an empty result")
	}

	return model.TranslatorTestResult{
		Translator:   name,
		TargetField:  targetField,
		SourceText:   item.SourceText,
		OutputText:   outputText,
		ResponseTime: time.Since(startedAt).Milliseconds(),
	}, nil
}

func sortedKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
