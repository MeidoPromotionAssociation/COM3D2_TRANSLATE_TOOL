package importer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/textutil"
)

type ArcKSFolderTextImporter struct {
	store *db.Store
}

func NewArcKSFolderTextImporter(store *db.Store) *ArcKSFolderTextImporter {
	return &ArcKSFolderTextImporter{store: store}
}

func (i *ArcKSFolderTextImporter) Name() string {
	return "arc-ks-folder-text"
}

func normalizeImportArc(dirName string) string {
	if strings.HasSuffix(strings.ToLower(dirName), ".arc_extracted") {
		return dirName[:len(dirName)-len("_extracted")]
	}
	return dirName
}

func normalizeImportSourceFile(fileName string) string {
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	return base + ".ks"
}

func splitImportLine(line string) (string, string, bool) {
	start := strings.IndexRune(line, '\t')
	if start == -1 {
		return "", "", false
	}

	end := start
	for end < len(line) && line[end] == '\t' {
		end++
	}

	left := textutil.NormalizeSourceText(line[:start])
	right := strings.TrimSpace(line[end:])
	if left == "" || right == "" {
		return "", "", false
	}
	return left, right, true
}

func (i *ArcKSFolderTextImporter) Import(ctx context.Context, req model.ImportRequest) (model.ImportResult, error) {
	rootDir := strings.TrimSpace(req.RootDir)
	if rootDir == "" {
		return model.ImportResult{}, fmt.Errorf("import directory is required")
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

	err = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".txt") {
			return nil
		}

		sourceArc := normalizeImportArc(filepath.Base(filepath.Dir(path)))
		sourceFile := normalizeImportSourceFile(d.Name())
		result.FilesProcessed++
		runtime.BeginFile(path)

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := strings.TrimRight(scanner.Text(), "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}

			sourceText, translatedText, ok := splitImportLine(line)
			if !ok {
				result.ErrorLines++
				result.Messages = append(result.Messages, fmt.Sprintf("%s:%d invalid line", path, lineNo))
				if err := runtime.LineProcessed(); err != nil {
					return err
				}
				continue
			}

			matched, updated, skipped, err := runtime.Session().UpdateImportedTranslation(sourceArc, sourceFile, sourceText, translatedText, req.AllowOverwrite)
			if err != nil {
				return err
			}
			if !matched {
				result.Unmatched++
			} else if skipped {
				result.Skipped++
			} else if updated {
				result.Updated++
			}

			if err := runtime.LineProcessed(); err != nil {
				return err
			}
		}

		return scanner.Err()
	})
	if err != nil {
		return result, err
	}
	if err := runtime.Commit(); err != nil {
		return result, err
	}

	return result, nil
}
