package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
)

func TestTranslatedCSVImporterUpdatesAndInsertsUsingExistingArc(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{{
		Type:       "talk",
		SourceArc:  "script.arc",
		SourceFile: "001_a_minigame.ks",
		SourceText: "source-a",
	}}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	csvPath := filepath.Join(tempDir, "001_a_minigame_translated.csv")
	content := "\uFEFF" + translatedCSVHeaderSourceText + "," + ksExtractHeaderTargetText + "\n" +
		"source-a,target-a\n" +
		"source-b,target-b\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	importer := NewTranslatedCSVImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        csvPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Updated != 1 || result.Inserted != 1 || result.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 2 {
		t.Fatalf("unexpected entries: %#v", entries.Items)
	}
	if entries.Items[0].TranslatedText != "target-a" && entries.Items[1].TranslatedText != "target-a" {
		t.Fatalf("expected updated translation: %#v", entries.Items)
	}
	if entries.Items[0].SourceArc != "script.arc" || entries.Items[1].SourceArc != "script.arc" {
		t.Fatalf("expected source arc from existing db: %#v", entries.Items)
	}
}

func TestTranslatedCSVImporterInsertsWithEmptyArcWhenSourceFileUnknown(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	csvPath := filepath.Join(tempDir, "002_unknown_translated.csv")
	content := "\uFEFF" + translatedCSVHeaderSourceText + "," + ksExtractHeaderTargetText + "\n" +
		"source-a,target-a\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	importer := NewTranslatedCSVImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        csvPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Inserted != 1 || result.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 1 {
		t.Fatalf("unexpected entries: %#v", entries.Items)
	}
	if entries.Items[0].SourceArc != "" || entries.Items[0].SourceFile != "002_unknown.ks" {
		t.Fatalf("expected empty arc and derived source file: %#v", entries.Items)
	}
}

func TestTranslatedCSVImporterPreservesFileOrderAcrossParallelParsing(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{{
		Type:       "talk",
		SourceArc:  "script.arc",
		SourceFile: "001_scene.ks",
		SourceText: "source-a",
	}}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	firstDir := filepath.Join(tempDir, "a")
	secondDir := filepath.Join(tempDir, "b")
	if err := os.MkdirAll(firstDir, 0o755); err != nil {
		t.Fatalf("mkdir first dir: %v", err)
	}
	if err := os.MkdirAll(secondDir, 0o755); err != nil {
		t.Fatalf("mkdir second dir: %v", err)
	}

	contentA := "\uFEFF" + translatedCSVHeaderSourceText + "," + ksExtractHeaderTargetText + "\n" +
		"source-a,target-from-a\n"
	if err := os.WriteFile(filepath.Join(firstDir, "001_scene_translated.csv"), []byte(contentA), 0o644); err != nil {
		t.Fatalf("write first csv: %v", err)
	}

	contentB := "\uFEFF" + translatedCSVHeaderSourceText + "," + ksExtractHeaderTargetText + "\n" +
		"source-a,target-from-b\n"
	if err := os.WriteFile(filepath.Join(secondDir, "001_scene_translated.csv"), []byte(contentB), 0o644); err != nil {
		t.Fatalf("write second csv: %v", err)
	}

	importer := NewTranslatedCSVImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        tempDir,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.FilesProcessed != 2 || result.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 1 {
		t.Fatalf("unexpected entries: %#v", entries.Items)
	}
	if entries.Items[0].TranslatedText != "target-from-b" {
		t.Fatalf("expected lexical file order to be preserved, got %#v", entries.Items[0])
	}
}
