package service

import (
	"testing"

	"COM3D2TranslateTool/internal/model"
)

func TestPrepareTranslationBatchDeduplicatesReusableSourceText(t *testing.T) {
	cache := newTranslationReuseCache(nil)
	batch := translationBatch{
		group: []model.Entry{
			{ID: 1, SourceText: "hello"},
			{ID: 2, SourceText: "hello"},
			{ID: 3, SourceText: "bye"},
		},
		start: 0,
		entries: []model.Entry{
			{ID: 1, SourceText: "hello"},
			{ID: 2, SourceText: "hello"},
			{ID: 3, SourceText: "bye"},
		},
	}

	plan := prepareTranslationBatch(batch, "translated", cache)
	if len(plan.translateEntries) != 2 {
		t.Fatalf("expected 2 unique translation entries, got %d", len(plan.translateEntries))
	}
	if len(plan.duplicateEntries["hello"]) != 1 || plan.duplicateEntries["hello"][0].ID != 2 {
		t.Fatalf("expected second hello entry to be treated as duplicate, got %#v", plan.duplicateEntries)
	}
	if len(plan.immediateUpdates) != 0 {
		t.Fatalf("did not expect immediate updates without cache hits, got %#v", plan.immediateUpdates)
	}
}

func TestPrepareTranslationBatchUsesReuseCacheBeforeTranslator(t *testing.T) {
	cache := newTranslationReuseCache(map[string]string{
		"bye": "再见",
	})
	batch := translationBatch{
		group: []model.Entry{
			{ID: 1, SourceText: "hello"},
			{ID: 2, SourceText: "bye"},
		},
		start: 0,
		entries: []model.Entry{
			{ID: 1, SourceText: "hello"},
			{ID: 2, SourceText: "bye"},
		},
	}

	plan := prepareTranslationBatch(batch, "translated", cache)
	if len(plan.translateEntries) != 1 || plan.translateEntries[0].entry.ID != 1 {
		t.Fatalf("expected only hello to be sent to translator, got %#v", plan.translateEntries)
	}
	if len(plan.immediateUpdates) != 1 || plan.immediateUpdates[0].ID != 2 || plan.immediateUpdates[0].TranslatedText != "再见" {
		t.Fatalf("expected cached bye translation to be applied immediately, got %#v", plan.immediateUpdates)
	}
}

func TestTranslationReuseKeyForPolishedIncludesTranslatedText(t *testing.T) {
	entry := model.Entry{
		SourceText:     "alpha",
		TranslatedText: "阿尔法",
	}

	if got := translationReuseKey(entry, "polished"); got != "alpha\x00阿尔法" {
		t.Fatalf("unexpected polished reuse key: %q", got)
	}
	if got := translationReuseKey(model.Entry{SourceText: "alpha"}, "polished"); got != "" {
		t.Fatalf("expected empty polished reuse key without translated text, got %q", got)
	}
}
