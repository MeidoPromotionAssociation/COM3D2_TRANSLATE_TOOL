package exporter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"
)

type EntryJSONLExporter struct {
	store *db.Store
}

func NewEntryJSONLExporter(store *db.Store) *EntryJSONLExporter {
	return &EntryJSONLExporter{store: store}
}

func (e *EntryJSONLExporter) Name() string {
	return "entry-jsonl"
}

func (e *EntryJSONLExporter) Export(ctx context.Context, req model.ExportRequest) (model.ExportResult, error) {
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
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)

	exported := 0
	err = e.store.StreamExportEntries(ctx, req, func(entry model.Entry) error {
		if err := encoder.Encode(entry); err != nil {
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
		Skipped:    0,
		Message:    fmt.Sprintf("exported %d entries to %s", exported, req.OutputPath),
	}, nil
}
