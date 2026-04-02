package importer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
)

type EntryJSONLImporter struct {
	store *db.Store
}

func NewEntryJSONLImporter(store *db.Store) *EntryJSONLImporter {
	return &EntryJSONLImporter{store: store}
}

func (i *EntryJSONLImporter) Name() string {
	return "entry-jsonl"
}

func (i *EntryJSONLImporter) Import(ctx context.Context, req model.ImportRequest) (model.ImportResult, error) {
	filePath := strings.TrimSpace(req.RootDir)
	if filePath == "" {
		return model.ImportResult{}, fmt.Errorf("import jsonl file is required")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return model.ImportResult{}, err
	}
	defer file.Close()

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

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimRight(scanner.Text(), "\r")
		if lineNumber == 1 {
			line = strings.TrimPrefix(line, "\uFEFF")
		}
		if strings.TrimSpace(line) == "" {
			continue
		}

		var entry model.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			result.ErrorLines++
			result.Messages = append(result.Messages, fmt.Sprintf("%s:%d invalid jsonl entry: %v", filePath, lineNumber, err))
			if err := runtime.LineProcessed(); err != nil {
				return result, err
			}
			continue
		}

		inserted, updated, skipped, err := runtime.Session().UpsertImportedEntry(entry, req.AllowOverwrite)
		if err != nil {
			return result, err
		}
		if inserted {
			result.Inserted++
		} else if updated {
			result.Updated++
		} else if skipped {
			result.Skipped++
		} else {
			result.Unmatched++
		}

		if err := runtime.LineProcessed(); err != nil {
			return result, err
		}
	}

	if err := scanner.Err(); err != nil {
		return result, err
	}
	if err := runtime.Commit(); err != nil {
		return result, err
	}

	return result, nil
}
