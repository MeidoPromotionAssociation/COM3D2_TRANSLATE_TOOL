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

type VoiceSubtitleTextExporter struct {
	store *db.Store
}

func NewVoiceSubtitleTextExporter(store *db.Store) *VoiceSubtitleTextExporter {
	return &VoiceSubtitleTextExporter{store: store}
}

func (e *VoiceSubtitleTextExporter) Name() string {
	return "voice-subtitle-text"
}

func (e *VoiceSubtitleTextExporter) Export(ctx context.Context, req model.ExportRequest) (model.ExportResult, error) {
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
	err = e.store.StreamDistinctVoiceSubtitleRows(ctx, req, func(voiceID, finalText string) error {
		if !isJATExportableSource(voiceID) {
			skipped++
			runtime.RowSkipped()
			return nil
		}

		sourceField, translatedField := formatJATTextTranslationLine(voiceID, finalText)
		if _, err := writer.WriteString(sourceField); err != nil {
			return err
		}
		if err := writer.WriteByte('\t'); err != nil {
			return err
		}
		if _, err := writer.WriteString(translatedField); err != nil {
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
		Message:    fmt.Sprintf("exported %d voice subtitle lines to %s, skipped %d unsupported lines", exported, req.OutputPath, skipped),
	}, nil
}
