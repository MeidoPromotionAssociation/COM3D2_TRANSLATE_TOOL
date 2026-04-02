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

type ArcSourceTextFileImporter struct {
	store *db.Store
}

func NewArcSourceTextFileImporter(store *db.Store) *ArcSourceTextFileImporter {
	return &ArcSourceTextFileImporter{store: store}
}

func (i *ArcSourceTextFileImporter) Name() string {
	return "arc-source-text-file"
}

func normalizeArcFromTextFilename(fileName string) string {
	base := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	if strings.HasSuffix(strings.ToLower(base), ".arc") {
		return base
	}
	return base + ".arc"
}

func parseArcSourceTextLine(line string) (sourceText, translatedText string, hasTranslation, ok bool) {
	line = strings.TrimRight(line, "\r")
	line = strings.TrimPrefix(line, "\uFEFF")
	if textutil.IsBlankSourceText(line) {
		return "", "", false, false
	}

	start := strings.IndexRune(line, '\t')
	if start == -1 {
		return line, "", false, true
	}

	end := start
	for end < len(line) && line[end] == '\t' {
		end++
	}

	sourceText = textutil.NormalizeSourceText(line[:start])
	if sourceText == "" {
		return "", "", false, false
	}

	translatedText = strings.TrimSpace(line[end:])
	if translatedText == "" {
		return sourceText, "", false, true
	}
	return sourceText, translatedText, true, true
}

func collectArcSourceTextFiles(rootPath string) ([]string, error) {
	info, err := os.Stat(rootPath)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(info.Name()), ".txt") {
			return nil, fmt.Errorf("import source must be a .txt file or a directory containing .txt files")
		}
		return []string{rootPath}, nil
	}

	files := make([]string, 0)
	err = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".txt") {
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

func (i *ArcSourceTextFileImporter) Import(ctx context.Context, req model.ImportRequest) (model.ImportResult, error) {
	rootPath := strings.TrimSpace(req.RootDir)
	if rootPath == "" {
		return model.ImportResult{}, fmt.Errorf("import file or directory is required")
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

	files, err := collectArcSourceTextFiles(rootPath)
	if err != nil {
		return model.ImportResult{}, err
	}

	for _, path := range files {
		sourceArc := normalizeArcFromTextFilename(filepath.Base(path))
		result.FilesProcessed++
		runtime.BeginFile(path)

		file, err := os.Open(path)
		if err != nil {
			return result, err
		}

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			sourceText, translatedText, hasTranslation, ok := parseArcSourceTextLine(line)
			if !ok {
				result.ErrorLines++
				result.Messages = append(result.Messages, fmt.Sprintf("%s:%d invalid line", path, lineNo))
				if err := runtime.LineProcessed(); err != nil {
					_ = file.Close()
					return result, err
				}
				continue
			}

			matchCount, updated, skipped, err := runtime.Session().UpdateImportedTranslationByArcAndText(
				sourceArc,
				sourceText,
				translatedText,
				req.AllowOverwrite,
			)
			if err != nil {
				_ = file.Close()
				return result, err
			}
			if matchCount == 0 {
				result.Unmatched++
			} else if !hasTranslation {
				result.Skipped++
			} else if matchCount > 1 {
				result.Skipped++
				result.Messages = append(result.Messages, fmt.Sprintf("%s:%d ambiguous match in %s (%d entries)", path, lineNo, sourceArc, matchCount))
			} else if skipped {
				result.Skipped++
			} else if updated {
				result.Updated++
			}
			if err := runtime.LineProcessed(); err != nil {
				_ = file.Close()
				return result, err
			}
		}

		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return result, err
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
