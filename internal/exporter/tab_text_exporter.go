package exporter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
)

type TabTextExporter struct {
	store *db.Store
}

func NewTabTextExporter(store *db.Store) *TabTextExporter {
	return &TabTextExporter{store: store}
}

func (e *TabTextExporter) Name() string {
	return "tab-text"
}

func (e *TabTextExporter) Export(ctx context.Context, req model.ExportRequest) (model.ExportResult, error) {
	if req.OutputPath == "" {
		return model.ExportResult{}, fmt.Errorf("output path is required")
	}

	runtime := NewExportRuntime(ctx, e.Name(), req.OutputPath)

	if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o755); err != nil {
		runtime.Fail()
		return model.ExportResult{}, err
	}

	file, err := os.Create(req.OutputPath)
	if err != nil {
		runtime.Fail()
		return model.ExportResult{}, err
	}
	defer file.Close()

	writer := bufio.NewWriterSize(file, 1024*1024)
	exported := 0
	skipped := 0
	err = e.store.StreamDistinctTabTextRows(ctx, req, func(sourceText, finalText string) error {
		if !isJATExportableSource(sourceText) {
			skipped++
			runtime.RowSkipped()
			return nil
		}
		sourceField, finalField := formatJATTextTranslationLine(sourceText, finalText)
		if _, err := writer.WriteString(sourceField); err != nil {
			return err
		}
		if err := writer.WriteByte('\t'); err != nil {
			return err
		}
		if _, err := writer.WriteString(finalField); err != nil {
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return err
		}
		exported++
		runtime.RowExported()
		return nil
	})
	if err != nil {
		runtime.Fail()
		return model.ExportResult{}, err
	}

	runtime.Flush()
	if err := writer.Flush(); err != nil {
		runtime.Fail()
		return model.ExportResult{}, err
	}
	runtime.Complete()

	return model.ExportResult{
		Exporter:   e.Name(),
		OutputPath: req.OutputPath,
		Exported:   exported,
		Skipped:    skipped,
		Message:    fmt.Sprintf("exported %d lines to %s, skipped %d unsupported lines", exported, req.OutputPath, skipped),
	}, nil
}

func formatJATTextTranslationLine(sourceText, finalText string) (string, string) {
	return escapeJATTextField(sourceText), escapeJATTextField(finalText)
}

func isJATExportableSource(sourceText string) bool {
	if sourceText == "" {
		return true
	}
	switch sourceText[0] {
	case ';', '$':
		return false
	default:
		return true
	}
}

func escapeJATTextField(value string) string {
	if value == "" {
		return ""
	}

	escaped := make([]byte, 0, len(value)+8)
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case 0:
			escaped = append(escaped, '\\', '0')
		case '\a':
			escaped = append(escaped, '\\', 'a')
		case '\b':
			escaped = append(escaped, '\\', 'b')
		case '\t':
			escaped = append(escaped, '\\', 't')
		case '\n':
			escaped = append(escaped, '\\', 'n')
		case '\v':
			escaped = append(escaped, '\\', 'v')
		case '\f':
			escaped = append(escaped, '\\', 'f')
		case '\r':
			escaped = append(escaped, '\\', 'r')
		case '\'':
			escaped = append(escaped, '\\', '\'')
		case '\\':
			escaped = append(escaped, '\\', '\\')
		case '"':
			escaped = append(escaped, '\\', '"')
		default:
			escaped = append(escaped, value[i])
		}
	}

	return string(escaped)
}
