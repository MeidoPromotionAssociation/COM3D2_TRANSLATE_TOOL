package service

import (
	"context"
	"sync"

	"COM3D2TranslateTool/internal/model"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const maintenanceProgressEventName = "maintenance:progress"

type maintenanceRuntime struct {
	ctx      context.Context
	mu       sync.Mutex
	progress model.MaintenanceProgress
}

func newMaintenanceRuntime(ctx context.Context, operation string, totalSourceTexts int) *maintenanceRuntime {
	runtime := &maintenanceRuntime{
		ctx: ctx,
		progress: model.MaintenanceProgress{
			Operation:        operation,
			TotalSourceTexts: totalSourceTexts,
		},
	}
	runtime.emit("starting")
	return runtime
}

func (r *maintenanceRuntime) MarkRunning(currentSourceText string, processedSourceTexts, filledEntries int) {
	r.mu.Lock()
	r.progress.CurrentSourceText = currentSourceText
	r.progress.ProcessedSourceTexts = processedSourceTexts
	r.progress.FilledEntries = filledEntries
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("running", progress)
}

func (r *maintenanceRuntime) MarkCommitting(filledEntries int) {
	r.mu.Lock()
	r.progress.FilledEntries = filledEntries
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("committing", progress)
}

func (r *maintenanceRuntime) MarkFailed() {
	r.mu.Lock()
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("failed", progress)
}

func (r *maintenanceRuntime) Complete(filledEntries int) {
	r.mu.Lock()
	r.progress.FilledEntries = filledEntries
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("completed", progress)
}

func (r *maintenanceRuntime) Stopped() {
	r.mu.Lock()
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("stopped", progress)
}

func (r *maintenanceRuntime) emit(phase string) {
	if r == nil {
		return
	}

	r.mu.Lock()
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress(phase, progress)
}

func (r *maintenanceRuntime) emitProgress(phase string, progress model.MaintenanceProgress) {
	progress.Phase = phase
	EmitMaintenanceProgress(r.ctx, progress)
}

func EmitMaintenanceProgress(ctx context.Context, progress model.MaintenanceProgress) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}
	wruntime.EventsEmit(ctx, maintenanceProgressEventName, progress)
}
