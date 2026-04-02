package kag

import (
	"bytes"
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

	"COM3D2TranslateTool/internal/textutil"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

type Entry struct {
	Type       string
	VoiceID    string
	Role       string
	SourceText string
	SourceFile string
	Arc        string
}

type fileResult struct {
	Index   int
	Entries []Entry
}

var (
	reTagName    = regexp.MustCompile(`^@\w+\s*(.*)`)
	reAttrKey    = regexp.MustCompile(`^\w+`)
	reChoicesSet = regexp.MustCompile(`(?i)^@ChoicesSet\b`)
	reCallDialog = regexp.MustCompile(`(?i)^@CallDialog\b`)
	reSubtitle   = regexp.MustCompile(`(?i)^@SubtitleDisplay(ForPlayVoice)?\b`)
	reTalk       = regexp.MustCompile(`(?i)^@talk(Repeat)?\b`)
	rePlayVoice  = regexp.MustCompile(`(?i)^@PlayVoice\b`)
	reHitret     = regexp.MustCompile(`(?i)^@hitret\b`)
)

type parserState int

const (
	stateIdle parserState = iota
	stateInTalk
	stateInPlayVoice
)

func ParseKSDir(root string, recursive bool, arcName string) ([]Entry, error) {
	files, err := collectKSFiles(root, recursive)
	if err != nil {
		return nil, err
	}
	results := parseFilesConcurrently(files, arcName)
	var entries []Entry
	for _, result := range results {
		entries = append(entries, result.Entries...)
	}
	return entries, nil
}

func ParseKSFile(path string, arcName string) ([]Entry, error) {
	return parseKSFile(path, arcName)
}

func collectKSFiles(root string, recursive bool) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if strings.EqualFold(filepath.Ext(root), ".ks") {
			return []string{root}, nil
		}
		return nil, nil
	}

	var files []string
	if recursive {
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".ks") {
				files = append(files, path)
			}
			return nil
		})
	} else {
		items, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if !item.IsDir() && strings.EqualFold(filepath.Ext(item.Name()), ".ks") {
				files = append(files, filepath.Join(root, item.Name()))
			}
		}
	}
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func parseFilesConcurrently(files []string, arcName string) []fileResult {
	if len(files) == 0 {
		return nil
	}

	workerCount := runtime.NumCPU()
	if workerCount > len(files) {
		workerCount = len(files)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	results := make([]fileResult, len(files))
	type job struct {
		index int
		path  string
	}

	jobs := make(chan job, len(files))
	var wg sync.WaitGroup

	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for current := range jobs {
				entries, err := parseKSFile(current.path, arcName)
				if err != nil {
					continue
				}
				results[current.index] = fileResult{
					Index:   current.index,
					Entries: entries,
				}
			}
		}()
	}

	for index, path := range files {
		jobs <- job{index: index, path: path}
	}
	close(jobs)
	wg.Wait()

	return results
}

func parseKSFile(path string, arcName string) ([]Entry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	text := detectAndDecode(raw)
	lines := strings.Split(text, "\n")
	sourceFile := filepath.Base(path)
	arc := arcName
	if arc == "" {
		arc = detectArcName(path)
	}

	var entries []Entry
	state := stateIdle
	var talkVoiceID, talkRole string
	var talkLines []string
	var playVoiceID string
	var playVoiceComments []string

	flushPlayVoice := func() {
		if playVoiceID == "" {
			return
		}
		commentText := textutil.NormalizeSourceText(strings.Join(playVoiceComments, ""))
		entryType := "playvoice"
		if commentText == "" {
			entryType = "playvoice_notext"
		}
		entries = append(entries, Entry{
			Type:       entryType,
			VoiceID:    playVoiceID,
			SourceText: commentText,
			SourceFile: sourceFile,
			Arc:        arc,
		})
		playVoiceID = ""
		playVoiceComments = nil
	}

	flushTalk := func() {
		text := normalizeText(strings.Join(talkLines, ""))
		if text == "" {
			return
		}
		entryType := "talk"
		if talkVoiceID == "" {
			entryType = "narration"
		}
		entries = append(entries, Entry{
			Type:       entryType,
			VoiceID:    talkVoiceID,
			Role:       talkRole,
			SourceText: text,
			SourceFile: sourceFile,
			Arc:        arc,
		})
	}

	for _, line := range lines {
		s := strings.TrimSpace(line)

		if reChoicesSet.MatchString(s) {
			if state == stateInPlayVoice {
				flushPlayVoice()
				state = stateIdle
			}
			attrs := parseTagAttributes(s)
			if text := textutil.NormalizeSourceText(attrs["text"]); text != "" {
				entries = append(entries, Entry{
					Type:       "choice",
					SourceText: text,
					SourceFile: sourceFile,
					Arc:        arc,
				})
			}
			continue
		}

		if reCallDialog.MatchString(s) {
			if state == stateInPlayVoice {
				flushPlayVoice()
				state = stateIdle
			}
			attrs := parseTagAttributes(s)
			if text := normalizeText(attrs["text"]); text != "" {
				entries = append(entries, Entry{
					Type:       "calldialog",
					SourceText: text,
					SourceFile: sourceFile,
					Arc:        arc,
				})
			}
			continue
		}

		if reSubtitle.MatchString(s) {
			if state == stateInPlayVoice {
				flushPlayVoice()
				state = stateIdle
			}
			attrs := parseTagAttributes(s)
			if text := textutil.NormalizeSourceText(attrs["text"]); text != "" {
				entries = append(entries, Entry{
					Type:       "subtitle",
					VoiceID:    attrs["voice"],
					SourceText: text,
					SourceFile: sourceFile,
					Arc:        arc,
				})
			}
			continue
		}

		if reTalk.MatchString(s) {
			if state == stateInPlayVoice {
				flushPlayVoice()
			}
			attrs := parseTagAttributes(s)
			talkVoiceID = attrs["voice"]
			talkRole = attrs["name"]
			talkLines = nil
			state = stateInTalk
			continue
		}

		if rePlayVoice.MatchString(s) {
			if state == stateInPlayVoice {
				flushPlayVoice()
			}
			attrs := parseTagAttributes(s)
			playVoiceID = attrs["voice"]
			playVoiceComments = nil
			state = stateInPlayVoice
			continue
		}

		if reHitret.MatchString(s) {
			if state == stateInTalk {
				flushTalk()
			}
			state = stateIdle
			talkVoiceID = ""
			talkRole = ""
			talkLines = nil
			continue
		}

		if state == stateInTalk {
			if s == "" || s[0] == ';' || s[0] == '@' || s[0] == '*' {
				continue
			}
			talkLines = append(talkLines, s)
			continue
		}

		if state == stateInPlayVoice {
			if len(s) > 0 && s[0] == ';' {
				comment := strings.TrimSpace(s[1:])
				if comment != "" && !strings.HasPrefix(comment, "@") {
					playVoiceComments = append(playVoiceComments, comment)
				}
				continue
			}
			if s != "" && (s[0] == '@' || s[0] == '*') {
				flushPlayVoice()
				state = stateIdle
			}
		}
	}

	if state == stateInPlayVoice {
		flushPlayVoice()
	}

	return entries, nil
}

func detectArcName(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	for _, part := range strings.Split(filepath.ToSlash(abs), "/") {
		lower := strings.ToLower(part)
		if strings.HasSuffix(lower, ".arc_extracted") {
			return part[:len(part)-len("_extracted")]
		}
	}

	return ""
}

func normalizeText(value string) string {
	value = strings.TrimRight(value, " \u3000|")
	value = strings.ReplaceAll(value, "|", "\n")
	return textutil.NormalizeSourceText(value)
}

func parseTagAttributes(tagLine string) map[string]string {
	attrs := map[string]string{}
	s := strings.TrimSpace(tagLine)
	match := reTagName.FindStringSubmatch(s)
	if match == nil {
		return attrs
	}

	rest := match[1]
	for pos := 0; pos < len(rest); {
		for pos < len(rest) && (rest[pos] == ' ' || rest[pos] == '\t') {
			pos++
		}
		if pos >= len(rest) {
			break
		}

		key := reAttrKey.FindString(rest[pos:])
		if key == "" {
			pos++
			continue
		}
		pos += len(key)

		if pos < len(rest) && rest[pos] == '=' {
			pos++
			if pos < len(rest) && (rest[pos] == '"' || rest[pos] == '\'') {
				quote := rest[pos]
				end := strings.IndexByte(rest[pos+1:], quote)
				if end == -1 {
					end = len(rest) - pos - 1
				}
				attrs[key] = rest[pos+1 : pos+1+end]
				pos = pos + end + 2
				continue
			}

			end := pos
			for end < len(rest) && rest[end] != ' ' && rest[end] != '\t' {
				end++
			}
			attrs[key] = rest[pos:end]
			pos = end
			continue
		}

		attrs[key] = ""
	}

	return attrs
}

func hasCJK(value string, limit int) bool {
	checked := 0
	for _, char := range value {
		if checked >= limit {
			break
		}
		if (char >= '\u3040' && char <= '\u309F') ||
			(char >= '\u30A0' && char <= '\u30FF') ||
			(char >= '\u4E00' && char <= '\u9FFF') ||
			(char >= '\uFF00' && char <= '\uFFEF') {
			return true
		}
		checked++
	}
	return false
}

func detectAndDecode(raw []byte) string {
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		return string(raw[3:])
	}
	if bytes.HasPrefix(raw, []byte{0xFF, 0xFE}) {
		return decodeUTF16LE(raw[2:])
	}
	if bytes.HasPrefix(raw, []byte{0xFE, 0xFF}) {
		return decodeUTF16BE(raw[2:])
	}

	if isValidUTF8(raw) {
		text := string(raw)
		if hasCJK(text, 2000) {
			return text
		}
	}

	if text, err := decodeShiftJIS(raw); err == nil && hasCJK(text, 2000) {
		return text
	}

	if isValidUTF8(raw) {
		return string(raw)
	}
	if text, err := decodeShiftJIS(raw); err == nil {
		return text
	}

	return string(raw)
}

func isValidUTF8(data []byte) bool {
	value := string(data)
	for _, char := range value {
		if char == '\uFFFD' {
			return false
		}
	}
	return true
}

func decodeShiftJIS(raw []byte) (string, error) {
	reader := transform.NewReader(bytes.NewReader(raw), japanese.ShiftJIS.NewDecoder())
	decoded, err := readAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func readAll(reader *transform.Reader) ([]byte, error) {
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(reader); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func decodeUTF16LE(raw []byte) string {
	if len(raw)%2 != 0 {
		raw = raw[:len(raw)-1]
	}
	values := make([]uint16, len(raw)/2)
	for index := range values {
		values[index] = uint16(raw[index*2]) | uint16(raw[index*2+1])<<8
	}
	return string(utf16.Decode(values))
}

func decodeUTF16BE(raw []byte) string {
	if len(raw)%2 != 0 {
		raw = raw[:len(raw)-1]
	}
	values := make([]uint16, len(raw)/2)
	for index := range values {
		values[index] = uint16(raw[index*2])<<8 | uint16(raw[index*2+1])
	}
	return string(utf16.Decode(values))
}

func MustParseKSFile(path string, arcName string) []Entry {
	entries, err := ParseKSFile(path, arcName)
	if err != nil {
		panic(fmt.Sprintf("failed to parse %s: %v", path, err))
	}
	return entries
}
