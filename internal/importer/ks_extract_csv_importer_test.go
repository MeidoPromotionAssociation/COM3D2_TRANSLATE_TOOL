package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
)

func TestKSExtractCSVImporterImportsByFullIdentity(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{{
		Type:       "talk",
		VoiceID:    "H0_04530",
		Role:       "[HF]",
		SourceArc:  "script.arc",
		SourceFile: "scene01.ks",
		SourceText: "source-a",
	}}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	csvPath := filepath.Join(tempDir, "ks_extract.csv")
	content := "\uFEFF" + ksExtractHeaderType + ",voice_id," + ksExtractHeaderRole + "," + ksExtractHeaderSourceArc + "," + ksExtractHeaderSourceFile + "," + ksExtractHeaderSourceText + "," + ksExtractHeaderTargetText + "\n" +
		"talk,H0_04530,[HF],script.arc,scene01.ks,source-a,target-a\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	importer := NewKSExtractCSVImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        csvPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Updated != 1 || result.Unmatched != 0 || result.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 1 || entries.Items[0].TranslatedText != "target-a" {
		t.Fatalf("unexpected imported entries: %#v", entries.Items)
	}
}

func TestKSExtractCSVImporterAllowsEmptyOptionalColumns(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{
		{
			Type:       "choice",
			SourceArc:  "script.arc",
			SourceFile: "ck_menu_000.ks",
			SourceText: "choice-a",
		},
		{
			Type:       "choice",
			SourceArc:  "script.arc",
			SourceFile: "ck_menu_000.ks",
			SourceText: "choice-b",
		},
	}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	csvPath := filepath.Join(tempDir, "ks_extract_choice.csv")
	content := "\uFEFF" + ksExtractHeaderType + ",voice_id," + ksExtractHeaderRole + "," + ksExtractHeaderSourceArc + "," + ksExtractHeaderSourceFile + "," + ksExtractHeaderSourceText + "," + ksExtractHeaderTargetText + "\n" +
		"choice,,,script.arc,ck_menu_000.ks,choice-a,\n" +
		"choice,,,script.arc,ck_menu_000.ks,choice-b,translated-b\n" +
		"choice,,,script.arc,ck_menu_000.ks,missing,\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	importer := NewKSExtractCSVImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        csvPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Inserted != 1 || result.Updated != 1 || result.Skipped != 1 || result.Unmatched != 0 || result.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 3 {
		t.Fatalf("unexpected entry count: %#v", entries.Items)
	}
	if entries.Items[0].TranslatedText != "translated-b" && entries.Items[1].TranslatedText != "translated-b" {
		t.Fatalf("expected translated entry, got: %#v", entries.Items)
	}
}

func TestKSExtractCSVImporterAllowsEmptyTypeValue(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{{
		Type:       "choice",
		SourceArc:  "script.arc",
		SourceFile: "ck_menu_000.ks",
		SourceText: "choice-c",
	}}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	csvPath := filepath.Join(tempDir, "ks_extract_empty_type.csv")
	content := "\uFEFF" + ksExtractHeaderType + ",voice_id," + ksExtractHeaderRole + "," + ksExtractHeaderSourceArc + "," + ksExtractHeaderSourceFile + "," + ksExtractHeaderSourceText + "," + ksExtractHeaderTargetText + "\n" +
		",,,script.arc,ck_menu_000.ks,choice-c,translated-c\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	importer := NewKSExtractCSVImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        csvPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Updated != 1 || result.Unmatched != 0 || result.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 1 || entries.Items[0].TranslatedText != "translated-c" {
		t.Fatalf("unexpected imported entries: %#v", entries.Items)
	}
}

func TestKSExtractCSVImporterInsertsUnmatchedRows(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	csvPath := filepath.Join(tempDir, "ks_extract_insert.csv")
	content := "\uFEFF" + ksExtractHeaderType + ",voice_id," + ksExtractHeaderRole + "," + ksExtractHeaderSourceArc + "," + ksExtractHeaderSourceFile + "," + ksExtractHeaderSourceText + "," + ksExtractHeaderTargetText + "\n" +
		"choice,,,script.arc,ck_menu_000.ks,choice-new,\n" +
		"talk,V_0001,[A],script2,scene01.txt,source-new,target-new\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	importer := NewKSExtractCSVImporter(store)
	result, err := importer.Import(context.Background(), model.ImportRequest{
		RootDir:        csvPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Inserted != 2 || result.Unmatched != 0 || result.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", result)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 2 {
		t.Fatalf("unexpected inserted entries: %#v", entries.Items)
	}

	if entries.Items[0].SourceArc != "script.arc" && entries.Items[1].SourceArc != "script.arc" {
		t.Fatalf("expected normalized arc name, got: %#v", entries.Items)
	}
	if entries.Items[0].SourceFile != "scene01.ks" && entries.Items[1].SourceFile != "scene01.ks" {
		t.Fatalf("expected normalized source file, got: %#v", entries.Items)
	}
}
