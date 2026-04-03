package exporter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
)

func TestVoiceSubtitleTextExporterExportsVoiceIDToFinalText(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{
		{
			Type:       "talk",
			VoiceID:    "H0_13123",
			Role:       "[A]",
			SourceArc:  "script.arc",
			SourceFile: "scene01.ks",
			SourceText: "comment-a",
		},
		{
			Type:       "playvoice_notext",
			VoiceID:    "MC_t2",
			SourceArc:  "script.arc",
			SourceFile: "scene01.ks",
			SourceText: "",
		},
		{
			Type:       "talk",
			VoiceID:    "",
			Role:       "[B]",
			SourceArc:  "script.arc",
			SourceFile: "scene01.ks",
			SourceText: "no-voice",
		},
	}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	entries, err := store.ListEntries(model.EntryQuery{SourceArc: "script.arc", Limit: 20})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	for _, entry := range entries.Items {
		switch entry.VoiceID {
		case "H0_13123":
			if err := store.UpdateEntry(model.UpdateEntryInput{
				ID:             entry.ID,
				TranslatedText: "subtitle-a",
				PolishedText:   "should-not-be-used",
			}); err != nil {
				t.Fatalf("update H0_13123: %v", err)
			}
		case "MC_t2":
			if err := store.UpdateEntry(model.UpdateEntryInput{
				ID:             entry.ID,
				TranslatedText: "subtitle-b",
			}); err != nil {
				t.Fatalf("update MC_t2: %v", err)
			}
		default:
			if err := store.UpdateEntry(model.UpdateEntryInput{
				ID:             entry.ID,
				TranslatedText: "should-not-export",
			}); err != nil {
				t.Fatalf("update no-voice row: %v", err)
			}
		}
	}

	outputPath := filepath.Join(tempDir, "voice-subtitles.txt")
	exporter := NewVoiceSubtitleTextExporter(store)
	result, err := exporter.Export(context.Background(), model.ExportRequest{
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if result.Exported != 2 || result.Skipped != 0 {
		t.Fatalf("unexpected export result: %#v", result)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "H0_13123\tshould-not-be-used\n") {
		t.Fatalf("expected H0_13123 subtitle mapping, got %q", text)
	}
	if !strings.Contains(text, "MC_t2\tsubtitle-b\n") {
		t.Fatalf("expected MC_t2 subtitle mapping, got %q", text)
	}
	if strings.Contains(text, "H0_13123\tsubtitle-a\n") {
		t.Fatalf("voice subtitle export should prefer polished_text over translated_text, got %q", text)
	}
	if strings.Contains(text, "should-not-export") {
		t.Fatalf("rows without voice_id should not be exported, got %q", text)
	}
}

func TestVoiceSubtitleTextExporterDeduplicatesByVoiceID(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	session, err := store.BeginImportSession(context.Background())
	if err != nil {
		t.Fatalf("begin import session: %v", err)
	}
	defer session.Rollback()

	rows := []struct {
		sourceArc  string
		sourceFile string
		sourceText string
		voiceID    string
		translated string
	}{
		{sourceArc: "a.arc", sourceFile: "a.ks", sourceText: "line-a", voiceID: "MC_t2", translated: "first"},
		{sourceArc: "b.arc", sourceFile: "b.ks", sourceText: "line-b", voiceID: "MC_t2", translated: "second"},
	}
	for _, row := range rows {
		inserted, err := session.InsertImportedEntry("talk", row.voiceID, "", row.sourceArc, row.sourceFile, row.sourceText, row.translated)
		if err != nil {
			t.Fatalf("insert row %+v: %v", row, err)
		}
		if !inserted {
			t.Fatalf("expected row %+v to be inserted", row)
		}
	}
	if err := session.Commit(); err != nil {
		t.Fatalf("commit import session: %v", err)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	for _, entry := range entries.Items {
		if entry.SourceArc != "b.arc" {
			continue
		}
		if err := store.UpdateEntry(model.UpdateEntryInput{
			ID:               entry.ID,
			TranslatedText:   "best-translated",
			PolishedText:     "best-polished",
			TranslatorStatus: "reviewed",
		}); err != nil {
			t.Fatalf("update preferred row: %v", err)
		}
	}

	outputPath := filepath.Join(tempDir, "voice-deduped.txt")
	exporter := NewVoiceSubtitleTextExporter(store)
	result, err := exporter.Export(context.Background(), model.ExportRequest{
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if result.Exported != 1 {
		t.Fatalf("expected 1 deduplicated row, got %#v", result)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(data) != "MC_t2\tbest-polished\n" {
		t.Fatalf("expected best voice-id candidate to win, got %q", string(data))
	}
}
