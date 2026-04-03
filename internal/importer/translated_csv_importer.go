package importer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"sync"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
	"COM3D2TranslateTool/internal/textutil"
)

const translatedCSVHeaderSourceText = "\u539f\u6587"

const (
	translatedCSVImportBatchLineThreshold   = 100000
	translatedCSVImportProgressEmitInterval = 2000
	translatedCSVImportMaxParseWorkers      = 8
)

type TranslatedCSVImporter struct {
	store *db.Store
}

type translatedCSVParsedLine struct {
	lineNumber     int
	sourceText     string
	translatedText string
	errorMessage   string
}

type translatedCSVParsedFile struct {
	index      int
	path       string
	sourceFile string
	lines      []translatedCSVParsedLine
}

type translatedCSVParseJob struct {
	index int
	path  string
}

type translatedCSVParseResult struct {
	index int
	file  translatedCSVParsedFile
	err   error
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

func translatedCSVParseWorkerCount(fileCount int) int {
	if fileCount <= 0 {
		return 1
	}

	workers := stdruntime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > translatedCSVImportMaxParseWorkers {
		workers = translatedCSVImportMaxParseWorkers
	}
	if workers > fileCount {
		workers = fileCount
	}
	if workers <= 0 {
		return 1
	}
	return workers
}

func parseTranslatedCSVFile(path string, index int) (translatedCSVParsedFile, error) {
	fileState := translatedCSVParsedFile{
		index:      index,
		path:       path,
		sourceFile: normalizeTranslatedCSVSourceFile(path),
		lines:      make([]translatedCSVParsedLine, 0, 32),
	}

	file, err := os.Open(path)
	if err != nil {
		return fileState, err
	}
	defer file.Close()

	reader, err := newCSVReaderWithBOM(file)
	if err != nil {
		return fileState, err
	}

	header, err := reader.Read()
	if err != nil {
		return fileState, err
	}

	sourceTextIndex := indexHeader(header, translatedCSVHeaderSourceText, "source_text", "text")
	translatedTextIndex := indexHeader(header, ksExtractHeaderTargetText, "translated_text", "translation")
	if sourceTextIndex < 0 || translatedTextIndex < 0 {
		return fileState, fmt.Errorf("%s: translated csv missing required columns", path)
	}

	lineNumber := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fileState, fmt.Errorf("%s: %w", path, err)
		}

		lineNumber++
		line := translatedCSVParsedLine{
			lineNumber:     lineNumber,
			sourceText:     textutil.NormalizeSourceText(recordValue(record, sourceTextIndex)),
			translatedText: recordValue(record, translatedTextIndex),
		}
		if line.sourceText == "" {
			line.errorMessage = fmt.Sprintf("%s:%d missing required values", path, lineNumber)
		}
		fileState.lines = append(fileState.lines, line)
	}

	return fileState, nil
}

func startTranslatedCSVParsePipeline(ctx context.Context, files []string) <-chan translatedCSVParseResult {
	results := make(chan translatedCSVParseResult, translatedCSVParseWorkerCount(len(files))*2)
	jobs := make(chan translatedCSVParseJob, translatedCSVParseWorkerCount(len(files))*2)

	var wg sync.WaitGroup
	workerCount := translatedCSVParseWorkerCount(len(files))
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobs:
					if !ok {
						return
					}

					fileState, err := parseTranslatedCSVFile(job.path, job.index)
					result := translatedCSVParseResult{
						index: job.index,
						file:  fileState,
						err:   err,
					}

					select {
					case <-ctx.Done():
						return
					case results <- result:
					}
				}
			}
		}()
	}

	go func() {
		defer close(results)
		for index, path := range files {
			select {
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				return
			case jobs <- translatedCSVParseJob{index: index, path: path}:
			}
		}
		close(jobs)
		wg.Wait()
	}()

	return results
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

	sourceFiles := make([]string, 0, len(files))
	for _, path := range files {
		sourceFiles = append(sourceFiles, normalizeTranslatedCSVSourceFile(path))
	}
	sourceArcCache, err := i.store.FindSourceArcsBySourceFiles(sourceFiles)
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

	currentSession := runtime.Session()
	currentSession.SeedSourceFileArcCache(sourceArcCache)
	seededSession := currentSession

	parseCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	parseResults := startTranslatedCSVParsePipeline(parseCtx, files)
	pending := make(map[int]translatedCSVParseResult, translatedCSVParseWorkerCount(len(files))*2)

	for nextIndex := 0; nextIndex < len(files); {
		pendingResult, ok := pending[nextIndex]
		if !ok {
			parseResult, ok := <-parseResults
			if !ok {
				return result, fmt.Errorf("translated csv parse pipeline ended unexpectedly")
			}
			pending[parseResult.index] = parseResult
			continue
		}
		delete(pending, nextIndex)
		if pendingResult.err != nil {
			cancel()
			return result, pendingResult.err
		}

		result.FilesProcessed++
		runtime.BeginFile(pendingResult.file.path)

		if runtime.Session() != currentSession {
			currentSession = runtime.Session()
		}
		if currentSession != seededSession {
			currentSession.SeedSourceFileArcCache(sourceArcCache)
			seededSession = currentSession
		}

		fileState, err := currentSession.PrepareTranslatedCSVFile(pendingResult.file.sourceFile)
		if err != nil {
			cancel()
			return result, err
		}

		for _, line := range pendingResult.file.lines {
			if line.errorMessage != "" {
				result.ErrorLines++
				result.Messages = append(result.Messages, line.errorMessage)
				if err := runtime.LineProcessed(); err != nil {
					cancel()
					return result, err
				}
				continue
			}

			if runtime.Session() != currentSession {
				currentSession = runtime.Session()
				if currentSession != seededSession {
					currentSession.SeedSourceFileArcCache(sourceArcCache)
					seededSession = currentSession
				}
				fileState, err = currentSession.PrepareTranslatedCSVFile(pendingResult.file.sourceFile)
				if err != nil {
					cancel()
					return result, err
				}
			}

			applyResult, err := fileState.Apply(line.sourceText, line.translatedText, req.AllowOverwrite)
			if err != nil {
				cancel()
				return result, err
			}
			result.Inserted += applyResult.Inserted
			result.Updated += applyResult.Updated
			result.Skipped += applyResult.Skipped
			for _, sourceArc := range applyResult.AmbiguousArcs {
				result.Messages = append(result.Messages, fmt.Sprintf("%s:%d ambiguous match in %s", pendingResult.file.path, line.lineNumber, sourceArc))
			}
			if err := runtime.LineProcessed(); err != nil {
				cancel()
				return result, err
			}
		}

		nextIndex++
	}

	if err := runtime.Commit(); err != nil {
		return result, err
	}

	return result, nil
}
