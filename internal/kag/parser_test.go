package kag

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseKSFileExtractsSupportedEntryTypes(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "scene01.ks")
	content := `@talk voice=H0_04530 name=[HF]
こんにちは|
世界
@hitret
@PlayVoice voice=V_0001
;再生コメント
@SubtitleDisplay text="字幕"
@ChoicesSet text="選択肢"
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp ks: %v", err)
	}

	entries, err := ParseKSFile(path, "script.arc")
	if err != nil {
		t.Fatalf("ParseKSFile failed: %v", err)
	}

	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	if entries[0].Type != "talk" || entries[0].VoiceID != "H0_04530" || entries[0].Role != "[HF]" {
		t.Fatalf("unexpected talk entry: %#v", entries[0])
	}
	if entries[0].SourceText != "こんにちは\n世界" {
		t.Fatalf("unexpected normalized talk text: %q", entries[0].SourceText)
	}
	if entries[1].Type != "playvoice" || entries[1].SourceText != "再生コメント" {
		t.Fatalf("unexpected playvoice entry: %#v", entries[1])
	}
	if entries[2].Type != "subtitle" || entries[2].SourceText != "字幕" {
		t.Fatalf("unexpected subtitle entry: %#v", entries[2])
	}
	if entries[3].Type != "choice" || entries[3].SourceText != "選択肢" {
		t.Fatalf("unexpected choice entry: %#v", entries[3])
	}
}

func TestParseKSFileNarrationAndPlayvoiceWithoutText(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "scene02.ks")
	content := `@talk
地の文
@hitret
@PlayVoice voice=V_0002
*label
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp ks: %v", err)
	}

	entries, err := ParseKSFile(path, "script.arc")
	if err != nil {
		t.Fatalf("ParseKSFile failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Type != "narration" || entries[0].SourceText != "地の文" {
		t.Fatalf("unexpected narration entry: %#v", entries[0])
	}
	if entries[1].Type != "playvoice_notext" || entries[1].VoiceID != "V_0002" {
		t.Fatalf("unexpected playvoice_notext entry: %#v", entries[1])
	}
}
