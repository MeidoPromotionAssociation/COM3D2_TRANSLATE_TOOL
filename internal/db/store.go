package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/textutil"

	_ "modernc.org/sqlite"
)

const timestampLayout = time.RFC3339
const reuseLookupChunkSize = 200

type Store struct {
	db *sql.DB
}

type ImportSession struct {
	tx                     *sql.Tx
	selectEntriesByArcFile *sql.Stmt
	selectEntriesByArc     *sql.Stmt
	updateTranslatedEntry  *sql.Stmt
	insertEntry            *sql.Stmt
	updateFullEntry        *sql.Stmt
	insertFullEntry        *sql.Stmt
	findSourceArcsByFile   *sql.Stmt
	arcFileEntries         map[string]map[string][]*importEntry
	arcEntries             map[string]map[string][]*importEntry
	sourceFileArcCache     map[string][]string
}

type TranslatedCSVFileState struct {
	session      *ImportSession
	sourceFile   string
	sourceArcs   []string
	entriesByArc map[string]map[string][]*importEntry
}

type TranslatedCSVApplyResult struct {
	Inserted      int
	Updated       int
	Skipped       int
	AmbiguousArcs []string
}

type preservedEntry struct {
	SourceText     string
	TranslatedText string
	PolishedText   string
	Status         string
	CreatedAt      string
}

type importEntry struct {
	ID             int64
	Type           string
	VoiceID        string
	Role           string
	SourceArc      string
	SourceFile     string
	SourceText     string
	TranslatedText string
	PolishedText   string
	Status         string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.Exec(`
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
`); err != nil {
		_ = db.Close()
		return nil, err
	}

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) BeginImportSession(ctx context.Context) (*ImportSession, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	session := &ImportSession{
		tx:                 tx,
		arcFileEntries:     make(map[string]map[string][]*importEntry),
		arcEntries:         make(map[string]map[string][]*importEntry),
		sourceFileArcCache: make(map[string][]string),
	}
	if session.selectEntriesByArcFile, err = tx.Prepare(`
SELECT id, type, voice_id, role, source_text, translated_text, polished_text, translator_status
FROM translation_entries
WHERE source_arc = ? AND source_file = ?
ORDER BY id ASC
`); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if session.selectEntriesByArc, err = tx.Prepare(`
SELECT id, type, voice_id, role, source_file, source_text, translated_text, polished_text, translator_status
FROM translation_entries
WHERE source_arc = ?
ORDER BY id ASC
`); err != nil {
		_ = session.Close()
		_ = tx.Rollback()
		return nil, err
	}
	if session.updateTranslatedEntry, err = tx.Prepare(`
UPDATE translation_entries
SET translated_text = ?, translator_status = ?, updated_at = ?
WHERE id = ?
`); err != nil {
		_ = session.Close()
		_ = tx.Rollback()
		return nil, err
	}
	if session.insertEntry, err = tx.Prepare(`
INSERT OR IGNORE INTO translation_entries(
	type, voice_id, role, source_arc, source_file, source_text,
	translated_text, polished_text, translator_status, created_at, updated_at
) VALUES(?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?)
`); err != nil {
		_ = session.Close()
		_ = tx.Rollback()
		return nil, err
	}
	if session.updateFullEntry, err = tx.Prepare(`
UPDATE translation_entries
SET translated_text = ?, polished_text = ?, translator_status = ?, created_at = ?, updated_at = ?
WHERE id = ?
`); err != nil {
		_ = session.Close()
		_ = tx.Rollback()
		return nil, err
	}
	if session.insertFullEntry, err = tx.Prepare(`
INSERT OR IGNORE INTO translation_entries(
	type, voice_id, role, source_arc, source_file, source_text,
	translated_text, polished_text, translator_status, created_at, updated_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`); err != nil {
		_ = session.Close()
		_ = tx.Rollback()
		return nil, err
	}
	if session.findSourceArcsByFile, err = tx.Prepare(`
SELECT DISTINCT source_arc
FROM translation_entries
WHERE source_file = ?
ORDER BY source_arc ASC
`); err != nil {
		_ = session.Close()
		_ = tx.Rollback()
		return nil, err
	}

	return session, nil
}

func (s *ImportSession) Close() error {
	var closeErr error
	for _, stmt := range []*sql.Stmt{
		s.selectEntriesByArcFile,
		s.selectEntriesByArc,
		s.updateTranslatedEntry,
		s.insertEntry,
		s.updateFullEntry,
		s.insertFullEntry,
		s.findSourceArcsByFile,
	} {
		if stmt == nil {
			continue
		}
		if err := stmt.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (s *ImportSession) Rollback() error {
	if s == nil || s.tx == nil {
		return nil
	}
	defer s.Close()
	err := s.tx.Rollback()
	s.tx = nil
	if errors.Is(err, sql.ErrTxDone) {
		return nil
	}
	return err
}

func (s *ImportSession) Commit() error {
	if s == nil || s.tx == nil {
		return nil
	}
	err := s.tx.Commit()
	s.tx = nil
	closeErr := s.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func importArcFileKey(sourceArc, sourceFile string) string {
	return sourceArc + "\x00" + sourceFile
}

func (s *ImportSession) loadArcFileEntries(sourceArc, sourceFile string) (map[string][]*importEntry, error) {
	key := importArcFileKey(sourceArc, sourceFile)
	if cached, ok := s.arcFileEntries[key]; ok {
		return cached, nil
	}

	rows, err := s.selectEntriesByArcFile.Query(sourceArc, sourceFile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byText := make(map[string][]*importEntry)
	for rows.Next() {
		entry := &importEntry{
			SourceArc:  sourceArc,
			SourceFile: sourceFile,
		}
		if err := rows.Scan(
			&entry.ID,
			&entry.Type,
			&entry.VoiceID,
			&entry.Role,
			&entry.SourceText,
			&entry.TranslatedText,
			&entry.PolishedText,
			&entry.Status,
		); err != nil {
			return nil, err
		}
		byText[entry.SourceText] = append(byText[entry.SourceText], entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.arcFileEntries[key] = byText
	return byText, nil
}

func (s *ImportSession) loadArcEntries(sourceArc string) (map[string][]*importEntry, error) {
	if cached, ok := s.arcEntries[sourceArc]; ok {
		return cached, nil
	}

	rows, err := s.selectEntriesByArc.Query(sourceArc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byText := make(map[string][]*importEntry)
	for rows.Next() {
		entry := &importEntry{SourceArc: sourceArc}
		if err := rows.Scan(
			&entry.ID,
			&entry.Type,
			&entry.VoiceID,
			&entry.Role,
			&entry.SourceFile,
			&entry.SourceText,
			&entry.TranslatedText,
			&entry.PolishedText,
			&entry.Status,
		); err != nil {
			return nil, err
		}
		byText[entry.SourceText] = append(byText[entry.SourceText], entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.arcEntries[sourceArc] = byText
	return byText, nil
}

func filterImportEntries(candidates []*importEntry, entryType, voiceID, role string) []*importEntry {
	if strings.TrimSpace(entryType) == "" && strings.TrimSpace(voiceID) == "" && strings.TrimSpace(role) == "" {
		return candidates
	}

	filtered := make([]*importEntry, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(entryType) != "" && candidate.Type != entryType {
			continue
		}
		if strings.TrimSpace(voiceID) != "" && candidate.VoiceID != voiceID {
			continue
		}
		if strings.TrimSpace(role) != "" && candidate.Role != role {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func appendCachedImportEntry(index map[string][]*importEntry, entry *importEntry) {
	if index == nil {
		return
	}
	index[entry.SourceText] = append(index[entry.SourceText], entry)
}

func exactImportEntries(candidates []*importEntry, entryType, voiceID, role string) []*importEntry {
	filtered := make([]*importEntry, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Type != entryType {
			continue
		}
		if candidate.VoiceID != voiceID {
			continue
		}
		if candidate.Role != role {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func appendUniqueString(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func (s *Store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS arc_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			filename TEXT NOT NULL UNIQUE,
			path TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'new',
			last_error TEXT NOT NULL DEFAULT '',
			discovered_at TEXT NOT NULL,
			parsed_at TEXT NOT NULL DEFAULT '',
			last_scanned_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS translation_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			voice_id TEXT NOT NULL,
			role TEXT NOT NULL,
			source_arc TEXT NOT NULL,
			source_file TEXT NOT NULL,
			source_text TEXT NOT NULL,
			translated_text TEXT NOT NULL DEFAULT '',
			polished_text TEXT NOT NULL DEFAULT '',
			translator_status TEXT NOT NULL DEFAULT 'new',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(type, voice_id, role, source_arc, source_file, source_text)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_arc_files_status ON arc_files(status)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_arc ON translation_entries(source_arc)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_file ON translation_entries(source_file)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_type ON translation_entries(type)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_status ON translation_entries(translator_status)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_source_text ON translation_entries(source_text)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_source_text_translated ON translation_entries(source_text, translated_text)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_export_order ON translation_entries(source_arc, source_file, id)`,
	}

	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}

	return nil
}

func nowString() string {
	return time.Now().Format(timestampLayout)
}

func sanitizeStatus(requested, translated, polished string) string {
	switch strings.TrimSpace(requested) {
	case "reviewed":
		return "reviewed"
	case "polished":
		if polished != "" {
			return "polished"
		}
	case "translated":
		if translated != "" {
			return "translated"
		}
	case "new":
		return "new"
	}

	if polished != "" {
		return "polished"
	}
	if translated != "" {
		return "translated"
	}
	return "new"
}

func uniqueEntryKey(entry model.Entry) string {
	return strings.Join([]string{
		entry.Type,
		entry.VoiceID,
		entry.Role,
		entry.SourceArc,
		entry.SourceFile,
		normalizeEntrySourceText(entry.SourceText),
	}, "\x00")
}

func playVoiceNoTextFallbackKey(entryType, voiceID, role, sourceArc, sourceFile string) string {
	return strings.Join([]string{
		strings.TrimSpace(entryType),
		strings.TrimSpace(voiceID),
		strings.TrimSpace(role),
		strings.TrimSpace(sourceArc),
		strings.TrimSpace(sourceFile),
	}, "\x00")
}

func allowsEmptyEntrySourceText(entry model.Entry) bool {
	return entry.Type == "playvoice_notext" && strings.TrimSpace(entry.VoiceID) != ""
}

func normalizeEntrySourceText(value string) string {
	return textutil.NormalizeSourceText(value)
}

func normalizeImportedEntryRecord(entry model.Entry) model.Entry {
	entry.Type = strings.TrimSpace(entry.Type)
	entry.VoiceID = strings.TrimSpace(entry.VoiceID)
	entry.Role = strings.TrimSpace(entry.Role)
	entry.SourceArc = strings.TrimSpace(entry.SourceArc)
	entry.SourceFile = strings.TrimSpace(entry.SourceFile)
	entry.SourceText = normalizeEntrySourceText(entry.SourceText)
	entry.TranslatorStatus = strings.TrimSpace(entry.TranslatorStatus)
	entry.CreatedAt = strings.TrimSpace(entry.CreatedAt)
	entry.UpdatedAt = strings.TrimSpace(entry.UpdatedAt)

	if entry.CreatedAt == "" {
		entry.CreatedAt = nowString()
	}
	if entry.UpdatedAt == "" {
		entry.UpdatedAt = entry.CreatedAt
	}
	entry.TranslatorStatus = sanitizeStatus(entry.TranslatorStatus, entry.TranslatedText, entry.PolishedText)
	return entry
}

func (s *Store) GetSettings() (model.Settings, error) {
	rows, err := s.db.Query(`SELECT key, value FROM app_settings`)
	if err != nil {
		return model.Settings{}, err
	}
	defer rows.Close()

	values := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return model.Settings{}, err
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return model.Settings{}, err
	}

	translationSettings := model.DefaultTranslationSettings()
	if raw := strings.TrimSpace(values["translation_settings"]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &translationSettings); err != nil {
			return model.Settings{}, err
		}
	}

	return model.Settings{
		ArcScanDir:  values["arc_scan_dir"],
		WorkDir:     values["work_dir"],
		ImportDir:   values["import_dir"],
		ExportDir:   values["export_dir"],
		Translation: model.NormalizeTranslationSettings(translationSettings),
	}, nil
}

func (s *Store) SaveSettings(settings model.Settings) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	translationJSON, err := json.Marshal(model.NormalizeTranslationSettings(settings.Translation))
	if err != nil {
		return err
	}

	items := map[string]string{
		"arc_scan_dir":         settings.ArcScanDir,
		"work_dir":             settings.WorkDir,
		"import_dir":           settings.ImportDir,
		"export_dir":           settings.ExportDir,
		"translation_settings": string(translationJSON),
	}

	for key, value := range items {
		if _, err := tx.Exec(`
INSERT INTO app_settings(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value
`, key, value); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func scanArc(rowScanner interface{ Scan(dest ...any) error }) (model.ArcFile, error) {
	var item model.ArcFile
	err := rowScanner.Scan(
		&item.ID,
		&item.Filename,
		&item.Path,
		&item.Status,
		&item.LastError,
		&item.DiscoveredAt,
		&item.ParsedAt,
		&item.LastScannedAt,
	)
	return item, err
}

func scanEntry(rowScanner interface{ Scan(dest ...any) error }) (model.Entry, error) {
	var item model.Entry
	err := rowScanner.Scan(
		&item.ID,
		&item.Type,
		&item.VoiceID,
		&item.Role,
		&item.SourceArc,
		&item.SourceFile,
		&item.SourceText,
		&item.TranslatedText,
		&item.PolishedText,
		&item.TranslatorStatus,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (s *Store) TouchArc(filename, path string) (model.ArcFile, bool, error) {
	row := s.db.QueryRow(`
SELECT id, filename, path, status, last_error, discovered_at, parsed_at, last_scanned_at
FROM arc_files
WHERE filename = ?
`, filename)

	item, err := scanArc(row)
	if err == nil {
		item.Path = path
		item.LastScannedAt = nowString()
		if _, err := s.db.Exec(`
UPDATE arc_files
SET path = ?, last_scanned_at = ?
WHERE id = ?
`, path, item.LastScannedAt, item.ID); err != nil {
			return model.ArcFile{}, false, err
		}
		return item, false, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return model.ArcFile{}, false, err
	}

	now := nowString()
	res, err := s.db.Exec(`
INSERT INTO arc_files(filename, path, status, last_error, discovered_at, parsed_at, last_scanned_at)
VALUES(?, ?, 'new', '', ?, '', ?)
`, filename, path, now, now)
	if err != nil {
		return model.ArcFile{}, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.ArcFile{}, false, err
	}

	return model.ArcFile{
		ID:            id,
		Filename:      filename,
		Path:          path,
		Status:        "new",
		LastError:     "",
		DiscoveredAt:  now,
		ParsedAt:      "",
		LastScannedAt: now,
	}, true, nil
}

func (s *Store) UpdateArcStatus(id int64, status, lastError string, parsed bool) error {
	parsedAt := ""
	if parsed {
		parsedAt = nowString()
	}

	_, err := s.db.Exec(`
UPDATE arc_files
SET status = ?, last_error = ?, parsed_at = ?
WHERE id = ?
`, status, lastError, parsedAt, id)
	return err
}

func (s *Store) ListArcs() ([]model.ArcFile, error) {
	rows, err := s.db.Query(`
SELECT id, filename, path, status, last_error, discovered_at, parsed_at, last_scanned_at
FROM arc_files
ORDER BY last_scanned_at DESC, filename ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.ArcFile, 0)
	for rows.Next() {
		item, err := scanArc(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *Store) ListArcsByStatus(status string) ([]model.ArcFile, error) {
	rows, err := s.db.Query(`
SELECT id, filename, path, status, last_error, discovered_at, parsed_at, last_scanned_at
FROM arc_files
WHERE status = ?
ORDER BY last_scanned_at DESC, filename ASC
`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.ArcFile, 0)
	for rows.Next() {
		item, err := scanArc(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *Store) GetArcByID(id int64) (model.ArcFile, error) {
	row := s.db.QueryRow(`
SELECT id, filename, path, status, last_error, discovered_at, parsed_at, last_scanned_at
FROM arc_files
WHERE id = ?
`, id)
	return scanArc(row)
}

func (s *Store) GetArcByFilename(filename string) (model.ArcFile, error) {
	row := s.db.QueryRow(`
SELECT id, filename, path, status, last_error, discovered_at, parsed_at, last_scanned_at
FROM arc_files
WHERE filename = ?
`, filename)
	return scanArc(row)
}

func (s *Store) ReplaceEntriesForArc(ctx context.Context, arcName string, entries []model.Entry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	existing := map[string]preservedEntry{}
	playVoiceNoTextExisting := map[string]preservedEntry{}
	rows, err := tx.QueryContext(ctx, `
SELECT type, voice_id, role, source_arc, source_file, source_text, translated_text, polished_text, translator_status, created_at
FROM translation_entries
WHERE source_arc = ?
`, arcName)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item model.Entry
		var preserved preservedEntry
		if err := rows.Scan(
			&item.Type,
			&item.VoiceID,
			&item.Role,
			&item.SourceArc,
			&item.SourceFile,
			&item.SourceText,
			&preserved.TranslatedText,
			&preserved.PolishedText,
			&preserved.Status,
			&preserved.CreatedAt,
		); err != nil {
			rows.Close()
			return err
		}
		preserved.SourceText = item.SourceText
		existing[uniqueEntryKey(item)] = preserved
		if item.Type == "playvoice_notext" && strings.TrimSpace(item.VoiceID) != "" {
			playVoiceNoTextExisting[playVoiceNoTextFallbackKey(item.Type, item.VoiceID, item.Role, item.SourceArc, item.SourceFile)] = preserved
		}
	}
	rows.Close()

	if _, err := tx.ExecContext(ctx, `DELETE FROM translation_entries WHERE source_arc = ?`, arcName); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO translation_entries(
	type, voice_id, role, source_arc, source_file, source_text,
	translated_text, polished_text, translator_status, created_at, updated_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := nowString()
	for _, entry := range entries {
		entry.SourceText = normalizeEntrySourceText(entry.SourceText)
		preserved := existing[uniqueEntryKey(entry)]
		if entry.Type == "playvoice_notext" && entry.SourceText == "" {
			if fallback, ok := playVoiceNoTextExisting[playVoiceNoTextFallbackKey(entry.Type, entry.VoiceID, entry.Role, entry.SourceArc, entry.SourceFile)]; ok {
				preserved = fallback
				entry.SourceText = fallback.SourceText
			}
		}
		if entry.SourceText == "" && !allowsEmptyEntrySourceText(entry) {
			continue
		}

		translated := preserved.TranslatedText
		polished := preserved.PolishedText
		status := sanitizeStatus(preserved.Status, translated, polished)
		createdAt := preserved.CreatedAt
		if createdAt == "" {
			createdAt = now
		}

		if _, err := stmt.ExecContext(
			ctx,
			entry.Type,
			entry.VoiceID,
			entry.Role,
			entry.SourceArc,
			entry.SourceFile,
			entry.SourceText,
			translated,
			polished,
			status,
			createdAt,
			now,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func buildWhere(query model.EntryQuery) (string, []any) {
	var clauses []string
	var args []any

	if q := strings.TrimSpace(query.Search); q != "" {
		pattern := "%" + q + "%"
		clauses = append(clauses, `(source_text LIKE ? OR translated_text LIKE ? OR polished_text LIKE ? OR role LIKE ? OR voice_id LIKE ? OR source_file LIKE ? OR source_arc LIKE ?)`)
		args = append(args, pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	}
	if query.SourceArc != "" {
		clauses = append(clauses, `source_arc = ?`)
		args = append(args, query.SourceArc)
	}
	if query.SourceFile != "" {
		clauses = append(clauses, `source_file = ?`)
		args = append(args, query.SourceFile)
	}
	if query.Type != "" {
		clauses = append(clauses, `type = ?`)
		args = append(args, query.Type)
	}
	if query.Status != "" {
		clauses = append(clauses, `translator_status = ?`)
		args = append(args, query.Status)
	}
	if query.UntranslatedOnly {
		clauses = append(clauses, `translated_text = '' AND polished_text = ''`)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func hasFilteredScope(query model.EntryQuery) bool {
	return strings.TrimSpace(query.Search) != "" ||
		strings.TrimSpace(query.SourceArc) != "" ||
		strings.TrimSpace(query.SourceFile) != "" ||
		strings.TrimSpace(query.Type) != "" ||
		strings.TrimSpace(query.Status) != "" ||
		query.UntranslatedOnly
}

func buildFilteredScopeWhere(query model.EntryQuery) (string, []any, error) {
	if !hasFilteredScope(query) {
		return "", nil, fmt.Errorf("at least one filter is required for this batch operation")
	}
	whereSQL, args := buildWhere(query)
	if whereSQL == "" {
		return "", nil, fmt.Errorf("at least one filter is required for this batch operation")
	}
	return whereSQL, args, nil
}

func buildTranslationWhere(query model.EntryQuery, targetField string, allowOverwrite bool) (string, []any) {
	whereSQL, args := buildWhere(query)
	clauses := []string{`source_text <> ''`}
	if strings.TrimSpace(targetField) == "polished" {
		// Polishing requires an existing translation as input.
		clauses = append(clauses, `translated_text <> ''`)
	}

	if !allowOverwrite {
		targetColumn := "translated_text"
		if strings.TrimSpace(targetField) == "polished" {
			targetColumn = "polished_text"
		}
		clauses = append(clauses, targetColumn+` = ''`)
	}

	if len(clauses) == 0 {
		return whereSQL, args
	}
	extra := strings.Join(clauses, " AND ")
	if whereSQL == "" {
		return " WHERE " + extra, args
	}
	return whereSQL + " AND " + extra, args
}

func exportEntryQuery(req model.ExportRequest) model.EntryQuery {
	return model.EntryQuery{
		Search:           req.Search,
		SourceArc:        req.SourceArc,
		SourceFile:       req.SourceFile,
		Type:             req.Type,
		Status:           req.Status,
		UntranslatedOnly: req.UntranslatedOnly,
	}
}

func (s *Store) ListEntries(query model.EntryQuery) (model.EntryList, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 50000 {
		limit = 50000
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}

	whereSQL, args := buildWhere(query)

	countQuery := `SELECT COUNT(*) FROM translation_entries` + whereSQL
	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return model.EntryList{}, err
	}

	listQuery := `
SELECT id, type, voice_id, role, source_arc, source_file, source_text,
       translated_text, polished_text, translator_status, created_at, updated_at
FROM translation_entries` + whereSQL + `
ORDER BY source_arc ASC, source_file ASC, id ASC
LIMIT ? OFFSET ?`

	rows, err := s.db.Query(listQuery, append(args, limit, offset)...)
	if err != nil {
		return model.EntryList{}, err
	}
	defer rows.Close()

	items := make([]model.Entry, 0)
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return model.EntryList{}, err
		}
		items = append(items, item)
	}

	return model.EntryList{Items: items, Total: total}, rows.Err()
}

func (s *Store) CountEntriesForTranslation(query model.EntryQuery, targetField string, allowOverwrite bool) (int, error) {
	whereSQL, args := buildTranslationWhere(query, targetField, allowOverwrite)
	countQuery := `SELECT COUNT(*) FROM translation_entries` + whereSQL

	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Store) ListEntriesForTranslation(query model.EntryQuery, targetField string, allowOverwrite bool, limit, offset int) ([]model.Entry, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 50000 {
		limit = 50000
	}
	if offset < 0 {
		offset = 0
	}

	whereSQL, args := buildTranslationWhere(query, targetField, allowOverwrite)
	listQuery := `
SELECT id, type, voice_id, role, source_arc, source_file, source_text,
       translated_text, polished_text, translator_status, created_at, updated_at
FROM translation_entries` + whereSQL + `
ORDER BY source_arc ASC, source_file ASC, id ASC
LIMIT ? OFFSET ?`

	rows, err := s.db.Query(listQuery, append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Entry, 0, limit)
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func buildSourceRecognitionWhere(query model.EntryQuery, allowOverwrite bool) (string, []any) {
	whereSQL, args := buildWhere(query)
	clauses := []string{
		`type = 'playvoice_notext'`,
		`voice_id <> ''`,
	}
	if !allowOverwrite {
		clauses = append(clauses, `source_text = ''`)
	}

	extra := strings.Join(clauses, " AND ")
	if whereSQL == "" {
		return " WHERE " + extra, args
	}
	return whereSQL + " AND " + extra, args
}

func (s *Store) CountEntriesForSourceRecognition(query model.EntryQuery, allowOverwrite bool) (int, error) {
	whereSQL, args := buildSourceRecognitionWhere(query, allowOverwrite)
	countQuery := `SELECT COUNT(*) FROM translation_entries` + whereSQL

	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Store) ListEntriesForSourceRecognition(query model.EntryQuery, allowOverwrite bool, limit, offset int) ([]model.Entry, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 50000 {
		limit = 50000
	}
	if offset < 0 {
		offset = 0
	}

	whereSQL, args := buildSourceRecognitionWhere(query, allowOverwrite)
	listQuery := `
SELECT id, type, voice_id, role, source_arc, source_file, source_text,
       translated_text, polished_text, translator_status, created_at, updated_at
FROM translation_entries` + whereSQL + `
ORDER BY source_arc ASC, source_file ASC, id ASC
LIMIT ? OFFSET ?`

	rows, err := s.db.Query(listQuery, append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Entry, 0, limit)
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) FindReusableTargetTexts(entries []model.Entry, targetField string) (map[string]string, error) {
	if strings.TrimSpace(targetField) == "polished" {
		return s.findReusablePolishedTexts(entries)
	}
	return s.findReusableTranslatedTexts(entries)
}

func (s *Store) findReusableTranslatedTexts(entries []model.Entry) (map[string]string, error) {
	sourceTexts := uniqueSourceTexts(entries)
	if len(sourceTexts) == 0 {
		return map[string]string{}, nil
	}

	candidates := make(map[string]string, len(sourceTexts))
	conflicts := make(map[string]struct{})
	for start := 0; start < len(sourceTexts); start += reuseLookupChunkSize {
		end := start + reuseLookupChunkSize
		if end > len(sourceTexts) {
			end = len(sourceTexts)
		}

		chunk := sourceTexts[start:end]
		query := `
SELECT source_text, translated_text
FROM translation_entries
WHERE translated_text <> ''
  AND source_text IN (` + sqlPlaceholders(len(chunk)) + `)
ORDER BY source_text ASC, updated_at DESC, id DESC`

		rows, err := s.db.Query(query, stringsToAny(chunk)...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var sourceText string
			var translatedText string
			if err := rows.Scan(&sourceText, &translatedText); err != nil {
				rows.Close()
				return nil, err
			}
			addReusableCandidate(candidates, conflicts, sourceText, translatedText)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	return candidates, nil
}

func (s *Store) findReusablePolishedTexts(entries []model.Entry) (map[string]string, error) {
	sourceTexts := uniqueSourceTexts(entries)
	if len(sourceTexts) == 0 {
		return map[string]string{}, nil
	}

	candidates := make(map[string]string, len(sourceTexts))
	conflicts := make(map[string]struct{})
	for start := 0; start < len(sourceTexts); start += reuseLookupChunkSize {
		end := start + reuseLookupChunkSize
		if end > len(sourceTexts) {
			end = len(sourceTexts)
		}

		chunk := sourceTexts[start:end]
		query := `
SELECT source_text, translated_text, polished_text
FROM translation_entries
WHERE polished_text <> ''
  AND translated_text <> ''
  AND source_text IN (` + sqlPlaceholders(len(chunk)) + `)
ORDER BY source_text ASC, updated_at DESC, id DESC`

		rows, err := s.db.Query(query, stringsToAny(chunk)...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var sourceText string
			var translatedText string
			var polishedText string
			if err := rows.Scan(&sourceText, &translatedText, &polishedText); err != nil {
				rows.Close()
				return nil, err
			}
			addReusableCandidate(candidates, conflicts, polishedReuseKey(sourceText, translatedText), polishedText)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	return candidates, nil
}

func uniqueSourceTexts(entries []model.Entry) []string {
	seen := make(map[string]struct{}, len(entries))
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		sourceText := normalizeEntrySourceText(entry.SourceText)
		if sourceText == "" {
			continue
		}
		if _, ok := seen[sourceText]; ok {
			continue
		}
		seen[sourceText] = struct{}{}
		values = append(values, sourceText)
	}
	return values
}

func addReusableCandidate(candidates map[string]string, conflicts map[string]struct{}, key, value string) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return
	}
	if _, conflicted := conflicts[key]; conflicted {
		return
	}
	if existing, ok := candidates[key]; ok {
		if existing != value {
			delete(candidates, key)
			conflicts[key] = struct{}{}
		}
		return
	}
	candidates[key] = value
}

func sqlPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func stringsToAny(values []string) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
}

func polishedReuseKey(sourceText, translatedText string) string {
	return normalizeEntrySourceText(sourceText) + "\x00" + translatedText
}

func cleanupSourceTextSQLExpression(column string) string {
	expression := column
	for _, current := range textutil.SourceTextCleanupRunes() {
		expression = fmt.Sprintf("REPLACE(%s, char(%d), '')", expression, current)
	}
	return expression
}

func (s *Store) CleanupInvisibleBlankSourceEntries() (int, error) {
	res, err := s.db.Exec(`
DELETE FROM translation_entries
WHERE ` + cleanupSourceTextSQLExpression("source_text") + ` = ''`)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(rowsAffected), nil
}

func (s *Store) StreamDistinctTabTextRows(ctx context.Context, req model.ExportRequest, yield func(sourceText, finalText string) error) error {
	whereSQL, args := buildWhere(exportEntryQuery(req))

	outerClauses := []string{"row_rank = 1"}
	if req.SkipEmptyFinal {
		outerClauses = append(outerClauses, "final_text <> ''")
	}
	outerWhere := "WHERE " + strings.Join(outerClauses, " AND ")

	exportQuery := `
SELECT export_source, final_text
FROM (
	SELECT export_source, final_text
	FROM (
		SELECT CASE
		         WHEN type IN ('playvoice', 'playvoice_notext') AND voice_id <> '' THEN voice_id
		         ELSE source_text
		       END AS export_source,
		       CASE
		         WHEN polished_text <> '' THEN polished_text
		         ELSE translated_text
		       END AS final_text,
		       ROW_NUMBER() OVER (
		         PARTITION BY CASE
		                      WHEN type IN ('playvoice', 'playvoice_notext') AND voice_id <> '' THEN voice_id
		                      ELSE source_text
		                    END
		         ORDER BY
		           CASE
		             WHEN polished_text <> '' THEN 3
		             WHEN translated_text <> '' THEN 2
		             ELSE 1
		           END DESC,
		           CASE translator_status
		             WHEN 'reviewed' THEN 4
		             WHEN 'polished' THEN 3
		             WHEN 'translated' THEN 2
		             ELSE 1
		           END DESC,
		           updated_at DESC,
		           id DESC
		       ) AS row_rank
		FROM translation_entries` + whereSQL + `
	) prepared_rows
	` + outerWhere + `
) deduped_rows
ORDER BY export_source ASC`

	rows, err := s.db.QueryContext(ctx, exportQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var sourceText string
		var finalText string
		if err := rows.Scan(&sourceText, &finalText); err != nil {
			return err
		}
		if err := yield(sourceText, finalText); err != nil {
			return err
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Store) StreamDistinctVoiceSubtitleRows(ctx context.Context, req model.ExportRequest, yield func(voiceID, finalText string) error) error {
	whereSQL, args := buildWhere(exportEntryQuery(req))

	exportQuery := `
SELECT voice_id, final_text
FROM (
	SELECT voice_id, final_text, row_rank
	FROM (
		SELECT voice_id,
		       CASE
		         WHEN polished_text <> '' THEN polished_text
		         ELSE translated_text
		       END AS final_text,
		       ROW_NUMBER() OVER (
		         PARTITION BY voice_id
		         ORDER BY
		           CASE
		             WHEN polished_text <> '' THEN 3
		             WHEN translated_text <> '' THEN 2
		             ELSE 1
		           END DESC,
		           CASE translator_status
		             WHEN 'reviewed' THEN 4
		             WHEN 'polished' THEN 3
		             WHEN 'translated' THEN 2
		             ELSE 1
		           END DESC,
		           updated_at DESC,
		           id DESC
		       ) AS row_rank
		FROM translation_entries` + whereSQL + `
	) prepared_rows
	WHERE voice_id <> '' AND final_text <> ''
) deduped_rows
WHERE row_rank = 1
ORDER BY voice_id ASC`

	rows, err := s.db.QueryContext(ctx, exportQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var voiceID string
		var finalText string
		if err := rows.Scan(&voiceID, &finalText); err != nil {
			return err
		}
		if err := yield(voiceID, finalText); err != nil {
			return err
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Store) StreamExportRows(ctx context.Context, req model.ExportRequest, yield func(sourceText, finalText string, skipped bool) error) error {
	whereSQL, args := buildWhere(exportEntryQuery(req))

	exportQuery := `
SELECT source_text,
       CASE
         WHEN polished_text <> '' THEN polished_text
         ELSE translated_text
       END AS final_text
FROM translation_entries` + whereSQL + `
ORDER BY source_arc ASC, source_file ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, exportQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var sourceText string
		var finalText string
		if err := rows.Scan(&sourceText, &finalText); err != nil {
			return err
		}
		if req.SkipEmptyFinal && finalText == "" {
			if err := yield(sourceText, finalText, true); err != nil {
				return err
			}
			continue
		}
		if err := yield(sourceText, finalText, false); err != nil {
			return err
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Store) StreamExportEntries(ctx context.Context, req model.ExportRequest, yield func(entry model.Entry) error) error {
	whereSQL, args := buildWhere(exportEntryQuery(req))

	exportQuery := `
SELECT id, type, voice_id, role, source_arc, source_file, source_text,
       translated_text, polished_text, translator_status, created_at, updated_at
FROM translation_entries` + whereSQL + `
ORDER BY source_arc ASC, source_file ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, exportQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return err
		}
		if err := yield(entry); err != nil {
			return err
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Store) GetFilterOptions() (model.FilterOptions, error) {
	distinct := func(column string) ([]string, error) {
		rows, err := s.db.Query(fmt.Sprintf(`
SELECT DISTINCT %s
FROM translation_entries
WHERE %s <> ''
ORDER BY %s ASC
`, column, column, column))
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		values := make([]string, 0)
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, rows.Err()
	}

	rows, err := s.db.Query(`
SELECT filename
FROM arc_files
ORDER BY filename ASC
`)
	if err != nil {
		return model.FilterOptions{}, err
	}
	defer rows.Close()

	arcs := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return model.FilterOptions{}, err
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		arcs = append(arcs, value)
	}
	if err := rows.Err(); err != nil {
		return model.FilterOptions{}, err
	}
	if len(arcs) == 0 {
		arcs, err = distinct("source_arc")
		if err != nil {
			return model.FilterOptions{}, err
		}
	}

	types, err := distinct("type")
	if err != nil {
		return model.FilterOptions{}, err
	}

	statuses := []string{"new", "translated", "polished", "reviewed"}
	slices.Sort(statuses)

	return model.FilterOptions{
		Arcs:     arcs,
		Files:    []string{},
		Types:    types,
		Statuses: statuses,
	}, nil
}

func (s *Store) UpdateEntry(input model.UpdateEntryInput) error {
	now := nowString()
	status := sanitizeStatus(input.TranslatorStatus, input.TranslatedText, input.PolishedText)

	_, err := s.db.Exec(`
UPDATE translation_entries
SET translated_text = ?, polished_text = ?, translator_status = ?, updated_at = ?
WHERE id = ?
`, input.TranslatedText, input.PolishedText, status, now, input.ID)
	return err
}

func (s *Store) ApplyEntryUpdates(inputs []model.UpdateEntryInput) (int, error) {
	if len(inputs) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
UPDATE translation_entries
SET translated_text = ?, polished_text = ?, translator_status = ?, updated_at = ?
WHERE id = ?
`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := nowString()
	updated := 0
	for _, input := range inputs {
		status := sanitizeStatus(input.TranslatorStatus, input.TranslatedText, input.PolishedText)
		res, err := stmt.Exec(input.TranslatedText, input.PolishedText, status, now, input.ID)
		if err != nil {
			return 0, err
		}
		count, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		updated += int(count)
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return updated, nil
}

func (s *Store) BatchUpdateEntries(input model.BatchUpdateInput) (model.BatchUpdateResult, error) {
	if len(input.IDs) == 0 {
		return model.BatchUpdateResult{}, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return model.BatchUpdateResult{}, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
UPDATE translation_entries
SET translated_text = ?, polished_text = ?, translator_status = ?, updated_at = ?
WHERE id = ?
`)
	if err != nil {
		return model.BatchUpdateResult{}, err
	}
	defer stmt.Close()

	updated := 0
	now := nowString()
	status := sanitizeStatus(input.TranslatorStatus, input.TranslatedText, input.PolishedText)
	for _, id := range input.IDs {
		res, err := stmt.Exec(input.TranslatedText, input.PolishedText, status, now, id)
		if err != nil {
			return model.BatchUpdateResult{}, err
		}
		count, err := res.RowsAffected()
		if err != nil {
			return model.BatchUpdateResult{}, err
		}
		updated += int(count)
	}

	if err := tx.Commit(); err != nil {
		return model.BatchUpdateResult{}, err
	}

	return model.BatchUpdateResult{Updated: updated}, nil
}

func (s *Store) BatchUpdateEntryStatusByQuery(input model.FilterBatchStatusInput) (model.BatchUpdateResult, error) {
	status := strings.TrimSpace(input.TranslatorStatus)
	if status == "" {
		return model.BatchUpdateResult{}, fmt.Errorf("translator status is required")
	}

	whereSQL, args, err := buildFilteredScopeWhere(input.Query)
	if err != nil {
		return model.BatchUpdateResult{}, err
	}

	now := nowString()
	sqlArgs := make([]any, 0, len(args)+5)
	sqlArgs = append(sqlArgs, status, status, status, status, now)
	sqlArgs = append(sqlArgs, args...)

	res, err := s.db.Exec(`
UPDATE translation_entries
SET translator_status = CASE
	WHEN ? = 'reviewed' THEN 'reviewed'
	WHEN ? = 'polished' AND polished_text <> '' THEN 'polished'
	WHEN ? = 'translated' AND translated_text <> '' THEN 'translated'
	WHEN ? = 'new' THEN 'new'
	WHEN polished_text <> '' THEN 'polished'
	WHEN translated_text <> '' THEN 'translated'
	ELSE 'new'
END,
updated_at = ?
`+whereSQL, sqlArgs...)
	if err != nil {
		return model.BatchUpdateResult{}, err
	}

	updated, err := res.RowsAffected()
	if err != nil {
		return model.BatchUpdateResult{}, err
	}
	return model.BatchUpdateResult{Updated: int(updated)}, nil
}

func (s *Store) DeleteEntriesByQuery(query model.EntryQuery) (model.BatchDeleteResult, error) {
	whereSQL, args, err := buildFilteredScopeWhere(query)
	if err != nil {
		return model.BatchDeleteResult{}, err
	}

	res, err := s.db.Exec(`DELETE FROM translation_entries`+whereSQL, args...)
	if err != nil {
		return model.BatchDeleteResult{}, err
	}

	deleted, err := res.RowsAffected()
	if err != nil {
		return model.BatchDeleteResult{}, err
	}
	return model.BatchDeleteResult{Deleted: int(deleted)}, nil
}

func (s *Store) ClearEntryTranslationsByQuery(query model.EntryQuery) (model.BatchUpdateResult, error) {
	whereSQL, args, err := buildFilteredScopeWhere(query)
	if err != nil {
		return model.BatchUpdateResult{}, err
	}

	now := nowString()
	sqlArgs := make([]any, 0, len(args)+1)
	sqlArgs = append(sqlArgs, now)
	sqlArgs = append(sqlArgs, args...)

	res, err := s.db.Exec(`
UPDATE translation_entries
SET translated_text = '',
	polished_text = '',
	translator_status = 'new',
	updated_at = ?
`+whereSQL, sqlArgs...)
	if err != nil {
		return model.BatchUpdateResult{}, err
	}

	updated, err := res.RowsAffected()
	if err != nil {
		return model.BatchUpdateResult{}, err
	}
	return model.BatchUpdateResult{Updated: int(updated)}, nil
}

func (s *Store) UpdateEntrySourceText(id int64, sourceText string) error {
	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return fmt.Errorf("source text is required")
	}

	_, err := s.db.Exec(`
UPDATE translation_entries
SET source_text = ?, updated_at = ?
WHERE id = ?
`, sourceText, nowString(), id)
	return err
}

func (s *Store) UpdateImportedTranslation(sourceArc, sourceFile, sourceText, translatedText string, allowOverwrite bool) (matched, updated, skipped bool, err error) {
	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return false, false, true, nil
	}

	row := s.db.QueryRow(`
SELECT id, translated_text, polished_text, translator_status
FROM translation_entries
WHERE source_arc = ? AND source_file = ? AND source_text = ?
`, sourceArc, sourceFile, sourceText)

	var id int64
	var existingTranslated, polished, status string
	if err := row.Scan(&id, &existingTranslated, &polished, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, false, false, nil
		}
		return false, false, false, err
	}

	if !allowOverwrite && existingTranslated != "" {
		return true, false, true, nil
	}

	nextStatus := sanitizeStatus("", translatedText, polished)
	if polished != "" {
		if status == "reviewed" {
			nextStatus = "reviewed"
		} else {
			nextStatus = "polished"
		}
	}

	if _, err := s.db.Exec(`
UPDATE translation_entries
SET translated_text = ?, translator_status = ?, updated_at = ?
WHERE id = ?
`, translatedText, nextStatus, nowString(), id); err != nil {
		return true, false, false, err
	}

	return true, true, false, nil
}

func (s *ImportSession) UpdateImportedTranslation(sourceArc, sourceFile, sourceText, translatedText string, allowOverwrite bool) (matched, updated, skipped bool, err error) {
	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return false, false, true, nil
	}

	entriesByText, err := s.loadArcFileEntries(sourceArc, sourceFile)
	if err != nil {
		return false, false, false, err
	}
	candidates := entriesByText[sourceText]
	if len(candidates) == 0 {
		return false, false, false, nil
	}

	updated, skipped, err = s.updateImportedRow(candidates[0], translatedText, allowOverwrite)
	if err != nil {
		return true, false, false, err
	}
	return true, updated, skipped, nil
}

func (s *Store) UpdateImportedTranslationByIdentity(entryType, voiceID, role, sourceArc, sourceFile, sourceText, translatedText string, allowOverwrite bool) (matched, updated, skipped bool, err error) {
	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return false, false, true, nil
	}

	row := s.db.QueryRow(`
SELECT id, translated_text, polished_text, translator_status
FROM translation_entries
WHERE type = ? AND voice_id = ? AND role = ? AND source_arc = ? AND source_file = ? AND source_text = ?
`, entryType, voiceID, role, sourceArc, sourceFile, sourceText)

	var id int64
	var existingTranslated, polished, status string
	if err := row.Scan(&id, &existingTranslated, &polished, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, false, false, nil
		}
		return false, false, false, err
	}

	if !allowOverwrite && existingTranslated != "" {
		return true, false, true, nil
	}

	nextStatus := sanitizeStatus("", translatedText, polished)
	if polished != "" {
		if status == "reviewed" {
			nextStatus = "reviewed"
		} else {
			nextStatus = "polished"
		}
	}

	if _, err := s.db.Exec(`
UPDATE translation_entries
SET translated_text = ?, translator_status = ?, updated_at = ?
WHERE id = ?
`, translatedText, nextStatus, nowString(), id); err != nil {
		return true, false, false, err
	}

	return true, true, false, nil
}

func (s *Store) UpdateImportedTranslationByKSExtractRow(entryType, voiceID, role, sourceArc, sourceFile, sourceText, translatedText string, allowOverwrite bool) (matchCount int, updated, skipped bool, err error) {
	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return 0, false, true, nil
	}

	query := `
SELECT id, translated_text, polished_text, translator_status
FROM translation_entries
WHERE source_arc = ? AND source_file = ? AND source_text = ?`
	args := []any{sourceArc, sourceFile, sourceText}

	if strings.TrimSpace(entryType) != "" {
		query += ` AND type = ?`
		args = append(args, entryType)
	}
	if strings.TrimSpace(voiceID) != "" {
		query += ` AND voice_id = ?`
		args = append(args, voiceID)
	}
	if strings.TrimSpace(role) != "" {
		query += ` AND role = ?`
		args = append(args, role)
	}
	query += ` ORDER BY id ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return 0, false, false, err
	}

	var id int64
	var existingTranslated, polished, status string
	for rows.Next() {
		var currentID int64
		var currentTranslated, currentPolished, currentStatus string
		if err := rows.Scan(&currentID, &currentTranslated, &currentPolished, &currentStatus); err != nil {
			return 0, false, false, err
		}
		matchCount++
		if matchCount == 1 {
			id = currentID
			existingTranslated = currentTranslated
			polished = currentPolished
			status = currentStatus
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, false, false, err
	}
	rows.Close()

	// If soft hint columns do not match anything, fall back to the required identity only.
	if matchCount == 0 && (strings.TrimSpace(entryType) != "" || strings.TrimSpace(voiceID) != "" || strings.TrimSpace(role) != "") {
		return s.UpdateImportedTranslationByKSExtractRow("", "", "", sourceArc, sourceFile, sourceText, translatedText, allowOverwrite)
	}

	if matchCount == 0 {
		return 0, false, false, nil
	}
	if strings.TrimSpace(translatedText) == "" {
		return matchCount, false, true, nil
	}
	if matchCount > 1 {
		return matchCount, false, true, nil
	}
	if !allowOverwrite && existingTranslated != "" {
		return matchCount, false, true, nil
	}

	nextStatus := sanitizeStatus("", translatedText, polished)
	if polished != "" {
		if status == "reviewed" {
			nextStatus = "reviewed"
		} else {
			nextStatus = "polished"
		}
	}

	if _, err := s.db.Exec(`
UPDATE translation_entries
SET translated_text = ?, translator_status = ?, updated_at = ?
WHERE id = ?
`, translatedText, nextStatus, nowString(), id); err != nil {
		return matchCount, false, false, err
	}

	return matchCount, true, false, nil
}

func (s *ImportSession) UpdateImportedTranslationByKSExtractRow(entryType, voiceID, role, sourceArc, sourceFile, sourceText, translatedText string, allowOverwrite bool) (matchCount int, updated, skipped bool, err error) {
	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return 0, false, true, nil
	}

	entriesByText, err := s.loadArcFileEntries(sourceArc, sourceFile)
	if err != nil {
		return 0, false, false, err
	}
	candidates := filterImportEntries(entriesByText[sourceText], entryType, voiceID, role)
	matchCount = len(candidates)

	if matchCount == 0 && (strings.TrimSpace(entryType) != "" || strings.TrimSpace(voiceID) != "" || strings.TrimSpace(role) != "") {
		return s.UpdateImportedTranslationByKSExtractRow("", "", "", sourceArc, sourceFile, sourceText, translatedText, allowOverwrite)
	}
	if matchCount == 0 {
		return 0, false, false, nil
	}
	if strings.TrimSpace(translatedText) == "" {
		return matchCount, false, true, nil
	}
	if matchCount > 1 {
		return matchCount, false, true, nil
	}

	updated, skipped, err = s.updateImportedRow(candidates[0], translatedText, allowOverwrite)
	if err != nil {
		return matchCount, false, false, err
	}
	return matchCount, updated, skipped, nil
}

func (s *Store) InsertImportedEntry(entryType, voiceID, role, sourceArc, sourceFile, sourceText, translatedText string) (bool, error) {
	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return false, nil
	}

	now := nowString()
	status := sanitizeStatus("", translatedText, "")

	res, err := s.db.Exec(`
INSERT OR IGNORE INTO translation_entries(
	type, voice_id, role, source_arc, source_file, source_text,
	translated_text, polished_text, translator_status, created_at, updated_at
) VALUES(?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?)
`, entryType, voiceID, role, sourceArc, sourceFile, sourceText, translatedText, status, now, now)
	if err != nil {
		return false, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (s *ImportSession) InsertImportedEntry(entryType, voiceID, role, sourceArc, sourceFile, sourceText, translatedText string) (bool, error) {
	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return false, nil
	}

	now := nowString()
	status := sanitizeStatus("", translatedText, "")

	res, err := s.insertEntry.Exec(entryType, voiceID, role, sourceArc, sourceFile, sourceText, translatedText, status, now, now)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected <= 0 {
		return false, nil
	}

	id, err := res.LastInsertId()
	if err != nil {
		id = 0
	}
	entry := &importEntry{
		ID:             id,
		Type:           entryType,
		VoiceID:        voiceID,
		Role:           role,
		SourceArc:      sourceArc,
		SourceFile:     sourceFile,
		SourceText:     sourceText,
		TranslatedText: translatedText,
		PolishedText:   "",
		Status:         status,
	}
	appendCachedImportEntry(s.arcFileEntries[importArcFileKey(sourceArc, sourceFile)], entry)
	appendCachedImportEntry(s.arcEntries[sourceArc], entry)
	if cached, ok := s.sourceFileArcCache[sourceFile]; ok {
		s.sourceFileArcCache[sourceFile] = appendUniqueString(cached, sourceArc)
	}
	return true, nil
}

func (s *ImportSession) UpsertImportedEntry(entry model.Entry, allowOverwrite bool) (inserted, updated, skipped bool, err error) {
	entry = normalizeImportedEntryRecord(entry)
	if entry.SourceText == "" {
		return false, false, true, nil
	}

	entriesByText, err := s.loadArcFileEntries(entry.SourceArc, entry.SourceFile)
	if err != nil {
		return false, false, false, err
	}
	candidates := exactImportEntries(entriesByText[entry.SourceText], entry.Type, entry.VoiceID, entry.Role)
	switch len(candidates) {
	case 0:
		return s.insertImportedEntryRecord(entry)
	case 1:
		return s.updateImportedEntryRecord(candidates[0], entry, allowOverwrite)
	default:
		return false, false, true, nil
	}
}

func (s *Store) FindSourceArcsBySourceFile(sourceFile string) ([]string, error) {
	rows, err := s.db.Query(`
SELECT DISTINCT source_arc
FROM translation_entries
WHERE source_file = ?
ORDER BY source_arc ASC
`, sourceFile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *ImportSession) FindSourceArcsBySourceFile(sourceFile string) ([]string, error) {
	if cached, ok := s.sourceFileArcCache[sourceFile]; ok {
		return append([]string(nil), cached...), nil
	}

	rows, err := s.findSourceArcsByFile.Query(sourceFile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.sourceFileArcCache[sourceFile] = append([]string(nil), values...)
	return values, nil
}

func (s *ImportSession) PrepareTranslatedCSVFile(sourceFile string) (*TranslatedCSVFileState, error) {
	sourceFile = strings.TrimSpace(sourceFile)
	if sourceFile == "" {
		return nil, fmt.Errorf("source file is required")
	}

	sourceArcs, err := s.FindSourceArcsBySourceFile(sourceFile)
	if err != nil {
		return nil, err
	}
	if len(sourceArcs) == 0 {
		sourceArcs = []string{""}
	}

	state := &TranslatedCSVFileState{
		session:      s,
		sourceFile:   sourceFile,
		sourceArcs:   append([]string(nil), sourceArcs...),
		entriesByArc: make(map[string]map[string][]*importEntry, len(sourceArcs)),
	}
	for _, sourceArc := range sourceArcs {
		entriesByText, err := s.loadArcFileEntries(sourceArc, sourceFile)
		if err != nil {
			return nil, err
		}
		state.entriesByArc[sourceArc] = entriesByText
	}
	return state, nil
}

func (s *TranslatedCSVFileState) SourceArcs() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.sourceArcs...)
}

func (s *TranslatedCSVFileState) Apply(sourceText, translatedText string, allowOverwrite bool) (TranslatedCSVApplyResult, error) {
	if s == nil || s.session == nil {
		return TranslatedCSVApplyResult{}, fmt.Errorf("translated csv file state is not initialized")
	}

	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return TranslatedCSVApplyResult{Skipped: 1}, nil
	}

	result := TranslatedCSVApplyResult{
		AmbiguousArcs: make([]string, 0),
	}
	for _, sourceArc := range s.sourceArcs {
		entriesByText := s.entriesByArc[sourceArc]
		candidates := entriesByText[sourceText]
		matchCount := len(candidates)
		if matchCount == 0 {
			inserted, err := s.session.InsertImportedEntry("", "", "", sourceArc, s.sourceFile, sourceText, translatedText)
			if err != nil {
				return result, err
			}
			if inserted {
				result.Inserted++
			}
			continue
		}

		if strings.TrimSpace(translatedText) == "" {
			result.Skipped++
			continue
		}
		if matchCount > 1 {
			result.Skipped++
			result.AmbiguousArcs = append(result.AmbiguousArcs, sourceArc)
			continue
		}

		updated, skipped, err := s.session.updateImportedRow(candidates[0], translatedText, allowOverwrite)
		if err != nil {
			return result, err
		}
		if skipped {
			result.Skipped++
			continue
		}
		if updated {
			result.Updated++
		}
	}

	return result, nil
}

func (s *Store) UpdateImportedTranslationByArcAndText(sourceArc, sourceText, translatedText string, allowOverwrite bool) (matchCount int, updated, skipped bool, err error) {
	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return 0, false, true, nil
	}

	rows, err := s.db.Query(`
SELECT id, translated_text, polished_text, translator_status
FROM translation_entries
WHERE source_arc = ? AND source_text = ?
ORDER BY id ASC
`, sourceArc, sourceText)
	if err != nil {
		return 0, false, false, err
	}
	defer rows.Close()

	var id int64
	var existingTranslated, polished, status string
	for rows.Next() {
		var currentID int64
		var currentTranslated, currentPolished, currentStatus string
		if err := rows.Scan(&currentID, &currentTranslated, &currentPolished, &currentStatus); err != nil {
			return 0, false, false, err
		}
		matchCount++
		if matchCount == 1 {
			id = currentID
			existingTranslated = currentTranslated
			polished = currentPolished
			status = currentStatus
		}
	}
	if err := rows.Err(); err != nil {
		return 0, false, false, err
	}

	if matchCount == 0 {
		return 0, false, false, nil
	}
	if strings.TrimSpace(translatedText) == "" {
		return matchCount, false, true, nil
	}
	if matchCount > 1 {
		return matchCount, false, true, nil
	}
	if !allowOverwrite && existingTranslated != "" {
		return matchCount, false, true, nil
	}

	nextStatus := sanitizeStatus("", translatedText, polished)
	if polished != "" {
		if status == "reviewed" {
			nextStatus = "reviewed"
		} else {
			nextStatus = "polished"
		}
	}

	if _, err := s.db.Exec(`
UPDATE translation_entries
SET translated_text = ?, translator_status = ?, updated_at = ?
WHERE id = ?
`, translatedText, nextStatus, nowString(), id); err != nil {
		return matchCount, false, false, err
	}

	return matchCount, true, false, nil
}

func (s *ImportSession) UpdateImportedTranslationByArcAndText(sourceArc, sourceText, translatedText string, allowOverwrite bool) (matchCount int, updated, skipped bool, err error) {
	sourceText = normalizeEntrySourceText(sourceText)
	if sourceText == "" {
		return 0, false, true, nil
	}

	entriesByText, err := s.loadArcEntries(sourceArc)
	if err != nil {
		return 0, false, false, err
	}
	candidates := entriesByText[sourceText]
	matchCount = len(candidates)

	if matchCount == 0 {
		return 0, false, false, nil
	}
	if strings.TrimSpace(translatedText) == "" {
		return matchCount, false, true, nil
	}
	if matchCount > 1 {
		return matchCount, false, true, nil
	}

	updated, skipped, err = s.updateImportedRow(candidates[0], translatedText, allowOverwrite)
	if err != nil {
		return matchCount, false, false, err
	}
	return matchCount, updated, skipped, nil
}

func (s *ImportSession) updateImportedRow(entry *importEntry, translatedText string, allowOverwrite bool) (updated, skipped bool, err error) {
	if !allowOverwrite && entry.TranslatedText != "" {
		return false, true, nil
	}

	nextStatus := sanitizeStatus("", translatedText, entry.PolishedText)
	if entry.PolishedText != "" {
		if entry.Status == "reviewed" {
			nextStatus = "reviewed"
		} else {
			nextStatus = "polished"
		}
	}

	if _, err := s.updateTranslatedEntry.Exec(translatedText, nextStatus, nowString(), entry.ID); err != nil {
		return false, false, err
	}
	entry.TranslatedText = translatedText
	entry.Status = nextStatus
	return true, false, nil
}

func (s *ImportSession) insertImportedEntryRecord(entry model.Entry) (inserted, updated, skipped bool, err error) {
	res, err := s.insertFullEntry.Exec(
		entry.Type,
		entry.VoiceID,
		entry.Role,
		entry.SourceArc,
		entry.SourceFile,
		entry.SourceText,
		entry.TranslatedText,
		entry.PolishedText,
		entry.TranslatorStatus,
		entry.CreatedAt,
		entry.UpdatedAt,
	)
	if err != nil {
		return false, false, false, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, false, false, err
	}
	if affected <= 0 {
		return false, false, true, nil
	}

	id, err := res.LastInsertId()
	if err != nil {
		id = 0
	}
	cachedEntry := &importEntry{
		ID:             id,
		Type:           entry.Type,
		VoiceID:        entry.VoiceID,
		Role:           entry.Role,
		SourceArc:      entry.SourceArc,
		SourceFile:     entry.SourceFile,
		SourceText:     entry.SourceText,
		TranslatedText: entry.TranslatedText,
		PolishedText:   entry.PolishedText,
		Status:         entry.TranslatorStatus,
	}
	appendCachedImportEntry(s.arcFileEntries[importArcFileKey(entry.SourceArc, entry.SourceFile)], cachedEntry)
	appendCachedImportEntry(s.arcEntries[entry.SourceArc], cachedEntry)
	if cached, ok := s.sourceFileArcCache[entry.SourceFile]; ok {
		s.sourceFileArcCache[entry.SourceFile] = appendUniqueString(cached, entry.SourceArc)
	}
	return true, false, false, nil
}

func (s *ImportSession) updateImportedEntryRecord(existing *importEntry, entry model.Entry, allowOverwrite bool) (inserted, updated, skipped bool, err error) {
	if !allowOverwrite && (existing.TranslatedText != "" || existing.PolishedText != "") {
		return false, false, true, nil
	}

	if _, err := s.updateFullEntry.Exec(
		entry.TranslatedText,
		entry.PolishedText,
		entry.TranslatorStatus,
		entry.CreatedAt,
		entry.UpdatedAt,
		existing.ID,
	); err != nil {
		return false, false, false, err
	}

	existing.TranslatedText = entry.TranslatedText
	existing.PolishedText = entry.PolishedText
	existing.Status = entry.TranslatorStatus
	return false, true, false, nil
}
