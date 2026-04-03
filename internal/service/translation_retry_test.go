package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/translation"
)

type fakeRetryTranslator struct {
	mu        sync.Mutex
	calls     int
	failUntil int
}

func (f *fakeRetryTranslator) Name() string {
	return "google-translate"
}

func (f *fakeRetryTranslator) Translate(_ context.Context, req translation.Request) ([]translation.Result, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()

	if call <= f.failUntil {
		return nil, errors.New("temporary translator failure")
	}

	results := make([]translation.Result, 0, len(req.Items))
	for _, item := range req.Items {
		results = append(results, translation.Result{
			ID:   item.ID,
			Text: item.SourceText + "-zh",
		})
	}
	return results, nil
}

func (f *fakeRetryTranslator) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func seedRetryTestService(t *testing.T, translator translation.Translator, retryCount int) *Service {
	t.Helper()

	baseDir := t.TempDir()
	store, err := db.Open(filepath.Join(baseDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	if err := store.SaveSettings(model.Settings{
		Translation: model.TranslationSettings{
			ActiveTranslator: "google-translate",
			RetryCount:       retryCount,
		},
	}); err != nil {
		store.Close()
		t.Fatalf("save settings: %v", err)
	}

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{
		{
			Type:       "talk",
			SourceArc:  "script.arc",
			SourceFile: "scene01.ks",
			SourceText: "alpha",
		},
		{
			Type:       "talk",
			SourceArc:  "script.arc",
			SourceFile: "scene01.ks",
			SourceText: "beta",
		},
	}); err != nil {
		store.Close()
		t.Fatalf("seed entries: %v", err)
	}

	return &Service{
		baseDir: baseDir,
		store:   store,
		translators: map[string]translation.Translator{
			"google-translate": translator,
		},
	}
}

func TestRunTranslationRetriesFailedBatchAndEventuallySucceeds(t *testing.T) {
	translator := &fakeRetryTranslator{failUntil: 1}
	svc := seedRetryTestService(t, translator, 1)
	defer svc.Close()

	result, err := svc.RunTranslation(context.Background(), model.TranslateRequest{
		Translator:       "google-translate",
		TargetField:      "translated",
		UntranslatedOnly: true,
	})
	if err != nil {
		t.Fatalf("run translation: %v", err)
	}

	if translator.CallCount() != 2 {
		t.Fatalf("expected translator to be called twice, got %d", translator.CallCount())
	}
	if result.Failed != 0 {
		t.Fatalf("expected no final failures, got %#v", result)
	}
	if result.Processed != 2 || result.Updated != 2 || result.Skipped != 0 {
		t.Fatalf("unexpected result counts: %#v", result)
	}

	entries, err := svc.store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	for _, entry := range entries.Items {
		if entry.TranslatedText != entry.SourceText+"-zh" {
			t.Fatalf("expected translated text to be saved after retry, got %#v", entry)
		}
	}
}

func TestRunTranslationMarksBatchFailedAfterRetriesExhausted(t *testing.T) {
	translator := &fakeRetryTranslator{failUntil: 2}
	svc := seedRetryTestService(t, translator, 1)
	defer svc.Close()

	result, err := svc.RunTranslation(context.Background(), model.TranslateRequest{
		Translator:       "google-translate",
		TargetField:      "translated",
		UntranslatedOnly: true,
	})
	if err != nil {
		t.Fatalf("run translation: %v", err)
	}

	if translator.CallCount() != 2 {
		t.Fatalf("expected translator to be called twice, got %d", translator.CallCount())
	}
	if result.Failed != 2 {
		t.Fatalf("expected both entries to fail after retries, got %#v", result)
	}
	if result.Processed != 0 || result.Updated != 0 || result.Skipped != 0 {
		t.Fatalf("expected no successful updates after final failure, got %#v", result)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected one final failure message, got %#v", result.Messages)
	}

	entries, err := svc.store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	for _, entry := range entries.Items {
		if entry.TranslatedText != "" {
			t.Fatalf("expected failed entries to remain untranslated, got %#v", entry)
		}
	}
}
