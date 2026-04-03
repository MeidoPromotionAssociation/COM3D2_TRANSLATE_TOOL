package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/service"
	"COM3D2TranslateTool/internal/translation"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx     context.Context
	service *service.Service
	mu      sync.Mutex
	taskID  int64
	cancel  context.CancelFunc
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if a.service != nil {
		_ = a.service.Close()
	}
}

func (a *App) beginTask() (context.Context, int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cancel != nil {
		return nil, 0, fmt.Errorf("another task is already running")
	}

	base := a.ctx
	if base == nil {
		base = context.Background()
	}

	ctx, cancel := context.WithCancel(base)
	a.taskID++
	a.cancel = cancel
	return ctx, a.taskID, nil
}

func (a *App) endTask(taskID int64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.taskID != taskID {
		return
	}
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}

func (a *App) StopCurrentTask() bool {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()

	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (a *App) ensureService() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.service != nil {
		return nil
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	baseDir := filepath.Join(configDir, "COM3D2TranslateTool")
	svc, err := service.New(baseDir)
	if err != nil {
		return err
	}
	a.service = svc
	return nil
}

func normalizeSettingsInput(in model.Settings) model.Settings {
	return model.Settings{
		ArcScanDir:  strings.TrimSpace(in.ArcScanDir),
		WorkDir:     strings.TrimSpace(in.WorkDir),
		ImportDir:   strings.TrimSpace(in.ImportDir),
		ExportDir:   strings.TrimSpace(in.ExportDir),
		Translation: normalizeTranslationSettingsInput(in.Translation),
	}
}

func normalizeTranslationSettingsInput(in model.TranslationSettings) model.TranslationSettings {
	in.ActiveTranslator = strings.TrimSpace(in.ActiveTranslator)
	in.SourceLanguage = strings.TrimSpace(in.SourceLanguage)
	in.TargetLanguage = strings.TrimSpace(in.TargetLanguage)
	in.Glossary = strings.TrimSpace(in.Glossary)
	in.Proxy.Mode = strings.TrimSpace(strings.ToLower(in.Proxy.Mode))
	in.Proxy.URL = strings.TrimSpace(in.Proxy.URL)

	in.Google.BaseURL = strings.TrimSpace(in.Google.BaseURL)
	in.Google.APIKey = strings.TrimSpace(in.Google.APIKey)
	in.Google.Format = strings.TrimSpace(in.Google.Format)
	in.Google.Model = strings.TrimSpace(in.Google.Model)

	in.Baidu.BaseURL = strings.TrimSpace(in.Baidu.BaseURL)
	in.Baidu.AppID = strings.TrimSpace(in.Baidu.AppID)
	in.Baidu.Secret = strings.TrimSpace(in.Baidu.Secret)
	in.ASR.BaseURL = strings.TrimSpace(in.ASR.BaseURL)
	in.ASR.Language = strings.TrimSpace(in.ASR.Language)
	in.ASR.Prompt = strings.TrimSpace(in.ASR.Prompt)

	in.OpenAIChat = normalizeOpenAIProviderInput(in.OpenAIChat)
	in.OpenAIResponses = normalizeOpenAIProviderInput(in.OpenAIResponses)
	return in
}

func normalizeOpenAIProviderInput(in model.OpenAIProviderConfig) model.OpenAIProviderConfig {
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.APIKey = strings.TrimSpace(in.APIKey)
	in.Model = strings.TrimSpace(in.Model)
	in.Prompt = strings.TrimSpace(in.Prompt)
	in.ReasoningEffort = strings.TrimSpace(in.ReasoningEffort)
	in.ExtraJSON = strings.TrimSpace(in.ExtraJSON)
	return in
}

func (a *App) GetSettings() (model.Settings, error) {
	if err := a.ensureService(); err != nil {
		return model.Settings{}, err
	}
	return a.service.GetSettings()
}

func (a *App) SaveSettings(settings model.Settings) error {
	if err := a.ensureService(); err != nil {
		return err
	}
	return a.service.SaveSettings(normalizeSettingsInput(settings))
}

func (a *App) RunMaintenance() (model.MaintenanceResult, error) {
	if err := a.ensureService(); err != nil {
		return model.MaintenanceResult{}, err
	}
	return a.service.RunMaintenance()
}

func (a *App) RunMaintenanceFillTranslated() (model.MaintenanceResult, error) {
	if err := a.ensureService(); err != nil {
		return model.MaintenanceResult{}, err
	}
	ctx, taskID, err := a.beginTask()
	if err != nil {
		return model.MaintenanceResult{}, err
	}
	defer a.endTask(taskID)
	return a.service.RunMaintenanceFillTranslated(ctx)
}

func (a *App) TestProxy(proxy model.ProxyConfig) (model.ProxyTestResult, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	testCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	return translation.TestProxyConnectivity(testCtx, proxy, "https://www.google.com")
}

func (a *App) TestTranslator(req model.TranslatorTestRequest) (model.TranslatorTestResult, error) {
	if err := a.ensureService(); err != nil {
		return model.TranslatorTestResult{}, err
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	testCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req.Translator = strings.TrimSpace(req.Translator)
	req.TargetField = strings.TrimSpace(req.TargetField)
	req.Settings = normalizeTranslationSettingsInput(req.Settings)
	return a.service.TestTranslator(testCtx, req)
}

func (a *App) TestASR(settings model.TranslationSettings) (model.ASRTestResult, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	testCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	settings = model.NormalizeTranslationSettings(normalizeTranslationSettingsInput(settings))
	return translation.TestASRTranscription(testCtx, settings.Proxy, settings.ASR)
}

func (a *App) ScanArcs() (model.ScanResult, error) {
	if err := a.ensureService(); err != nil {
		return model.ScanResult{}, err
	}
	ctx, taskID, err := a.beginTask()
	if err != nil {
		return model.ScanResult{}, err
	}
	defer a.endTask(taskID)
	return a.service.ScanArcs(ctx)
}

func (a *App) ListArcs() ([]model.ArcFile, error) {
	if err := a.ensureService(); err != nil {
		return nil, err
	}
	return a.service.ListArcs()
}

func (a *App) ReparseArc(arcID int64) (model.ParseResult, error) {
	if err := a.ensureService(); err != nil {
		return model.ParseResult{}, err
	}
	ctx, taskID, err := a.beginTask()
	if err != nil {
		return model.ParseResult{}, err
	}
	defer a.endTask(taskID)
	return a.service.ReparseArc(ctx, arcID)
}

func (a *App) ReparseFailedArcs() (model.ReparseFailedResult, error) {
	if err := a.ensureService(); err != nil {
		return model.ReparseFailedResult{}, err
	}
	ctx, taskID, err := a.beginTask()
	if err != nil {
		return model.ReparseFailedResult{}, err
	}
	defer a.endTask(taskID)
	return a.service.ReparseFailedArcs(ctx)
}

func (a *App) ReparseAllArcs() (model.ReparseAllResult, error) {
	if err := a.ensureService(); err != nil {
		return model.ReparseAllResult{}, err
	}
	ctx, taskID, err := a.beginTask()
	if err != nil {
		return model.ReparseAllResult{}, err
	}
	defer a.endTask(taskID)
	return a.service.ReparseAllArcs(ctx)
}

func (a *App) ListEntries(query model.EntryQuery) (model.EntryList, error) {
	if err := a.ensureService(); err != nil {
		return model.EntryList{}, err
	}
	return a.service.ListEntries(query)
}

func (a *App) GetFilterOptions() (model.FilterOptions, error) {
	if err := a.ensureService(); err != nil {
		return model.FilterOptions{}, err
	}
	return a.service.GetFilterOptions()
}

func (a *App) UpdateEntry(input model.UpdateEntryInput) error {
	if err := a.ensureService(); err != nil {
		return err
	}
	return a.service.UpdateEntry(input)
}

func (a *App) BatchUpdateEntries(input model.BatchUpdateInput) (model.BatchUpdateResult, error) {
	if err := a.ensureService(); err != nil {
		return model.BatchUpdateResult{}, err
	}
	return a.service.BatchUpdateEntries(input)
}

func (a *App) BatchUpdateEntryStatusByQuery(input model.FilterBatchStatusInput) (model.BatchUpdateResult, error) {
	if err := a.ensureService(); err != nil {
		return model.BatchUpdateResult{}, err
	}
	return a.service.BatchUpdateEntryStatusByQuery(input)
}

func (a *App) DeleteEntriesByQuery(query model.EntryQuery) (model.BatchDeleteResult, error) {
	if err := a.ensureService(); err != nil {
		return model.BatchDeleteResult{}, err
	}
	return a.service.DeleteEntriesByQuery(query)
}

func (a *App) ClearEntryTranslationsByQuery(query model.EntryQuery) (model.BatchUpdateResult, error) {
	if err := a.ensureService(); err != nil {
		return model.BatchUpdateResult{}, err
	}
	return a.service.ClearEntryTranslationsByQuery(query)
}

func (a *App) ListImporters() ([]string, error) {
	if err := a.ensureService(); err != nil {
		return nil, err
	}
	return a.service.ListImporters(), nil
}

func (a *App) RunImport(req model.ImportRequest) (model.ImportResult, error) {
	if err := a.ensureService(); err != nil {
		return model.ImportResult{}, err
	}
	ctx, taskID, err := a.beginTask()
	if err != nil {
		return model.ImportResult{}, err
	}
	defer a.endTask(taskID)
	return a.service.RunImport(ctx, req)
}

func (a *App) ListExporters() ([]string, error) {
	if err := a.ensureService(); err != nil {
		return nil, err
	}
	return a.service.ListExporters(), nil
}

func (a *App) ListTranslators() ([]string, error) {
	if err := a.ensureService(); err != nil {
		return nil, err
	}
	return a.service.ListTranslators(), nil
}

func (a *App) RunExport(req model.ExportRequest) (model.ExportResult, error) {
	if err := a.ensureService(); err != nil {
		return model.ExportResult{}, err
	}
	ctx, taskID, err := a.beginTask()
	if err != nil {
		return model.ExportResult{}, err
	}
	defer a.endTask(taskID)
	return a.service.RunExport(ctx, req)
}

func (a *App) RunTranslation(req model.TranslateRequest) (model.TranslateResult, error) {
	if err := a.ensureService(); err != nil {
		return model.TranslateResult{}, err
	}
	ctx, taskID, err := a.beginTask()
	if err != nil {
		return model.TranslateResult{}, err
	}
	defer a.endTask(taskID)
	return a.service.RunTranslation(ctx, req)
}

func (a *App) RunSourceRecognition(req model.SourceRecognitionRequest) (model.SourceRecognitionResult, error) {
	if err := a.ensureService(); err != nil {
		return model.SourceRecognitionResult{}, err
	}
	ctx, taskID, err := a.beginTask()
	if err != nil {
		return model.SourceRecognitionResult{}, err
	}
	defer a.endTask(taskID)
	return a.service.RunSourceRecognition(ctx, req)
}

func (a *App) BrowseDirectory(title string, currentPath string) (string, error) {
	defaultDir := strings.TrimSpace(currentPath)
	if defaultDir != "" {
		info, err := os.Stat(defaultDir)
		if err != nil || !info.IsDir() {
			defaultDir = ""
		}
	}

	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:            strings.TrimSpace(title),
		DefaultDirectory: defaultDir,
	})
}

func (a *App) BrowseSaveFile(title string, currentPath string, defaultFilename string) (string, error) {
	return a.BrowseSaveFileWithFilter(title, currentPath, defaultFilename, "Text Files", "*.txt")
}

func (a *App) BrowseSaveFileWithFilter(title string, currentPath string, defaultFilename string, displayName string, pattern string) (string, error) {
	currentPath = strings.TrimSpace(currentPath)
	defaultDir := ""
	if currentPath != "" {
		candidateDir := currentPath
		if ext := filepath.Ext(currentPath); ext != "" {
			candidateDir = filepath.Dir(currentPath)
		}
		if info, err := os.Stat(candidateDir); err == nil && info.IsDir() {
			defaultDir = candidateDir
		}
	}

	defaultName := strings.TrimSpace(defaultFilename)
	if defaultName == "" {
		defaultName = "translations.txt"
	}
	if currentPath != "" {
		if base := filepath.Base(currentPath); base != "." && base != string(filepath.Separator) {
			defaultName = base
		}
	}

	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = "All Files"
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		pattern = "*.*"
	}

	return wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:            strings.TrimSpace(title),
		DefaultDirectory: defaultDir,
		DefaultFilename:  defaultName,
		Filters: []wruntime.FileFilter{
			{DisplayName: displayName, Pattern: pattern},
		},
	})
}

func (a *App) BrowseOpenFile(title string, currentPath string, displayName string, pattern string) (string, error) {
	currentPath = strings.TrimSpace(currentPath)
	defaultDir := ""
	defaultName := ""

	if currentPath != "" {
		candidateDir := currentPath
		if ext := filepath.Ext(currentPath); ext != "" {
			candidateDir = filepath.Dir(currentPath)
			defaultName = filepath.Base(currentPath)
		}
		if info, err := os.Stat(candidateDir); err == nil && info.IsDir() {
			defaultDir = candidateDir
		}
	}

	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = "All Files"
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		pattern = "*.*"
	}

	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:            strings.TrimSpace(title),
		DefaultDirectory: defaultDir,
		DefaultFilename:  defaultName,
		Filters: []wruntime.FileFilter{
			{DisplayName: displayName, Pattern: pattern},
		},
	})
}

func (a *App) ReadTextFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) WriteTextFile(path string, content string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("path is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
