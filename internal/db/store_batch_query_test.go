package db

import (
	"context"
	"path/filepath"
	"testing"

	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/textutil"
)

func seedBatchQueryTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	if err := store.ReplaceEntriesForArc(context.Background(), "script.arc", []model.Entry{
		{
			Type:       "talk",
			SourceArc:  "script.arc",
			SourceFile: "scene01.ks",
			SourceText: "alpha",
		},
		{
			Type:             "talk",
			SourceArc:        "script.arc",
			SourceFile:       "scene01.ks",
			SourceText:       "beta",
			TranslatedText:   "beta-zh",
			TranslatorStatus: "translated",
		},
		{
			Type:       "talk",
			SourceArc:  "other.arc",
			SourceFile: "scene02.ks",
			SourceText: "gamma",
		},
	}); err != nil {
		store.Close()
		t.Fatalf("seed entries: %v", err)
	}

	return store
}

func TestBatchUpdateEntryStatusByQueryRequiresFilter(t *testing.T) {
	store := seedBatchQueryTestStore(t)
	defer store.Close()

	_, err := store.BatchUpdateEntryStatusByQuery(model.FilterBatchStatusInput{
		Query:            model.EntryQuery{},
		TranslatorStatus: "reviewed",
	})
	if err == nil {
		t.Fatalf("expected batch status update without filters to fail")
	}
}

func TestBatchUpdateEntryStatusByQueryUpdatesFilteredRows(t *testing.T) {
	store := seedBatchQueryTestStore(t)
	defer store.Close()

	result, err := store.BatchUpdateEntryStatusByQuery(model.FilterBatchStatusInput{
		Query: model.EntryQuery{
			SourceArc: "script.arc",
		},
		TranslatorStatus: "reviewed",
	})
	if err != nil {
		t.Fatalf("batch status update failed: %v", err)
	}
	if result.Updated != 2 {
		t.Fatalf("expected 2 updated rows, got %d", result.Updated)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}

	statusByArc := map[string][]string{}
	for _, entry := range entries.Items {
		statusByArc[entry.SourceArc] = append(statusByArc[entry.SourceArc], entry.TranslatorStatus)
	}
	if statusByArc["script.arc"][0] != "reviewed" || statusByArc["script.arc"][1] != "reviewed" {
		t.Fatalf("expected script.arc entries to be reviewed, got %#v", statusByArc)
	}
	if statusByArc["other.arc"][0] != "new" {
		t.Fatalf("expected other.arc entry to remain new, got %#v", statusByArc)
	}
}

func TestDeleteEntriesByQueryDeletesFilteredRows(t *testing.T) {
	store := seedBatchQueryTestStore(t)
	defer store.Close()

	result, err := store.DeleteEntriesByQuery(model.EntryQuery{
		SourceArc: "script.arc",
	})
	if err != nil {
		t.Fatalf("delete filtered entries failed: %v", err)
	}
	if result.Deleted != 2 {
		t.Fatalf("expected 2 deleted rows, got %d", result.Deleted)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries.Items) != 1 || entries.Items[0].SourceArc != "other.arc" {
		t.Fatalf("unexpected remaining entries: %#v", entries.Items)
	}
}

func TestClearEntryTranslationsByQueryClearsFilteredTexts(t *testing.T) {
	store := seedBatchQueryTestStore(t)
	defer store.Close()

	result, err := store.ClearEntryTranslationsByQuery(model.EntryQuery{
		SourceArc: "script.arc",
	})
	if err != nil {
		t.Fatalf("clear filtered translations failed: %v", err)
	}
	if result.Updated != 2 {
		t.Fatalf("expected 2 updated rows, got %d", result.Updated)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}

	for _, entry := range entries.Items {
		if entry.SourceArc != "script.arc" {
			continue
		}
		if entry.TranslatedText != "" || entry.PolishedText != "" || entry.TranslatorStatus != "new" {
			t.Fatalf("expected cleared translation fields for script.arc, got %#v", entry)
		}
	}
}

func TestFindReusableTargetTextsReturnsOnlyUniqueTranslatedMatches(t *testing.T) {
	store := seedBatchQueryTestStore(t)
	defer store.Close()

	now := nowString()
	if _, err := store.db.Exec(`
INSERT INTO translation_entries(
	type, voice_id, role, source_arc, source_file, source_text,
	translated_text, polished_text, translator_status, created_at, updated_at
) VALUES
	('talk', '', '', 'reuse.arc', 'scene03.ks', 'hello', '你好', '', 'translated', ?, ?),
	('talk', '', '', 'reuse.arc', 'scene04.ks', 'hello', '你好', '', 'translated', ?, ?),
	('talk', '', '', 'reuse.arc', 'scene05.ks', 'bye', '再见', '', 'translated', ?, ?),
	('talk', '', '', 'reuse.arc', 'scene06.ks', 'bye', '拜拜', '', 'translated', ?, ?)
`, now, now, now, now, now, now, now, now); err != nil {
		t.Fatalf("seed reusable entries: %v", err)
	}

	matches, err := store.FindReusableTargetTexts([]model.Entry{
		{SourceText: "hello"},
		{SourceText: "bye"},
	}, "translated")
	if err != nil {
		t.Fatalf("find reusable translated texts: %v", err)
	}

	if matches["hello"] != "你好" {
		t.Fatalf("expected unique reusable translated text for hello, got %#v", matches)
	}
	if _, ok := matches["bye"]; ok {
		t.Fatalf("did not expect conflicting translated text for bye to be reused, got %#v", matches)
	}
}

func TestFindReusableTargetTextsUsesSourceAndTranslatedForPolishedMatches(t *testing.T) {
	store := seedBatchQueryTestStore(t)
	defer store.Close()

	now := nowString()
	if _, err := store.db.Exec(`
INSERT INTO translation_entries(
	type, voice_id, role, source_arc, source_file, source_text,
	translated_text, polished_text, translator_status, created_at, updated_at
) VALUES
	('talk', '', '', 'polish.arc', 'scene10.ks', 'alpha', '阿尔法', '阿尔法呀', 'polished', ?, ?),
	('talk', '', '', 'polish.arc', 'scene11.ks', 'alpha', '阿尔法', '阿尔法呀', 'polished', ?, ?),
	('talk', '', '', 'polish.arc', 'scene12.ks', 'alpha', '甲', '甲呀', 'polished', ?, ?),
	('talk', '', '', 'polish.arc', 'scene13.ks', 'beta', '贝塔', '贝塔一', 'polished', ?, ?),
	('talk', '', '', 'polish.arc', 'scene14.ks', 'beta', '贝塔', '贝塔二', 'polished', ?, ?)
`, now, now, now, now, now, now, now, now, now, now); err != nil {
		t.Fatalf("seed reusable polished entries: %v", err)
	}

	matches, err := store.FindReusableTargetTexts([]model.Entry{
		{SourceText: "alpha", TranslatedText: "阿尔法"},
		{SourceText: "alpha", TranslatedText: "甲"},
		{SourceText: "beta", TranslatedText: "贝塔"},
	}, "polished")
	if err != nil {
		t.Fatalf("find reusable polished texts: %v", err)
	}

	if matches["alpha\x00阿尔法"] != "阿尔法呀" {
		t.Fatalf("expected reusable polished text for alpha/阿尔法, got %#v", matches)
	}
	if matches["alpha\x00甲"] != "甲呀" {
		t.Fatalf("expected reusable polished text for alpha/甲, got %#v", matches)
	}
	if _, ok := matches["beta\x00贝塔"]; ok {
		t.Fatalf("did not expect conflicting polished text for beta/贝塔 to be reused, got %#v", matches)
	}
}

func TestCleanupInvisibleBlankSourceEntriesDeletesOnlyWhitespaceLikeRows(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cleanup.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := nowString()
	if _, err := store.db.Exec(`
INSERT INTO translation_entries(
	type, voice_id, role, source_arc, source_file, source_text,
	translated_text, polished_text, translator_status, created_at, updated_at
) VALUES
	('talk', '', '', 'cleanup.arc', 'scene01.ks', ?, '', '', 'new', ?, ?),
	('talk', '', '', 'cleanup.arc', 'scene01.ks', ?, '', '', 'new', ?, ?),
	('talk', '', '', 'cleanup.arc', 'scene01.ks', ?, '', '', 'new', ?, ?)
`, "\u180E\u200B\u00A0", now, now, "\r\n\t\u3000", now, now, "\u180E visible \u200B", now, now); err != nil {
		t.Fatalf("seed cleanup rows: %v", err)
	}

	deleted, err := store.CleanupInvisibleBlankSourceEntries()
	if err != nil {
		t.Fatalf("cleanup invisible blank source entries: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted rows, got %d", deleted)
	}

	entries, err := store.ListEntries(model.EntryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list entries after cleanup: %v", err)
	}
	if len(entries.Items) != 1 {
		t.Fatalf("expected 1 remaining row after cleanup, got %#v", entries.Items)
	}
	if textutil.NormalizeSourceText(entries.Items[0].SourceText) != "visible" {
		t.Fatalf("expected a non-blank visible source text to remain, got %#v", entries.Items[0])
	}
}
