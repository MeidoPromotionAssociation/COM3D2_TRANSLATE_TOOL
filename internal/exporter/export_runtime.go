package exporter

import (
	"context"

	"COM3D2TranslateTool/internal/model"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	exportProgressEventName        = "export:progress"
	exportProgressEmitIntervalRows = 1000
)

type ExportRuntime struct {
	ctx              context.Context
	progress         model.ExportProgress
	lastEmittedAtRow int
}

func NewExportRuntime(ctx context.Context, exporterName, outputPath string) *ExportRuntime {
	runtime := &ExportRuntime{
		ctx: ctx,
		progress: model.ExportProgress{
			Exporter:   exporterName,
			OutputPath: outputPath,
		},
	}
	runtime.emit("starting")
	return runtime
}

func (r *ExportRuntime) RowExported() {
	r.progress.ProcessedRows++
	r.progress.Exported++
	r.maybeEmitRunning()
}

func (r *ExportRuntime) RowSkipped() {
	r.progress.ProcessedRows++
	r.progress.Skipped++
	r.maybeEmitRunning()
}

func (r *ExportRuntime) Flush() {
	r.emit("flushing")
}

func (r *ExportRuntime) Complete() {
	r.emit("completed")
}

func (r *ExportRuntime) Fail() {
	r.emit("failed")
}

func (r *ExportRuntime) maybeEmitRunning() {
	if r.progress.ProcessedRows == 1 || r.progress.ProcessedRows-r.lastEmittedAtRow >= exportProgressEmitIntervalRows {
		r.lastEmittedAtRow = r.progress.ProcessedRows
		r.emit("running")
	}
}

func (r *ExportRuntime) emit(phase string) {
	if r == nil {
		return
	}

	progress := r.progress
	progress.Phase = phase
	EmitExportProgress(r.ctx, progress)
}

func EmitExportProgress(ctx context.Context, progress model.ExportProgress) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}

	wruntime.EventsEmit(ctx, exportProgressEventName, progress)
}
