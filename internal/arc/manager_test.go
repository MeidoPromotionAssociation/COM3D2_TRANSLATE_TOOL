package arc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	msarc "github.com/MeidoPromotionAssociation/MeidoSerialization/serialization/COM3D2/arc"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
)

func writePackedArc(t *testing.T, rootDir string, arcFilename string, ksFilename string, ksContent string) string {
	t.Helper()

	arcSourceDir := filepath.Join(rootDir, arcFilename+"-source")
	if err := os.MkdirAll(arcSourceDir, 0o755); err != nil {
		t.Fatalf("create arc source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(arcSourceDir, ksFilename), []byte(ksContent), 0o644); err != nil {
		t.Fatalf("write ks file: %v", err)
	}

	arcPath := filepath.Join(rootDir, arcFilename)
	if err := msarc.Pack(arcSourceDir, arcPath); err != nil {
		t.Fatalf("pack arc: %v", err)
	}

	return arcPath
}

func writePackedArcFiles(t *testing.T, rootDir string, arcFilename string, files map[string]string) string {
	t.Helper()

	arcSourceDir := filepath.Join(rootDir, arcFilename+"-source")
	if err := os.MkdirAll(arcSourceDir, 0o755); err != nil {
		t.Fatalf("create arc source dir: %v", err)
	}

	for relativePath, content := range files {
		targetPath := filepath.Join(arcSourceDir, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			t.Fatalf("create parent dir for %s: %v", relativePath, err)
		}
		if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write file %s: %v", relativePath, err)
		}
	}

	arcPath := filepath.Join(rootDir, arcFilename)
	if err := msarc.Pack(arcSourceDir, arcPath); err != nil {
		t.Fatalf("pack arc: %v", err)
	}

	return arcPath
}

func TestParseArcKeepsArcFileOpenDuringExtraction(t *testing.T) {
	tempDir := t.TempDir()

	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	const (
		talkText   = "\u3053\u3093\u306b\u3061\u306f"
		choiceText = "\u9078\u629e\u80a2"
	)

	ksContent := "@talk voice=V_0001 name=[A]\n" + talkText + "\n@hitret\n@ChoicesSet text=\"" + choiceText + "\"\n"
	arcPath := writePackedArc(t, tempDir, "script.arc", "scene01.ks", ksContent)

	record, isNew, err := store.TouchArc(filepath.Base(arcPath), arcPath)
	if err != nil {
		t.Fatalf("touch arc: %v", err)
	}
	if !isNew {
		t.Fatalf("expected new arc record")
	}

	manager := NewManager(store)
	result, err := manager.parseArc(context.Background(), filepath.Join(tempDir, "work"), record)
	if err != nil {
		t.Fatalf("parseArc failed: %v", err)
	}
	if result.EntryCount != 2 {
		t.Fatalf("expected 2 parsed entries, got %d", result.EntryCount)
	}

	entries, err := store.ListEntries(model.EntryQuery{SourceArc: "script.arc", Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 2 {
		t.Fatalf("expected 2 stored entries, got %d", len(entries.Items))
	}

	var foundTalk bool
	var foundChoice bool
	for _, entry := range entries.Items {
		if entry.SourceFile != "scene01.ks" {
			t.Fatalf("unexpected source file: %#v", entry)
		}
		switch entry.Type {
		case "talk":
			foundTalk = entry.SourceText == talkText && entry.VoiceID == "V_0001" && entry.Role == "[A]"
		case "choice":
			foundChoice = entry.SourceText == choiceText
		}
	}

	if !foundTalk {
		t.Fatalf("expected talk entry in stored data: %#v", entries.Items)
	}
	if !foundChoice {
		t.Fatalf("expected choice entry in stored data: %#v", entries.Items)
	}
}

func TestReparseFailedOnlyProcessesFailedArcs(t *testing.T) {
	tempDir := t.TempDir()

	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	const talkText = "\u3053\u3093\u306b\u3061\u306f"
	failedArcPath := writePackedArc(t, tempDir, "failed.arc", "failed.ks", "@talk voice=V_0002 name=[B]\n"+talkText+"\n@hitret\n")
	parsedArcPath := writePackedArc(t, tempDir, "parsed.arc", "parsed.ks", "@talk\nignored\n@hitret\n")

	failedRecord, isNew, err := store.TouchArc(filepath.Base(failedArcPath), failedArcPath)
	if err != nil {
		t.Fatalf("touch failed arc: %v", err)
	}
	if !isNew {
		t.Fatalf("expected failed arc to be new")
	}
	if err := store.UpdateArcStatus(failedRecord.ID, "failed", "previous error", false); err != nil {
		t.Fatalf("mark failed arc status: %v", err)
	}

	parsedRecord, isNew, err := store.TouchArc(filepath.Base(parsedArcPath), parsedArcPath)
	if err != nil {
		t.Fatalf("touch parsed arc: %v", err)
	}
	if !isNew {
		t.Fatalf("expected parsed arc to be new")
	}
	if err := store.UpdateArcStatus(parsedRecord.ID, "parsed", "", true); err != nil {
		t.Fatalf("mark parsed arc status: %v", err)
	}
	if err := os.Remove(parsedArcPath); err != nil {
		t.Fatalf("remove parsed arc to catch unintended reparses: %v", err)
	}

	manager := NewManager(store)
	result, err := manager.ReparseFailed(context.Background(), model.Settings{
		WorkDir: filepath.Join(tempDir, "work"),
	})
	if err != nil {
		t.Fatalf("ReparseFailed failed: %v", err)
	}

	if result.TotalFailed != 1 || result.ReparsedCount != 1 || result.FailedCount != 0 {
		t.Fatalf("unexpected batch reparse result: %#v", result)
	}

	arcs, err := store.ListArcs()
	if err != nil {
		t.Fatalf("list arcs: %v", err)
	}
	statusByFilename := make(map[string]string, len(arcs))
	for _, arcFile := range arcs {
		statusByFilename[arcFile.Filename] = arcFile.Status
	}
	if statusByFilename["failed.arc"] != "parsed" {
		t.Fatalf("failed arc status was not updated: %#v", statusByFilename)
	}
	if statusByFilename["parsed.arc"] != "parsed" {
		t.Fatalf("parsed arc should remain untouched: %#v", statusByFilename)
	}

	entries, err := store.ListEntries(model.EntryQuery{SourceArc: "failed.arc", Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 1 || entries.Items[0].SourceText != talkText {
		t.Fatalf("unexpected reparsed entries: %#v", entries.Items)
	}
}

func TestReparseAllProcessesParsedArcsToo(t *testing.T) {
	tempDir := t.TempDir()

	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	firstArcPath := writePackedArc(t, tempDir, "first.arc", "first.ks", "@PlayVoice voice=V_0001\n")
	secondArcPath := writePackedArc(t, tempDir, "second.arc", "second.ks", "@talk voice=V_0002 name=[B]\nhello\n@hitret\n")

	firstRecord, isNew, err := store.TouchArc(filepath.Base(firstArcPath), firstArcPath)
	if err != nil {
		t.Fatalf("touch first arc: %v", err)
	}
	if !isNew {
		t.Fatalf("expected first arc to be new")
	}

	secondRecord, isNew, err := store.TouchArc(filepath.Base(secondArcPath), secondArcPath)
	if err != nil {
		t.Fatalf("touch second arc: %v", err)
	}
	if !isNew {
		t.Fatalf("expected second arc to be new")
	}

	if err := store.UpdateArcStatus(firstRecord.ID, "parsed", "", true); err != nil {
		t.Fatalf("mark first arc parsed: %v", err)
	}
	if err := store.UpdateArcStatus(secondRecord.ID, "parsed", "", true); err != nil {
		t.Fatalf("mark second arc parsed: %v", err)
	}

	manager := NewManager(store)
	result, err := manager.ReparseAll(context.Background(), model.Settings{
		WorkDir: filepath.Join(tempDir, "work"),
	})
	if err != nil {
		t.Fatalf("ReparseAll failed: %v", err)
	}

	if result.TotalArcs != 2 || result.ReparsedCount != 2 || result.FailedCount != 0 || result.SkippedCount != 0 {
		t.Fatalf("unexpected reparse all result: %#v", result)
	}

	firstEntries, err := store.ListEntries(model.EntryQuery{SourceArc: "first.arc", Limit: 10})
	if err != nil {
		t.Fatalf("list first arc entries: %v", err)
	}
	if len(firstEntries.Items) != 1 || firstEntries.Items[0].Type != "playvoice_notext" {
		t.Fatalf("expected playvoice_notext to be inserted after full reparse, got %#v", firstEntries.Items)
	}

	secondEntries, err := store.ListEntries(model.EntryQuery{SourceArc: "second.arc", Limit: 10})
	if err != nil {
		t.Fatalf("list second arc entries: %v", err)
	}
	if len(secondEntries.Items) != 1 || secondEntries.Items[0].Type != "talk" {
		t.Fatalf("expected talk entry after full reparse, got %#v", secondEntries.Items)
	}
}

func TestParseArcDeduplicatesExactDuplicateEntries(t *testing.T) {
	tempDir := t.TempDir()

	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	const (
		choiceA = "\u9078\u629e\u80a2A"
		choiceB = "\u9078\u629e\u80a2B"
	)

	ksContent := "@ChoicesSet text=\"" + choiceA + "\"\n" +
		"@ChoicesSet text=\"" + choiceA + "\"\n" +
		"@ChoicesSet text=\"" + choiceB + "\"\n"
	arcPath := writePackedArc(t, tempDir, "duplicate.arc", "duplicate.ks", ksContent)

	record, isNew, err := store.TouchArc(filepath.Base(arcPath), arcPath)
	if err != nil {
		t.Fatalf("touch arc: %v", err)
	}
	if !isNew {
		t.Fatalf("expected new arc record")
	}

	manager := NewManager(store)
	result, err := manager.parseArc(context.Background(), filepath.Join(tempDir, "work"), record)
	if err != nil {
		t.Fatalf("parseArc failed: %v", err)
	}
	if result.EntryCount != 2 {
		t.Fatalf("expected 2 deduplicated entries, got %d", result.EntryCount)
	}
	if result.Message != "duplicate.arc parsed, 2 entries stored, 1 duplicates skipped" {
		t.Fatalf("unexpected parse message: %q", result.Message)
	}

	entries, err := store.ListEntries(model.EntryQuery{SourceArc: "duplicate.arc", Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 2 {
		t.Fatalf("expected 2 stored entries, got %d", len(entries.Items))
	}
}

func TestFindVoiceFilePathMatchesBasenameOnly(t *testing.T) {
	match, err := findVoiceFilePath([]string{
		"voice/MC_t2.ogg",
		"script/scene01.ks",
	}, "MC_t2")
	if err != nil {
		t.Fatalf("findVoiceFilePath failed: %v", err)
	}
	if match != "voice/MC_t2.ogg" {
		t.Fatalf("expected basename match voice/MC_t2.ogg, got %q", match)
	}
}

func TestFindVoiceFilePathPrefersOggWhenMultipleExtensionsExist(t *testing.T) {
	match, err := findVoiceFilePath([]string{
		"voice_a/MC_t2.ogg",
		"voice_b/MC_t2.wav",
	}, "MC_t2")
	if err != nil {
		t.Fatalf("findVoiceFilePath failed: %v", err)
	}
	if match != "voice_a/MC_t2.ogg" {
		t.Fatalf("expected ogg match to win, got %q", match)
	}
}

func TestExtractVoiceFileFallsBackToOtherArc(t *testing.T) {
	tempDir := t.TempDir()

	store, err := db.Open(filepath.Join(tempDir, "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	scriptArcPath := writePackedArcFiles(t, tempDir, "script.arc", map[string]string{
		"scene01.ks": "@PlayVoice voice=S6_cm10th_61251\n",
	})
	voiceArcPath := writePackedArcFiles(t, tempDir, "voice.arc", map[string]string{
		"Sound/Voice/S6_cm10th_61251.ogg": "voice-bytes",
	})

	scriptRecord, isNew, err := store.TouchArc(filepath.Base(scriptArcPath), scriptArcPath)
	if err != nil {
		t.Fatalf("touch script arc: %v", err)
	}
	if !isNew {
		t.Fatalf("expected script arc to be new")
	}

	_, isNew, err = store.TouchArc(filepath.Base(voiceArcPath), voiceArcPath)
	if err != nil {
		t.Fatalf("touch voice arc: %v", err)
	}
	if !isNew {
		t.Fatalf("expected voice arc to be new")
	}

	manager := NewManager(store)
	outputPath, cleanup, err := manager.ExtractVoiceFile(filepath.Join(tempDir, "work"), scriptRecord, "S6_cm10th_61251")
	if err != nil {
		t.Fatalf("ExtractVoiceFile failed: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read extracted voice file: %v", err)
	}
	if string(data) != "voice-bytes" {
		t.Fatalf("unexpected extracted data: %q", string(data))
	}
}
