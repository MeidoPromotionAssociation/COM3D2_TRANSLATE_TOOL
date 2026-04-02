package importer

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/exporter"
	"COM3D2TranslateTool/internal/model"
)

func TestEntryJSONLRoundTripPreservesEntryFields(t *testing.T) {
	tempDir := t.TempDir()

	sourceStore, err := db.Open(filepath.Join(tempDir, "source.sqlite"))
	if err != nil {
		t.Fatalf("open source store: %v", err)
	}
	defer sourceStore.Close()

	if err := sourceStore.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{
		{
			Type:       "talk",
			VoiceID:    "voice-001",
			Role:       "hero",
			SourceArc:  "script.arc",
			SourceFile: "scene01.ks",
			SourceText: "line-1",
		},
		{
			Type:       "",
			VoiceID:    "",
			Role:       "",
			SourceArc:  "",
			SourceFile: "scene02.ks",
			SourceText: "line-2",
		},
	}); err != nil {
		t.Fatalf("seed source entries: %v", err)
	}

	sourceEntries, err := sourceStore.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list source entries: %v", err)
	}
	if len(sourceEntries.Items) != 2 {
		t.Fatalf("expected 2 source entries, got %#v", sourceEntries.Items)
	}
	if err := sourceStore.UpdateEntry(model.UpdateEntryInput{
		ID:               sourceEntries.Items[0].ID,
		TranslatedText:   "translated-1",
		PolishedText:     "polished-1",
		TranslatorStatus: "reviewed",
	}); err != nil {
		t.Fatalf("update source entry 1: %v", err)
	}
	if err := sourceStore.UpdateEntry(model.UpdateEntryInput{
		ID:               sourceEntries.Items[1].ID,
		TranslatedText:   "translated-2",
		PolishedText:     "",
		TranslatorStatus: "translated",
	}); err != nil {
		t.Fatalf("update source entry 2: %v", err)
	}

	exportPath := filepath.Join(tempDir, "entries.jsonl")
	entryExporter := exporter.NewEntryJSONLExporter(sourceStore)
	result, err := entryExporter.Export(context.Background(), model.ExportRequest{
		OutputPath: exportPath,
	})
	if err != nil {
		t.Fatalf("export jsonl: %v", err)
	}
	if result.Exported != 2 {
		t.Fatalf("expected 2 exported entries, got %#v", result)
	}

	targetStore, err := db.Open(filepath.Join(tempDir, "target.sqlite"))
	if err != nil {
		t.Fatalf("open target store: %v", err)
	}
	defer targetStore.Close()

	entryImporter := NewEntryJSONLImporter(targetStore)
	importResult, err := entryImporter.Import(context.Background(), model.ImportRequest{
		Importer:       entryImporter.Name(),
		RootDir:        exportPath,
		AllowOverwrite: true,
	})
	if err != nil {
		t.Fatalf("import jsonl: %v", err)
	}
	if importResult.Inserted != 2 || importResult.Updated != 0 || importResult.ErrorLines != 0 {
		t.Fatalf("unexpected import result: %#v", importResult)
	}

	expected, err := sourceStore.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list exported source entries: %v", err)
	}
	actual, err := targetStore.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list imported target entries: %v", err)
	}

	if got, want := normalizeEntriesForCompare(actual.Items), normalizeEntriesForCompare(expected.Items); !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip entries mismatch\nwant: %#v\ngot: %#v", want, got)
	}
}

func normalizeEntriesForCompare(entries []model.Entry) []model.Entry {
	normalized := make([]model.Entry, len(entries))
	copy(normalized, entries)
	for index := range normalized {
		normalized[index].ID = 0
	}
	slices.SortFunc(normalized, func(left, right model.Entry) int {
		leftKey := left.Type + "\x00" + left.VoiceID + "\x00" + left.Role + "\x00" + left.SourceArc + "\x00" + left.SourceFile + "\x00" + left.SourceText
		rightKey := right.Type + "\x00" + right.VoiceID + "\x00" + right.Role + "\x00" + right.SourceArc + "\x00" + right.SourceFile + "\x00" + right.SourceText
		switch {
		case leftKey < rightKey:
			return -1
		case leftKey > rightKey:
			return 1
		default:
			return 0
		}
	})
	return normalized
}
