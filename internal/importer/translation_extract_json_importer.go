package importer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/textutil"
)

type TranslationExtractJSONImporter struct {
	store *db.Store
}

type translationExtractJSONEntryValue struct {
	TranslatedText string
	ScriptFiles    []string
}

func NewTranslationExtractJSONImporter(store *db.Store) *TranslationExtractJSONImporter {
	return &TranslationExtractJSONImporter{store: store}
}

func (i *TranslationExtractJSONImporter) Name() string {
	return "translation-extract-json"
}

func collectTranslationExtractJSONFiles(rootPath string) ([]string, error) {
	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(info.Name()), ".json") {
			return nil, fmt.Errorf("import source must be a .json file or a directory containing .json files")
		}
		return []string{rootPath}, nil
	}

	files := make([]string, 0)
	err = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".json") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func newJSONDecoderWithBOM(file *os.File) (*json.Decoder, error) {
	reader := bufio.NewReaderSize(file, 1<<20)
	if bytes, err := reader.Peek(3); err == nil && len(bytes) == 3 && bytes[0] == 0xEF && bytes[1] == 0xBB && bytes[2] == 0xBF {
		if _, err := reader.Discard(3); err != nil {
			return nil, err
		}
	}
	return json.NewDecoder(reader), nil
}

func decodeTranslationExtractJSONEntryValue(raw json.RawMessage) (translationExtractJSONEntryValue, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return translationExtractJSONEntryValue{}, err
	}

	result := translationExtractJSONEntryValue{}
	for key, value := range payload {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "official", "offical", "translation", "translatedtext":
			var translatedText string
			if err := json.Unmarshal(value, &translatedText); err != nil {
				return translationExtractJSONEntryValue{}, fmt.Errorf("translation field must be a string")
			}
			result.TranslatedText = translatedText
		case "scriptfiles":
			var scriptFiles []string
			if err := json.Unmarshal(value, &scriptFiles); err != nil {
				return translationExtractJSONEntryValue{}, fmt.Errorf("scriptFiles field must be an array of strings")
			}
			result.ScriptFiles = scriptFiles
		}
	}

	return result, nil
}

func normalizeTranslationExtractScriptFiles(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		sourceFile := normalizeKSExtractSourceFile(value)
		if sourceFile == "" {
			continue
		}
		if _, ok := seen[sourceFile]; ok {
			continue
		}
		seen[sourceFile] = struct{}{}
		normalized = append(normalized, sourceFile)
	}
	return normalized
}

func translationExtractEntryPreview(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 80 {
		return value
	}
	return value[:77] + "..."
}

func parseTranslationExtractJSONFile(path string, visit func(entryIndex int, sourceText string, entry translationExtractJSONEntryValue, entryErr error) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder, err := newJSONDecoderWithBOM(file)
	if err != nil {
		return err
	}

	startToken, err := decoder.Token()
	if err != nil {
		return err
	}
	startDelim, ok := startToken.(json.Delim)
	if !ok || startDelim != '{' {
		return fmt.Errorf("%s: translation extract json must be a top-level object", path)
	}

	entryIndex := 0
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}

		sourceText, ok := token.(string)
		if !ok {
			return fmt.Errorf("%s: invalid entry key", path)
		}

		var rawValue json.RawMessage
		if err := decoder.Decode(&rawValue); err != nil {
			return err
		}

		entryIndex++
		entryValue, err := decodeTranslationExtractJSONEntryValue(rawValue)
		if err != nil {
			err = fmt.Errorf("%s: entry %d (%q): %w", path, entryIndex, translationExtractEntryPreview(sourceText), err)
		}
		if err := visit(entryIndex, sourceText, entryValue, err); err != nil {
			return err
		}
	}

	endToken, err := decoder.Token()
	if err != nil {
		return err
	}
	endDelim, ok := endToken.(json.Delim)
	if !ok || endDelim != '}' {
		return fmt.Errorf("%s: translation extract json ended unexpectedly", path)
	}
	return nil
}

func (i *TranslationExtractJSONImporter) Import(ctx context.Context, req model.ImportRequest) (model.ImportResult, error) {
	rootPath := strings.TrimSpace(req.RootDir)
	if rootPath == "" {
		return model.ImportResult{}, fmt.Errorf("import json file or directory is required")
	}

	files, err := collectTranslationExtractJSONFiles(rootPath)
	if err != nil {
		return model.ImportResult{}, err
	}

	result := model.ImportResult{
		Importer: i.Name(),
		Messages: make([]string, 0),
	}
	runtime, err := NewImportRuntime(ctx, i.store, i.Name(), &result)
	if err != nil {
		return model.ImportResult{}, err
	}
	defer runtime.Rollback()

	currentSession := runtime.Session()
	fileStates := make(map[string]*db.TranslatedCSVFileState)

	for _, path := range files {
		result.FilesProcessed++
		runtime.BeginFile(path)

		err := parseTranslationExtractJSONFile(path, func(entryIndex int, rawSourceText string, entryValue translationExtractJSONEntryValue, entryErr error) error {
			if entryErr != nil {
				result.ErrorLines++
				result.Messages = append(result.Messages, entryErr.Error())
				return runtime.LineProcessed()
			}

			sourceText := textutil.NormalizeSourceText(rawSourceText)
			if sourceText == "" {
				result.ErrorLines++
				result.Messages = append(result.Messages, fmt.Sprintf("%s: entry %d has empty source text after normalization", path, entryIndex))
				return runtime.LineProcessed()
			}

			scriptFiles := normalizeTranslationExtractScriptFiles(entryValue.ScriptFiles)
			if len(scriptFiles) == 0 {
				result.ErrorLines++
				result.Messages = append(result.Messages, fmt.Sprintf("%s: entry %d (%q) is missing scriptFiles", path, entryIndex, translationExtractEntryPreview(sourceText)))
				return runtime.LineProcessed()
			}

			translatedText := strings.TrimSpace(entryValue.TranslatedText)
			if translatedText == "" {
				result.Skipped++
				return runtime.LineProcessed()
			}

			if runtime.Session() != currentSession {
				currentSession = runtime.Session()
				fileStates = make(map[string]*db.TranslatedCSVFileState)
			}

			matchedAny := false
			for _, sourceFile := range scriptFiles {
				fileState := fileStates[sourceFile]
				if fileState == nil {
					fileState, err = currentSession.PrepareTranslatedCSVFile(sourceFile)
					if err != nil {
						return err
					}
					fileStates[sourceFile] = fileState
				}

				applyResult, err := fileState.ApplyMatchOnly(sourceText, translatedText, req.AllowOverwrite)
				if err != nil {
					return err
				}
				if applyResult.Unmatched == 0 {
					matchedAny = true
				}
				result.Updated += applyResult.Updated
				result.Skipped += applyResult.Skipped
				for _, sourceArc := range applyResult.AmbiguousArcs {
					result.Messages = append(result.Messages, fmt.Sprintf("%s: entry %d (%q) ambiguous match in %s / %s", path, entryIndex, translationExtractEntryPreview(sourceText), sourceArc, sourceFile))
				}
			}

			if !matchedAny {
				result.Unmatched++
			}
			return runtime.LineProcessed()
		})
		if err != nil {
			return result, err
		}
	}

	if err := runtime.Commit(); err != nil {
		return result, err
	}

	return result, nil
}
