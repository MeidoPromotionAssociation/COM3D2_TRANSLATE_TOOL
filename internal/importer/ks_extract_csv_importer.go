package importer

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/textutil"
)

const (
	ksExtractHeaderType       = "\u7c7b\u578b"
	ksExtractHeaderRole       = "\u89d2\u8272"
	ksExtractHeaderSourceArc  = "\u6240\u5c5earc"
	ksExtractHeaderSourceFile = "\u6e90\u6587\u4ef6"
	ksExtractHeaderSourceText = "\u539f\u6587"
	ksExtractHeaderTargetText = "\u8bd1\u6587"
)

type KSExtractCSVImporter struct {
	store *db.Store
}

func NewKSExtractCSVImporter(store *db.Store) *KSExtractCSVImporter {
	return &KSExtractCSVImporter{store: store}
}

func (i *KSExtractCSVImporter) Name() string {
	return "ks-extract-csv"
}

func normalizeCSVHeader(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "\uFEFF")
	return strings.ToLower(value)
}

func indexHeader(header []string, candidates ...string) int {
	for idx, value := range header {
		normalized := normalizeCSVHeader(value)
		for _, candidate := range candidates {
			if normalized == normalizeCSVHeader(candidate) {
				return idx
			}
		}
	}
	return -1
}

func recordValue(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(record[index], "\uFEFF"))
}

func normalizeKSExtractSourceArc(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = filepath.Base(value)
	if strings.HasSuffix(strings.ToLower(value), ".arc_extracted") {
		return value[:len(value)-len("_extracted")]
	}
	if filepath.Ext(value) == "" {
		return value + ".arc"
	}
	return value
}

func normalizeKSExtractSourceFile(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = filepath.Base(value)
	if strings.EqualFold(filepath.Ext(value), ".txt") {
		return strings.TrimSuffix(value, filepath.Ext(value)) + ".ks"
	}
	return value
}

func newCSVReaderWithBOM(file *os.File) (*csv.Reader, error) {
	reader := bufio.NewReaderSize(file, 1<<20)
	if bytes, err := reader.Peek(3); err == nil && len(bytes) == 3 && bytes[0] == 0xEF && bytes[1] == 0xBB && bytes[2] == 0xBF {
		if _, err := reader.Discard(3); err != nil {
			return nil, err
		}
	}

	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1
	return csvReader, nil
}

func (i *KSExtractCSVImporter) Import(ctx context.Context, req model.ImportRequest) (model.ImportResult, error) {
	filePath := strings.TrimSpace(req.RootDir)
	if filePath == "" {
		return model.ImportResult{}, fmt.Errorf("import csv file is required")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return model.ImportResult{}, err
	}
	defer file.Close()

	reader, err := newCSVReaderWithBOM(file)
	if err != nil {
		return model.ImportResult{}, err
	}

	header, err := reader.Read()
	if err != nil {
		return model.ImportResult{}, err
	}

	typeIndex := indexHeader(header, ksExtractHeaderType, "type")
	voiceIndex := indexHeader(header, "voice_id")
	roleIndex := indexHeader(header, ksExtractHeaderRole, "role", "name")
	arcIndex := indexHeader(header, ksExtractHeaderSourceArc, "source_arc", "arc")
	fileIndex := indexHeader(header, ksExtractHeaderSourceFile, "source_file")
	sourceTextIndex := indexHeader(header, ksExtractHeaderSourceText, "source_text", "text")
	translatedTextIndex := indexHeader(header, ksExtractHeaderTargetText, "translated_text", "translation")

	requiredColumns := map[string]int{
		"type":            typeIndex,
		"voice_id":        voiceIndex,
		"role":            roleIndex,
		"source_arc":      arcIndex,
		"source_file":     fileIndex,
		"source_text":     sourceTextIndex,
		"translated_text": translatedTextIndex,
	}
	for name, index := range requiredColumns {
		if index < 0 {
			return model.ImportResult{}, fmt.Errorf("ks_extract csv missing required column: %s", name)
		}
	}

	result := model.ImportResult{
		Importer:       i.Name(),
		FilesProcessed: 1,
		Messages:       make([]string, 0),
	}
	runtime, err := NewImportRuntime(ctx, i.store, i.Name(), &result)
	if err != nil {
		return model.ImportResult{}, err
	}
	defer runtime.Rollback()
	runtime.BeginFile(filePath)

	lineNumber := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, err
		}

		lineNumber++

		entryType := recordValue(record, typeIndex)
		voiceID := recordValue(record, voiceIndex)
		role := recordValue(record, roleIndex)
		sourceArc := normalizeKSExtractSourceArc(recordValue(record, arcIndex))
		sourceFile := normalizeKSExtractSourceFile(recordValue(record, fileIndex))
		sourceText := textutil.NormalizeSourceText(recordValue(record, sourceTextIndex))
		translatedText := recordValue(record, translatedTextIndex)

		if sourceArc == "" || sourceFile == "" || sourceText == "" {
			result.ErrorLines++
			result.Messages = append(result.Messages, fmt.Sprintf("%s:%d missing required values", filePath, lineNumber))
			if err := runtime.LineProcessed(); err != nil {
				return result, err
			}
			continue
		}

		matchCount, updated, skipped, err := runtime.Session().UpdateImportedTranslationByKSExtractRow(
			entryType,
			voiceID,
			role,
			sourceArc,
			sourceFile,
			sourceText,
			translatedText,
			req.AllowOverwrite,
		)
		if err != nil {
			return result, err
		}
		if matchCount == 0 {
			inserted, err := runtime.Session().InsertImportedEntry(
				entryType,
				voiceID,
				role,
				sourceArc,
				sourceFile,
				sourceText,
				translatedText,
			)
			if err != nil {
				return result, err
			}
			if inserted {
				result.Inserted++
			} else {
				result.Unmatched++
			}
			if err := runtime.LineProcessed(); err != nil {
				return result, err
			}
			continue
		}
		if matchCount > 1 {
			result.Skipped++
			result.Messages = append(result.Messages, fmt.Sprintf("%s:%d ambiguous match (%d entries)", filePath, lineNumber, matchCount))
			if err := runtime.LineProcessed(); err != nil {
				return result, err
			}
			continue
		}
		if skipped {
			result.Skipped++
		} else if updated {
			result.Updated++
		}
		if err := runtime.LineProcessed(); err != nil {
			return result, err
		}
	}

	if err := runtime.Commit(); err != nil {
		return result, err
	}

	return result, nil
}
