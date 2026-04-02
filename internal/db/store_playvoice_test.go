package db

import (
	"context"
	"path/filepath"
	"testing"

	"COM3D2TranslateTool/internal/model"
)

func TestReplaceEntriesForArcKeepsPlayvoiceNoTextRowsAndPreservesRecognizedSourceText(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	seed := []model.Entry{
		{
			Type:       "playvoice_notext",
			VoiceID:    "V_0001",
			SourceArc:  "script.arc",
			SourceFile: "scene01.ks",
			SourceText: "",
		},
	}

	if err := store.ReplaceEntriesForArc(ctx, "script.arc", seed); err != nil {
		t.Fatalf("seed arc entries: %v", err)
	}

	list, err := store.ListEntries(model.EntryQuery{SourceArc: "script.arc", Limit: 10})
	if err != nil {
		t.Fatalf("list entries after seed: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 playvoice_notext row, got %#v", list.Items)
	}
	if list.Items[0].Type != "playvoice_notext" || list.Items[0].SourceText != "" {
		t.Fatalf("unexpected stored entry after seed: %#v", list.Items[0])
	}

	if err := store.UpdateEntrySourceText(list.Items[0].ID, "recognized-line"); err != nil {
		t.Fatalf("update recognized source text: %v", err)
	}

	if err := store.ReplaceEntriesForArc(ctx, "script.arc", seed); err != nil {
		t.Fatalf("reparse arc entries: %v", err)
	}

	list, err = store.ListEntries(model.EntryQuery{SourceArc: "script.arc", Limit: 10})
	if err != nil {
		t.Fatalf("list entries after reparse: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 playvoice_notext row after reparse, got %#v", list.Items)
	}
	if list.Items[0].Type != "playvoice_notext" {
		t.Fatalf("expected playvoice_notext after reparse, got %#v", list.Items[0])
	}
	if list.Items[0].VoiceID != "V_0001" {
		t.Fatalf("expected voice id to stay V_0001, got %#v", list.Items[0])
	}
	if list.Items[0].SourceText != "recognized-line" {
		t.Fatalf("expected recognized source text to survive reparse, got %#v", list.Items[0])
	}
}
