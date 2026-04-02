package importer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/textutil"
)

const translatedCSVHeaderSourceText = "\u539f\u6587"

const (
	translatedCSVImportBatchLineThreshold   = 100000
	translatedCSVImportProgressEmitInterval = 2000
)

type TranslatedCSVImporter struct {
	store *db.Store
}

func NewTranslatedCSVImporter(store *db.Store) *TranslatedCSVImporter {
	return &TranslatedCSVImporter{store: store}
}

func (i *TranslatedCSVImporter) Name() string {
	return "translated-csv"
}

func normalizeTranslatedCSVSourceFile(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.TrimSuffix(base, "_translated")
	if strings.EqualFold(filepath.Ext(base), ".ks") {
		return base
	}
	return base + ".ks"
}

func collectTranslatedCSVFiles(rootPath string) ([]string, error) {
	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(info.Name()), ".csv") {
			return nil, fmt.Errorf("import source must be a .csv file or a directory containing *_translated.csv files")
		}
		return []string{rootPath}, nil
	}

	files := make([]string, 0)
	err = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".csv") {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), "_translated.csv") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

func (i *TranslatedCSVImporter) Import(ctx context.Context, req model.ImportRequest) (model.ImportResult, error) {
	rootPath := strings.TrimSpace(req.RootDir)
	if rootPath == "" {
		return model.ImportResult{}, fmt.Errorf("import file or directory is required")
	}

	files, err := collectTranslatedCSVFiles(rootPath)
	if err != nil {
		return model.ImportResult{}, err
	}

	result := model.ImportResult{
		Importer: i.Name(),
		Messages: make([]string, 0),
	}
	runtime, err := NewImportRuntimeWithOptions(ctx, i.store, i.Name(), &result, ImportRuntimeOptions{
		BatchLineThreshold:   translatedCSVImportBatchLineThreshold,
		ProgressEmitInterval: translatedCSVImportProgressEmitInterval,
	})
	if err != nil {
		return model.ImportResult{}, err
	}
	defer runtime.Rollback()

	for _, path := range files {
		sourceFile := normalizeTranslatedCSVSourceFile(path)

		file, err := os.Open(path)
		if err != nil {
			return result, err
		}

		reader, err := newCSVReaderWithBOM(file)
		if err != nil {
			_ = file.Close()
			return result, err
		}

		header, err := reader.Read()
		if err != nil {
			_ = file.Close()
			return result, err
		}

		sourceTextIndex := indexHeader(header, translatedCSVHeaderSourceText, "source_text", "text")
		translatedTextIndex := indexHeader(header, ksExtractHeaderTargetText, "translated_text", "translation")
		if sourceTextIndex < 0 || translatedTextIndex < 0 {
			_ = file.Close()
			return result, fmt.Errorf("translated csv missing required columns")
		}

		result.FilesProcessed++
		runtime.BeginFile(path)
		currentSession := runtime.Session()
		fileState, err := currentSession.PrepareTranslatedCSVFile(sourceFile)
		if err != nil {
			_ = file.Close()
			return result, err
		}
		lineNumber := 1
		for {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				_ = file.Close()
				return result, err
			}

			lineNumber++

			sourceText := textutil.NormalizeSourceText(recordValue(record, sourceTextIndex))
			translatedText := recordValue(record, translatedTextIndex)
			if sourceText == "" {
				result.ErrorLines++
				result.Messages = append(result.Messages, fmt.Sprintf("%s:%d missing required values", path, lineNumber))
				if err := runtime.LineProcessed(); err != nil {
					_ = file.Close()
					return result, err
				}
				continue
			}

			if runtime.Session() != currentSession {
				currentSession = runtime.Session()
				fileState, err = currentSession.PrepareTranslatedCSVFile(sourceFile)
				if err != nil {
					_ = file.Close()
					return result, err
				}
			}

			applyResult, err := fileState.Apply(sourceText, translatedText, req.AllowOverwrite)
			if err != nil {
				_ = file.Close()
				return result, err
			}
			result.Inserted += applyResult.Inserted
			result.Updated += applyResult.Updated
			result.Skipped += applyResult.Skipped
			for _, sourceArc := range applyResult.AmbiguousArcs {
				result.Messages = append(result.Messages, fmt.Sprintf("%s:%d ambiguous match in %s", path, lineNumber, sourceArc))
			}
			if err := runtime.LineProcessed(); err != nil {
				_ = file.Close()
				return result, err
			}
		}

		if err := file.Close(); err != nil {
			return result, err
		}
	}

	if err := runtime.Commit(); err != nil {
		return result, err
	}

	return result, nil
}
