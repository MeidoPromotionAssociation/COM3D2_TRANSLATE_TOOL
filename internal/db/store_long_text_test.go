package db

import (
	"path/filepath"
	"strings"
	"testing"

	"COM3D2TranslateTool/internal/model"
)

func TestUpdateEntryPersistsLongTextsAndSearchesThem(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "long-text.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	longSource := strings.Repeat("原文段落。", 1200)
	longTranslated := strings.Repeat("译文段落。", 1200)

	if err := store.ReplaceEntriesForArc(t.Context(), "long.arc", []model.Entry{
		{
			Type:       "talk",
			SourceArc:  "long.arc",
			SourceFile: "long.ks",
			SourceText: longSource,
		},
	}); err != nil {
		t.Fatalf("seed long entry: %v", err)
	}

	listed, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list seeded entries: %v", err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("expected 1 seeded entry, got %#v", listed.Items)
	}

	entry := listed.Items[0]
	if err := store.UpdateEntry(model.UpdateEntryInput{
		ID:               entry.ID,
		TranslatedText:   longTranslated,
		PolishedText:     "",
		TranslatorStatus: "translated",
	}); err != nil {
		t.Fatalf("update long entry: %v", err)
	}

	updated, err := store.ListEntries(model.EntryQuery{
		Search: "译文段落。译文段落。",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("search long translated text: %v", err)
	}
	if len(updated.Items) != 1 {
		t.Fatalf("expected to find the updated long-text entry, got %#v", updated.Items)
	}
	if updated.Items[0].TranslatedText != longTranslated {
		t.Fatalf("expected long translated text to persist, got length=%d want=%d", len(updated.Items[0].TranslatedText), len(longTranslated))
	}
	if updated.Items[0].SourceText != longSource {
		t.Fatalf("expected long source text to persist, got length=%d want=%d", len(updated.Items[0].SourceText), len(longSource))
	}
}
