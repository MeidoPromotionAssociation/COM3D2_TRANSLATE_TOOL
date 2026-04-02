package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
)

func TestArcKSFolderTextImporterImportsByArcAndFile(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{{
		Type:       "talk",
		SourceArc:  "script.arc",
		SourceFile: "scene01.ks",
		SourceText: "原文A",
	}}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	importRoot := filepath.Join(tempDir, "import")
	if err := os.MkdirAll(filepath.Join(importRoot, "script.arc_extracted"), 0o755); err != nil {
		t.Fatalf("mkdir import root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(importRoot, "script.arc_extracted", "scene01.txt"), []byte("原文A\t\t译文A\n"), 0o644); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	importer := NewArcKSFolderTextImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        importRoot,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Updated != 1 || result.ErrorLines != 0 || result.Unmatched != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 1 || entries.Items[0].TranslatedText != "译文A" {
		t.Fatalf("unexpected imported entries: %#v", entries.Items)
	}
}
