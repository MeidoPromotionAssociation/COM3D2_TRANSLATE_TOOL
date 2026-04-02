package exporter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
)

func TestTabTextExporterUsesPolishedBeforeTranslated(t *testing.T) {
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
	}, {
		Type:       "talk",
		SourceArc:  "script.arc",
		SourceFile: "scene01.ks",
		SourceText: "原文B",
	}}); err != nil {
		t.Fatalf("seed entries: %v", err)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if err := store.UpdateEntry(model.UpdateEntryInput{
		ID:             entries.Items[0].ID,
		TranslatedText: "译文A",
	}); err != nil {
		t.Fatalf("update translated text: %v", err)
	}
	if err := store.UpdateEntry(model.UpdateEntryInput{
		ID:             entries.Items[1].ID,
		TranslatedText: "译文B",
		PolishedText:   "润色B",
	}); err != nil {
		t.Fatalf("update polished text: %v", err)
	}

	outputPath := filepath.Join(tempDir, "output.txt")
	exporter := NewTabTextExporter(store)
	result, err := exporter.Export(context.Background(), model.ExportRequest{
		OutputPath:     outputPath,
		SkipEmptyFinal: true,
	})
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if result.Exported != 2 {
		t.Fatalf("unexpected export result: %#v", result)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "原文A\t译文A") {
		t.Fatalf("expected translated line in export, got %q", text)
	}
	if !strings.Contains(text, "原文B\t润色B") {
		t.Fatalf("expected polished line in export, got %q", text)
	}
}

func TestTabTextExporterExportsMoreThanFiftyThousandRows(t *testing.T) {
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

	const rowCount = 50005
	for index := 0; index < rowCount; index++ {
		inserted, err := session.InsertImportedEntry(
			"talk",
			"",
			"",
			"script.arc",
			"scene01.ks",
			fmt.Sprintf("source-%05d", index),
			fmt.Sprintf("target-%05d", index),
		)
		if err != nil {
			t.Fatalf("insert row %d: %v", index, err)
		}
		if !inserted {
			t.Fatalf("expected row %d to be inserted", index)
		}
	}
	if err := session.Commit(); err != nil {
		t.Fatalf("commit import session: %v", err)
	}

	outputPath := filepath.Join(tempDir, "output-large.txt")
	exporter := NewTabTextExporter(store)
	result, err := exporter.Export(context.Background(), model.ExportRequest{
		OutputPath:     outputPath,
		SkipEmptyFinal: true,
	})
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if result.Exported != rowCount || result.Skipped != 0 {
		t.Fatalf("unexpected export result: %#v", result)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "source-00000\ttarget-00000") {
		t.Fatalf("expected first line in export")
	}
	if !strings.Contains(text, "source-50004\ttarget-50004") {
		t.Fatalf("expected last line in export")
	}
	if lineCount := strings.Count(text, "\n"); lineCount != rowCount {
		t.Fatalf("expected %d exported lines, got %d", rowCount, lineCount)
	}
}

func TestTabTextExporterDeduplicatesBySourceTextUsingBestCandidate(t *testing.T) {
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
		translated string
	}{
		{sourceArc: "script_a.arc", sourceFile: "scene01.ks", sourceText: "shared", translated: "same-final"},
		{sourceArc: "script_b.arc", sourceFile: "scene02.ks", sourceText: "shared", translated: "same-final"},
		{sourceArc: "script_c.arc", sourceFile: "scene03.ks", sourceText: "shared", translated: "different-final"},
	}
	for _, row := range rows {
		inserted, err := session.InsertImportedEntry("talk", "", "", row.sourceArc, row.sourceFile, row.sourceText, row.translated)
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
		if entry.SourceArc != "script_c.arc" {
			continue
		}
		if err := store.UpdateEntry(model.UpdateEntryInput{
			ID:               entry.ID,
			TranslatedText:   "different-final",
			PolishedText:     "best-final",
			TranslatorStatus: "reviewed",
		}); err != nil {
			t.Fatalf("update preferred entry: %v", err)
		}
	}

	outputPath := filepath.Join(tempDir, "deduped-output.txt")
	exporter := NewTabTextExporter(store)
	result, err := exporter.Export(context.Background(), model.ExportRequest{
		OutputPath:     outputPath,
		SkipEmptyFinal: true,
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

	text := string(data)
	if lineCount := strings.Count(text, "\n"); lineCount != 1 {
		t.Fatalf("expected 1 exported line after dedupe, got %d: %q", lineCount, text)
	}
	if text != "shared\tbest-final\n" {
		t.Fatalf("expected the best candidate to win for a duplicated source text, got %q", text)
	}
}

func TestTabTextExporterUsesVoiceIDForPlayvoiceRows(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.ReplaceEntriesForArc(context.Background(), "voice.arc", []model.Entry{
		{
			Type:       "playvoice",
			VoiceID:    "MC_t2",
			SourceArc:  "voice.arc",
			SourceFile: "scene01.ks",
			SourceText: "comment text",
		},
		{
			Type:       "playvoice_notext",
			VoiceID:    "MC_t3",
			SourceArc:  "voice.arc",
			SourceFile: "scene01.ks",
			SourceText: "",
		},
	}); err != nil {
		t.Fatalf("seed voice rows: %v", err)
	}

	entries, err := store.ListEntries(model.EntryQuery{SourceArc: "voice.arc", Limit: 10})
	if err != nil {
		t.Fatalf("list voice rows: %v", err)
	}
	for _, entry := range entries.Items {
		switch entry.VoiceID {
		case "MC_t2":
			if err := store.UpdateEntry(model.UpdateEntryInput{
				ID:             entry.ID,
				TranslatedText: "voice-line-with-comment",
			}); err != nil {
				t.Fatalf("update playvoice row: %v", err)
			}
		case "MC_t3":
			if err := store.UpdateEntry(model.UpdateEntryInput{
				ID:             entry.ID,
				TranslatedText: "voice-line-no-comment",
			}); err != nil {
				t.Fatalf("update playvoice_notext row: %v", err)
			}
		}
	}

	outputPath := filepath.Join(tempDir, "voice-output.txt")
	exporter := NewTabTextExporter(store)
	result, err := exporter.Export(context.Background(), model.ExportRequest{
		OutputPath:     outputPath,
		SkipEmptyFinal: true,
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
	if !strings.Contains(text, "MC_t2\tvoice-line-with-comment\n") {
		t.Fatalf("expected playvoice row to export voice id, got %q", text)
	}
	if !strings.Contains(text, "MC_t3\tvoice-line-no-comment\n") {
		t.Fatalf("expected playvoice_notext row to export voice id, got %q", text)
	}
	if strings.Contains(text, "comment text\tvoice-line-with-comment") {
		t.Fatalf("playvoice rows should use voice id instead of source text, got %q", text)
	}
}

func TestTabTextExporterEscapesJATSpecialCharacters(t *testing.T) {
	tempDir := t.TempDir()
	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	sourceText := "Line 1\nLine 2\tC:\\Game"
	finalText := "第一行\t第二行\n路径 C:\\Game\\data"

	session, err := store.BeginImportSession(context.Background())
	if err != nil {
		t.Fatalf("begin import session: %v", err)
	}
	defer session.Rollback()

	inserted, err := session.InsertImportedEntry("talk", "", "", "script.arc", "scene01.ks", sourceText, finalText)
	if err != nil {
		t.Fatalf("insert row: %v", err)
	}
	if !inserted {
		t.Fatalf("expected row to be inserted")
	}
	if err := session.Commit(); err != nil {
		t.Fatalf("commit import session: %v", err)
	}

	outputPath := filepath.Join(tempDir, "escaped-output.txt")
	exporter := NewTabTextExporter(store)
	if _, err := exporter.Export(context.Background(), model.ExportRequest{
		OutputPath:     outputPath,
		SkipEmptyFinal: true,
	}); err != nil {
		t.Fatalf("export failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	text := string(data)
	if strings.Count(text, "\n") != 1 {
		t.Fatalf("expected escaped export to stay on one physical line, got %q", text)
	}
	if !strings.Contains(text, `Line 1\nLine 2\tC:\\Game`) {
		t.Fatalf("expected source text to be escaped for JAT parser, got %q", text)
	}
	if !strings.Contains(text, `第一行\t第二行\n路径 C:\\Game\\data`) {
		t.Fatalf("expected final text to be escaped for JAT parser, got %q", text)
	}

	exact := parseJATTextModuleFile(t, text)
	got, ok := exact[sourceText]
	if !ok {
		t.Fatalf("expected simulated JAT loader to resolve escaped source text")
	}
	if got != finalText {
		t.Fatalf("expected round-trip translation %q, got %q", finalText, got)
	}
}

func TestTabTextExporterSkipsSourcesWithReservedLeadingCharacters(t *testing.T) {
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
		source string
		final  string
	}{
		{source: ";comment-like", final: "ignored-a"},
		{source: "$regex-like", final: "ignored-b"},
		{source: "normal", final: "kept"},
	}
	for _, row := range rows {
		inserted, err := session.InsertImportedEntry("talk", "", "", "script.arc", "scene01.ks", row.source, row.final)
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

	outputPath := filepath.Join(tempDir, "reserved-output.txt")
	exporter := NewTabTextExporter(store)
	result, err := exporter.Export(context.Background(), model.ExportRequest{
		OutputPath:     outputPath,
		SkipEmptyFinal: true,
	})
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if result.Exported != 1 || result.Skipped != 2 {
		t.Fatalf("unexpected export result: %#v", result)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	text := string(data)
	if strings.Contains(text, ";comment-like") || strings.Contains(text, "$regex-like") {
		t.Fatalf("reserved-leading rows should be skipped entirely, got %q", text)
	}
	if text != "normal\tkept\n" {
		t.Fatalf("expected only the supported row to remain, got %q", text)
	}
}

func parseJATTextModuleFile(t *testing.T, content string) map[string]string {
	t.Helper()

	exact := make(map[string]string)

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		tabIndex := strings.IndexRune(line, '\t')
		if tabIndex < 0 {
			continue
		}

		original := unescapeJATField(line[:tabIndex])
		translation := unescapeJATField(line[tabIndex+1:])
		if original == "" || translation == "" {
			continue
		}

		exact[original] = translation
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan exported file: %v", err)
	}

	return exact
}

func unescapeJATField(value string) string {
	if value == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(value))

	for index := 0; index < len(value); {
		nextSlash := strings.IndexByte(value[index:], '\\')
		if nextSlash < 0 {
			builder.WriteString(value[index:])
			break
		}
		nextSlash += index
		if nextSlash == len(value)-1 {
			builder.WriteString(value[index:])
			break
		}

		builder.WriteString(value[index:nextSlash])
		switch value[nextSlash+1] {
		case '0':
			builder.WriteByte(0)
		case 'a':
			builder.WriteByte('\a')
		case 'b':
			builder.WriteByte('\b')
		case 't':
			builder.WriteByte('\t')
		case 'n':
			builder.WriteByte('\n')
		case 'v':
			builder.WriteByte('\v')
		case 'f':
			builder.WriteByte('\f')
		case 'r':
			builder.WriteByte('\r')
		case '\'':
			builder.WriteByte('\'')
		case '"':
			builder.WriteByte('"')
		case '\\':
			builder.WriteByte('\\')
		default:
			builder.WriteByte('\\')
			builder.WriteByte(value[nextSlash+1])
		}

		index = nextSlash + 2
	}

	return builder.String()
}
