package translation

import (
	"context"
	"sync"
	"time"

	"COM3D2TranslateTool/internal/model"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const translateProgressEventName = "translate:progress"
const translateLogEventName = "translate:log"

type Runtime struct {
	ctx      context.Context
	mu       sync.Mutex
	progress model.TranslateProgress
}

func NewRuntime(ctx context.Context, translatorName, targetField string, total int) *Runtime {
	runtime := &Runtime{
		ctx: ctx,
		progress: model.TranslateProgress{
			Translator:  translatorName,
			TargetField: NormalizeTargetField(targetField),
			Total:       total,
		},
	}
	runtime.emit("starting")
	return runtime
}

func (r *Runtime) MarkRunning(currentItem string) {
	r.mu.Lock()
	r.progress.CurrentItem = currentItem
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("running", progress)
}

func (r *Runtime) MarkUpdated(currentItem string) {
	r.mu.Lock()
	r.progress.CurrentItem = currentItem
	r.progress.Processed++
	r.progress.Updated++
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("running", progress)
}

func (r *Runtime) MarkSkipped(currentItem string) {
	r.mu.Lock()
	r.progress.CurrentItem = currentItem
	r.progress.Processed++
	r.progress.Skipped++
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("running", progress)
}

func (r *Runtime) MarkFailed(currentItem string, count int) {
	r.mu.Lock()
	r.progress.CurrentItem = currentItem
	r.progress.Failed += count
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("failed", progress)
}

func (r *Runtime) Complete() {
	r.mu.Lock()
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("completed", progress)
}

func (r *Runtime) Stopped() {
	r.mu.Lock()
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress("stopped", progress)
}

func (r *Runtime) emit(phase string) {
	if r == nil {
		return
	}

	r.mu.Lock()
	progress := r.progress
	r.mu.Unlock()
	r.emitProgress(phase, progress)
}

func (r *Runtime) emitProgress(phase string, progress model.TranslateProgress) {
	progress.Phase = phase
	EmitProgress(r.ctx, progress)
}

func EmitProgress(ctx context.Context, progress model.TranslateProgress) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}
	wruntime.EventsEmit(ctx, translateProgressEventName, progress)
}

func EmitLog(ctx context.Context, log model.TranslateLog) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}
	if log.Timestamp == "" {
		log.Timestamp = time.Now().Format(time.RFC3339)
	}
	wruntime.EventsEmit(ctx, translateLogEventName, log)
}
