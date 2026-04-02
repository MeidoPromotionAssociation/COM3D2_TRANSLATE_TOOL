// COM3D2 KAG Script (.ks) Text Extractor for JAT Translation Plugin
//
// Go reimplementation of ks_extract.py with concurrent file parsing.
//
// Usage:
//
//	ks_extract <input_path> [-o output.csv] [-r]
//	ks_extract ./ks_example -o translations.csv -r
package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// Entry represents one extracted text entry from a .ks file.
type Entry struct {
	Type       string // talk, narration, playvoice, playvoice_notext, choice, calldialog, subtitle
	VoiceID    string
	Name       string
	Text       string
	SourceFile string
	Arc        string
}

// fileResult holds the parse result for a single file, with its original index
// so we can reassemble results in deterministic order after concurrent parsing.
type fileResult struct {
	Index   int
	Entries []Entry
	Path    string
	Count   int
}

// ---------- Encoding detection ----------

// hasCJK checks whether the string contains any CJK characters (hiragana, katakana, kanji, fullwidth).
func hasCJK(s string, limit int) bool {
	checked := 0
	for _, c := range s {
		if checked >= limit {
			break
		}
		if (c >= '\u3040' && c <= '\u309F') || // Hiragana
			(c >= '\u30A0' && c <= '\u30FF') || // Katakana
			(c >= '\u4E00' && c <= '\u9FFF') || // CJK Unified Ideographs
			(c >= '\uFF00' && c <= '\uFFEF') { // Fullwidth forms
			return true
		}
		checked++
	}
	return false
}

// decodeShiftJIS decodes raw bytes as Shift-JIS.
func decodeShiftJIS(raw []byte) (string, error) {
	reader := transform.NewReader(bytes.NewReader(raw), japanese.ShiftJIS.NewDecoder())
	decoded, err := readAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func readAll(r *transform.Reader) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	return buf.Bytes(), err
}

// detectAndDecode reads the raw bytes, detects encoding, and returns decoded text.
func detectAndDecode(raw []byte) string {
	// Check BOM
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		return string(raw[3:]) // UTF-8 BOM
	}
	if bytes.HasPrefix(raw, []byte{0xFF, 0xFE}) {
		return decodeUTF16LE(raw[2:])
	}
	if bytes.HasPrefix(raw, []byte{0xFE, 0xFF}) {
		return decodeUTF16BE(raw[2:])
	}

	// Try UTF-8 first
	if isValidUTF8(raw) {
		s := string(raw)
		if hasCJK(s, 2000) {
			return s
		}
	}

	// Try Shift-JIS
	if s, err := decodeShiftJIS(raw); err == nil && hasCJK(s, 2000) {
		return s
	}

	// Fallback without CJK check
	if isValidUTF8(raw) {
		return string(raw)
	}
	if s, err := decodeShiftJIS(raw); err == nil {
		return s
	}

	// Last resort
	return string(raw)
}

func isValidUTF8(data []byte) bool {
	// Check for invalid UTF-8 sequences by round-tripping
	s := string(data)
	for _, r := range s {
		if r == '\uFFFD' {
			// Could be actual replacement char in source, but for multi-byte
			// sequences it likely means invalid UTF-8
			return false
		}
	}
	return true
}

func decodeUTF16LE(raw []byte) string {
	if len(raw)%2 != 0 {
		raw = raw[:len(raw)-1]
	}
	u16 := make([]uint16, len(raw)/2)
	for i := range u16 {
		u16[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	runes := utf16.Decode(u16)
	return string(runes)
}

func decodeUTF16BE(raw []byte) string {
	if len(raw)%2 != 0 {
		raw = raw[:len(raw)-1]
	}
	u16 := make([]uint16, len(raw)/2)
	for i := range u16 {
		u16[i] = uint16(raw[2*i])<<8 | uint16(raw[2*i+1])
	}
	runes := utf16.Decode(u16)
	return string(runes)
}

// ---------- Arc name detection ----------

func detectArcName(filepath string) string {
	abs, err := absPath(filepath)
	if err != nil {
		abs = filepath
	}
	parts := splitPath(abs)
	for _, part := range parts {
		lower := strings.ToLower(part)
		if strings.HasSuffix(lower, ".arc_extracted") {
			return part[:len(part)-len("_extracted")]
		}
	}
	return ""
}

func absPath(p string) (string, error) {
	return filepath.Abs(p)
}

func splitPath(p string) []string {
	p = filepath.ToSlash(p)
	return strings.Split(p, "/")
}

// ---------- Tag attribute parser ----------

var reTagName = regexp.MustCompile(`^@\w+\s*(.*)`)
var reAttrKey = regexp.MustCompile(`^\w+`)

// parseTagAttributes parses attributes from a KAG tag line.
// e.g. "@talk voice=H0_04530 name=[HF] maid=0 wait"
// Returns map: {"voice": "H0_04530", "name": "[HF]", "maid": "0", "wait": ""}
// Bare flags get empty-string values.
func parseTagAttributes(tagLine string) map[string]string {
	attrs := make(map[string]string)
	s := strings.TrimSpace(tagLine)
	m := reTagName.FindStringSubmatch(s)
	if m == nil {
		return attrs
	}
	rest := m[1]
	pos := 0
	for pos < len(rest) {
		// Skip whitespace
		for pos < len(rest) && (rest[pos] == ' ' || rest[pos] == '\t') {
			pos++
		}
		if pos >= len(rest) {
			break
		}
		// Match attribute name
		km := reAttrKey.FindString(rest[pos:])
		if km == "" {
			pos++
			continue
		}
		key := km
		pos += len(km)
		// Check for =value
		if pos < len(rest) && rest[pos] == '=' {
			pos++ // skip '='
			if pos < len(rest) && (rest[pos] == '"' || rest[pos] == '\'') {
				quote := rest[pos]
				end := strings.IndexByte(rest[pos+1:], quote)
				if end == -1 {
					end = len(rest) - pos - 1
				}
				attrs[key] = rest[pos+1 : pos+1+end]
				pos = pos + 1 + end + 1
			} else {
				// Unquoted value (until whitespace)
				end := pos
				for end < len(rest) && rest[end] != ' ' && rest[end] != '\t' {
					end++
				}
				attrs[key] = rest[pos:end]
				pos = end
			}
		} else {
			attrs[key] = "" // bare flag
		}

	}
	return attrs
}

// ---------- Text normalization ----------

func normalizeText(text string) string {
	// Strip trailing whitespace, full-width spaces, and pipe characters
	text = strings.TrimRight(text, " \u3000|")
	text = strings.ReplaceAll(text, "|", "\n")
	return strings.TrimSpace(text)
}

// ---------- KS file parser (state machine) ----------

type parserState int

const (
	stateIDLE parserState = iota
	stateInTalk
	stateInPlayVoice
)

var (
	reChoicesSet = regexp.MustCompile(`(?i)^@ChoicesSet\b`)
	reCallDialog = regexp.MustCompile(`(?i)^@CallDialog\b`)
	reSubtitle   = regexp.MustCompile(`(?i)^@SubtitleDisplay(ForPlayVoice)?\b`)
	reTalk       = regexp.MustCompile(`(?i)^@talk(Repeat)?\b`)
	rePlayVoice  = regexp.MustCompile(`(?i)^@PlayVoice\b`)
	reHitret     = regexp.MustCompile(`(?i)^@hitret\b`)
)

func parseKSFile(path string) []Entry {
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: Could not read %s: %v\n", path, err)
		return nil
	}

	text := detectAndDecode(raw)
	lines := strings.Split(text, "\n")

	var entries []Entry
	state := stateIDLE
	sourceFile := filepath.Base(path)
	arc := detectArcName(path)

	// @talk state
	var voiceID, name string
	var textLines []string

	// @PlayVoice state
	var pvVoiceID string
	var pvComments []string

	finalizePlayVoice := func() {
		if pvVoiceID != "" {
			commentText := strings.TrimSpace(strings.Join(pvComments, ""))
			typ := "playvoice"
			if commentText == "" {
				typ = "playvoice_notext"
			}
			entries = append(entries, Entry{
				Type:       typ,
				VoiceID:    pvVoiceID,
				Name:       "",
				Text:       commentText,
				SourceFile: sourceFile,
				Arc:        arc,
			})
		}
		pvVoiceID = ""
		pvComments = nil
	}

	for _, line := range lines {
		s := strings.TrimSpace(line)

		// --- Single-line text tags ---

		// @ChoicesSet text="..."
		if reChoicesSet.MatchString(s) {
			if state == stateInPlayVoice {
				finalizePlayVoice()
				state = stateIDLE
			}
			attrs := parseTagAttributes(s)
			if t, ok := attrs["text"]; ok && t != "" {
				entries = append(entries, Entry{
					Type:       "choice",
					VoiceID:    "",
					Name:       "",
					Text:       t,
					SourceFile: sourceFile,
					Arc:        arc,
				})
			}
			continue
		}

		// @CallDialog text='...'
		if reCallDialog.MatchString(s) {
			if state == stateInPlayVoice {
				finalizePlayVoice()
				state = stateIDLE
			}
			attrs := parseTagAttributes(s)
			if t, ok := attrs["text"]; ok && t != "" {
				t = normalizeText(t)
				if t != "" {
					entries = append(entries, Entry{
						Type:       "calldialog",
						VoiceID:    "",
						Name:       "",
						Text:       t,
						SourceFile: sourceFile,
						Arc:        arc,
					})
				}
			}
			continue
		}

		// @SubtitleDisplay / @SubtitleDisplayForPlayVoice
		if reSubtitle.MatchString(s) {
			if state == stateInPlayVoice {
				finalizePlayVoice()
				state = stateIDLE
			}
			attrs := parseTagAttributes(s)
			if t, ok := attrs["text"]; ok && t != "" {
				voice := attrs["voice"]
				entries = append(entries, Entry{
					Type:       "subtitle",
					VoiceID:    voice,
					Name:       "",
					Text:       t,
					SourceFile: sourceFile,
					Arc:        arc,
				})
			}
			continue
		}

		// --- @talk / @talkRepeat ---
		if reTalk.MatchString(s) {
			if state == stateInPlayVoice {
				finalizePlayVoice()
			}
			attrs := parseTagAttributes(s)
			voiceID = attrs["voice"]
			name = attrs["name"]
			textLines = nil
			state = stateInTalk
			continue
		}

		// --- @PlayVoice ---
		if rePlayVoice.MatchString(s) {
			if state == stateInPlayVoice {
				finalizePlayVoice()
			}
			attrs := parseTagAttributes(s)
			pvVoiceID = attrs["voice"]
			pvComments = nil
			state = stateInPlayVoice
			continue
		}

		// --- @hitret ---
		if reHitret.MatchString(s) {
			if state == stateInTalk {
				t := strings.Join(textLines, "")
				t = normalizeText(t)
				if t != "" {
					typ := "talk"
					if voiceID == "" {
						typ = "narration"
					}
					entries = append(entries, Entry{
						Type:       typ,
						VoiceID:    voiceID,
						Name:       name,
						Text:       t,
						SourceFile: sourceFile,
						Arc:        arc,
					})
				}
			}
			state = stateIDLE
			voiceID = ""
			name = ""
			textLines = nil
			continue
		}

		// --- Collect text in IN_TALK state ---
		if state == stateInTalk {
			if s == "" || s[0] == ';' || s[0] == '@' || s[0] == '*' {
				continue
			}
			textLines = append(textLines, s)
			continue
		}

		// --- Collect comments in IN_PLAYVOICE state ---
		if state == stateInPlayVoice {
			if len(s) > 0 && s[0] == ';' {
				comment := strings.TrimSpace(s[1:])
				if comment != "" && !strings.HasPrefix(comment, "@") {
					pvComments = append(pvComments, comment)
				}
				continue
			}
			if s != "" && (s[0] == '@' || s[0] == '*') {
				finalizePlayVoice()
				state = stateIDLE
			}
			continue
		}
	}

	// Finalize remaining state at EOF
	if state == stateInPlayVoice {
		finalizePlayVoice()
	}

	return entries
}

// ---------- File collection ----------

func collectKSFiles(inputPath string, recursive bool) ([]string, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return nil, fmt.Errorf("cannot access %s: %w", inputPath, err)
	}

	if !info.IsDir() {
		if strings.EqualFold(filepath.Ext(inputPath), ".ks") {
			abs, _ := filepath.Abs(inputPath)
			return []string{abs}, nil
		}
		return nil, nil
	}

	var files []string
	if recursive {
		err = filepath.WalkDir(inputPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".ks") {
				abs, _ := filepath.Abs(path)
				files = append(files, abs)
			}
			return nil
		})
	} else {
		entries, err2 := os.ReadDir(inputPath)
		if err2 != nil {
			return nil, err2
		}
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".ks") {
				abs, _ := filepath.Abs(filepath.Join(inputPath, e.Name()))
				files = append(files, abs)
			}
		}
	}
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

// ---------- Concurrent parsing ----------

func parseFilesConcurrently(files []string) []fileResult {
	numWorkers := runtime.NumCPU()
	if numWorkers > len(files) {
		numWorkers = len(files)
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	type job struct {
		Index int
		Path  string
	}

	jobs := make(chan job, len(files))
	results := make([]fileResult, len(files))
	var wg sync.WaitGroup

	// Start workers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				entries := parseKSFile(j.Path)
				results[j.Index] = fileResult{
					Index:   j.Index,
					Entries: entries,
					Path:    j.Path,
					Count:   len(entries),
				}
			}
		}()
	}

	// Send jobs
	for i, f := range files {
		jobs <- job{Index: i, Path: f}
	}
	close(jobs)

	wg.Wait()
	return results
}

// ---------- CSV output ----------

func writeCSV(entries []Entry, outputPath string) (int, error) {
	f, err := os.Create(outputPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// Write UTF-8 BOM
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return 0, err
	}

	w := csv.NewWriter(f)
	defer w.Flush()

	headers := []string{"类型", "voice_id", "角色", "所属arc", "源文件", "原文", "译文"}
	if err := w.Write(headers); err != nil {
		return 0, err
	}

	for _, e := range entries {
		row := []string{
			e.Type,
			e.VoiceID,
			e.Name,
			e.Arc,
			e.SourceFile,
			e.Text,
			"", // 译文 (to be filled)
		}
		if err := w.Write(row); err != nil {
			return 0, err
		}
	}

	return len(entries), nil
}

// ---------- Statistics ----------

func printStats(entries []Entry) {
	counts := make(map[string]int)
	for _, e := range entries {
		counts[e.Type]++
	}

	fmt.Println()
	fmt.Println("--- Statistics ---")
	fmt.Printf("  @talk with voice:        %d\n", counts["talk"])
	fmt.Printf("  @talk narration:         %d\n", counts["narration"])
	fmt.Printf("  @PlayVoice with comment: %d\n", counts["playvoice"])
	fmt.Printf("  @PlayVoice no comment:   %d\n", counts["playvoice_notext"])
	fmt.Printf("  @ChoicesSet:             %d\n", counts["choice"])
	fmt.Printf("  @CallDialog:             %d\n", counts["calldialog"])
	fmt.Printf("  @Subtitle:               %d\n", counts["subtitle"])
	fmt.Printf("  Total:                   %d\n", len(entries))
}

// ---------- Argument parsing ----------

// parseArgs handles argument parsing where flags can appear anywhere
// (before or after the positional argument), matching Python argparse behavior.
// Go's flag package stops at the first non-flag argument, so we reorder args.
func parseArgs() (inputPath string, outputPath string, recursive bool) {
	outputPath = "output.csv"

	// Manually scan os.Args to separate flags from positional args
	var positional []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-r":
			recursive = true
		case "-o":
			if i+1 < len(args) {
				outputPath = args[i+1]
				i++ // skip value
			}
		case "-h", "--help":
			printUsage()
			os.Exit(0)
		default:
			if strings.HasPrefix(args[i], "-o=") {
				outputPath = args[i][3:]
			} else if strings.HasPrefix(args[i], "-") {
				fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", args[i])
				printUsage()
				os.Exit(1)
			} else {
				positional = append(positional, args[i])
			}
		}
	}

	if len(positional) < 1 {
		printUsage()
		os.Exit(1)
	}
	inputPath = positional[0]
	return
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [flags] <input_path>\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "COM3D2 KAG Script (.ks) Text Extractor for JAT Translation Plugin\n\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	fmt.Fprintf(os.Stderr, "  -o string\n\tOutput CSV file path (default \"output.csv\")\n")
	fmt.Fprintf(os.Stderr, "  -r\n\tRecursively search subdirectories for .ks files\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  %s ./ks_example -o output.csv -r\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s single_file.ks -o result.csv\n", os.Args[0])
}

// ---------- Main ----------

func main() {
	inputPath, output, recursive := parseArgs()

	// Collect .ks files
	files, err := collectKSFiles(inputPath, recursive)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "Error: No .ks files found in: %s\n", inputPath)
		os.Exit(1)
	}

	fmt.Printf("Found %d .ks file(s)\n", len(files))
	fmt.Printf("Using %d worker(s)\n", min(runtime.NumCPU(), len(files)))

	// Parse all files concurrently
	results := parseFilesConcurrently(files)

	// Collect entries in order
	var allEntries []Entry
	for _, r := range results {
		if r.Count > 0 {
			fmt.Printf("  %s: %d entries\n", filepath.Base(r.Path), r.Count)
		}
		allEntries = append(allEntries, r.Entries...)
	}

	if len(allEntries) == 0 {
		fmt.Fprintln(os.Stderr, "No translatable entries found.")
		os.Exit(0)
	}

	// Print stats
	printStats(allEntries)

	// Write output
	count, err := writeCSV(allEntries, output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing CSV: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nWrote %d entries to: %s\n", count, output)
	fmt.Println("  Encoding: UTF-8 BOM")
}
