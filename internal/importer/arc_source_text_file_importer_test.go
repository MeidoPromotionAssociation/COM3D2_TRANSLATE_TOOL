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

func TestArcSourceTextFileImporterSupportsSourceOnlyAndTranslatedLines(t *testing.T) {
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
			SourceFile: "scene01.ks",
			SourceText: "原文A",
		},
		{
			Type:       "talk",
			SourceArc:  "script.arc",
			SourceFile: "scene02.ks",
			SourceText: "原文B",
		},
	}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	importPath := filepath.Join(tempDir, "script.txt")
	content := "\uFEFF原文A\t译文A\n原文B\n不存在\n"
	if err := os.WriteFile(importPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	importer := NewArcSourceTextFileImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        importPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.FilesProcessed != 1 || result.TotalLines != 3 || result.Updated != 1 || result.Skipped != 1 || result.Unmatched != 1 || result.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 2 {
		t.Fatalf("unexpected entry count: %#v", entries.Items)
	}
	if entries.Items[0].TranslatedText != "译文A" {
		t.Fatalf("unexpected imported translation: %#v", entries.Items)
	}
	if entries.Items[1].TranslatedText != "" {
		t.Fatalf("source-only line should not update translation: %#v", entries.Items)
	}
}

func TestArcSourceTextFileImporterSkipsAmbiguousTranslatedLine(t *testing.T) {
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
			SourceFile: "scene01.ks",
			SourceText: "重复原文",
		},
		{
			Type:       "talk",
			SourceArc:  "script.arc",
			SourceFile: "scene02.ks",
			SourceText: "重复原文",
		},
	}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	importPath := filepath.Join(tempDir, "script.arc.txt")
	if err := os.WriteFile(importPath, []byte("重复原文\t译文A\n"), 0o644); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	importer := NewArcSourceTextFileImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        importPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Updated != 0 || result.Skipped != 1 || result.Unmatched != 0 || len(result.Messages) == 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	if !strings.Contains(result.Messages[0], "ambiguous match") {
		t.Fatalf("expected ambiguous match message, got: %#v", result.Messages)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	for _, entry := range entries.Items {
		if entry.TranslatedText != "" {
			t.Fatalf("ambiguous line should not update entries: %#v", entries.Items)
		}
	}
}
