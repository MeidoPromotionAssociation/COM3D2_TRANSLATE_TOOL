package service

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/translation"
)

type fakeLongTextTranslator struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeLongTextTranslator) Name() string {
	return "baidu-translate"
}

func (f *fakeLongTextTranslator) Translate(_ context.Context, req translation.Request) ([]translation.Result, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	results := make([]translation.Result, 0, len(req.Items))
	for _, item := range req.Items {
		results = append(results, translation.Result{
			ID:   item.ID,
			Text: item.SourceText,
		})
	}
	return results, nil
}

func (f *fakeLongTextTranslator) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestRunTranslationSplitsLongAutomaticEntry(t *testing.T) {
	baseDir := t.TempDir()
	store, err := db.Open(filepath.Join(baseDir, "long-translation.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.SaveSettings(model.Settings{
		Translation: model.TranslationSettings{
			ActiveTranslator: "baidu-translate",
		},
	}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	longSource := strings.Repeat("長文テキスト。", 900)
	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{
		{
			Type:       "talk",
			SourceArc:  "script.arc",
			SourceFile: "scene01.ks",
			SourceText: longSource,
		},
	}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	translator := &fakeLongTextTranslator{}
	svc := &Service{
		baseDir: baseDir,
		store:   store,
		translators: map[string]translation.Translator{
			"baidu-translate": translator,
		},
	}

	result, err := svc.RunTranslation(context.Background(), model.TranslateRequest{
		Translator:       "baidu-translate",
		TargetField:      "translated",
		UntranslatedOnly: true,
	})
	if err != nil {
		t.Fatalf("run translation: %v", err)
	}

	if translator.CallCount() < 2 {
		t.Fatalf("expected long entry to be split into multiple translator calls, got %d", translator.CallCount())
	}
	if result.Updated != 1 || result.Failed != 0 {
		t.Fatalf("unexpected translate result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 1 {
		t.Fatalf("expected 1 entry, got %#v", entries.Items)
	}
	if entries.Items[0].TranslatedText != longSource {
		t.Fatalf("expected chunked translation to be reassembled exactly, got length=%d want=%d", len(entries.Items[0].TranslatedText), len(longSource))
	}
}
