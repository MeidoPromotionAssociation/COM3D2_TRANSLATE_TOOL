package importer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
)

func TestTranslationExtractJSONImporterUpdatesAllListedScriptFiles(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{
		{
			Type:       "talk",
			SourceArc:  "script.arc",
			SourceFile: "b_christmas2024_0002.ks",
			SourceText: "あっ、にゃんにゃん♪",
		},
		{
			Type:       "talk",
			SourceArc:  "script.arc",
			SourceFile: "d1_rrc_00880.ks",
			SourceText: "あっ、にゃんにゃん♪",
		},
	}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	jsonPath := filepath.Join(tempDir, "translation_extract.json")
	content := "\uFEFF" + `{
  "あっ、にゃんにゃん♪": {
    "Official": "Oh, look! It's a cat!",
    "scriptFiles": [
      "b_christmas2024_0002.ks",
      "d1_rrc_00880.ks"
    ]
  }
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}

	importer := NewTranslationExtractJSONImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        jsonPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.FilesProcessed != 1 || result.Updated != 2 || result.Unmatched != 0 || result.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 2 {
		t.Fatalf("unexpected entries: %#v", entries.Items)
	}
	for _, entry := range entries.Items {
		if entry.TranslatedText != "Oh, look! It's a cat!" {
			t.Fatalf("expected translated text to be applied to all matching ks files: %#v", entries.Items)
		}
	}
}

func TestTranslationExtractJSONImporterCountsCompletelyUnmatchedEntryOnce(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	jsonPath := filepath.Join(tempDir, "translation_extract.json")
	content := `{
  "missing line": {
    "Official": "translated",
    "scriptFiles": [
      "missing_a.ks",
      "missing_b.ks"
    ]
  }
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}

	importer := NewTranslationExtractJSONImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        jsonPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.FilesProcessed != 1 || result.Updated != 0 || result.Unmatched != 1 || result.ErrorLines != 0 || result.Inserted != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 0 {
		t.Fatalf("expected no inserted entries, got %#v", entries.Items)
	}
}

func TestTranslationExtractJSONImporterSkipsAmbiguousMatches(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{
		{
			Type:       "talk",
			SourceArc:  "script.arc",
			SourceFile: "scene.ks",
			SourceText: "same line",
		},
		{
			Type:       "choice",
			SourceArc:  "script.arc",
			SourceFile: "scene.ks",
			SourceText: "same line",
		},
	}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	jsonPath := filepath.Join(tempDir, "translation_extract.json")
	content := `{
  "same line": {
    "Official": "translated",
    "scriptFiles": [
      "scene.ks"
    ]
  }
}`
	if err := os.WriteFile(jsonPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}

	importer := NewTranslationExtractJSONImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        jsonPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Updated != 0 || result.Skipped != 1 || result.Unmatched != 0 || result.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	if len(result.Messages) == 0 || !strings.Contains(result.Messages[0], "ambiguous match") {
		t.Fatalf("expected ambiguous match message, got %#v", result.Messages)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	for _, entry := range entries.Items {
		if entry.TranslatedText != "" {
			t.Fatalf("expected ambiguous entries to remain unchanged: %#v", entries.Items)
		}
	}
}
