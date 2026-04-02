package importer

import (
	"context"

	"COM3D2TranslateTool/internal/db"
	"COM3D2TranslateTool/internal/model"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	importProgressEventName           = "import:progress"
	defaultImportBatchLineThreshold   = 20000
	defaultImportProgressEmitInterval = 1000
)

type ImportRuntimeOptions struct {
	BatchLineThreshold   int
	ProgressEmitInterval int
}

type ImportRuntime struct {
	ctx                  context.Context
	store                *db.Store
	importer             string
	result               *model.ImportResult
	session              *db.ImportSession
	currentFile          string
	linesSinceCommit     int
	lastEmittedAtLine    int
	batchLineThreshold   int
	progressEmitInterval int
}

func NewImportRuntime(ctx context.Context, store *db.Store, importerName string, result *model.ImportResult) (*ImportRuntime, error) {
	return NewImportRuntimeWithOptions(ctx, store, importerName, result, ImportRuntimeOptions{})
}

func NewImportRuntimeWithOptions(ctx context.Context, store *db.Store, importerName string, result *model.ImportResult, options ImportRuntimeOptions) (*ImportRuntime, error) {
	session, err := store.BeginImportSession(ctx)
	if err != nil {
		return nil, err
	}

	batchLineThreshold := options.BatchLineThreshold
	if batchLineThreshold <= 0 {
		batchLineThreshold = defaultImportBatchLineThreshold
	}
	progressEmitInterval := options.ProgressEmitInterval
	if progressEmitInterval <= 0 {
		progressEmitInterval = defaultImportProgressEmitInterval
	}

	runtime := &ImportRuntime{
		ctx:                  ctx,
		store:                store,
		importer:             importerName,
		result:               result,
		session:              session,
		batchLineThreshold:   batchLineThreshold,
		progressEmitInterval: progressEmitInterval,
	}
	runtime.emit("starting")
	return runtime, nil
}

func (r *ImportRuntime) Session() *db.ImportSession {
	return r.session
}

func (r *ImportRuntime) BeginFile(path string) {
	r.currentFile = path
	r.emit("running")
}

func (r *ImportRuntime) LineProcessed() error {
	r.result.TotalLines++
	r.linesSinceCommit++

	if r.result.TotalLines == 1 || r.result.TotalLines-r.lastEmittedAtLine >= r.progressEmitInterval {
		r.lastEmittedAtLine = r.result.TotalLines
		r.emit("running")
	}

	if r.linesSinceCommit >= r.batchLineThreshold {
		return r.rotateSession()
	}

	return nil
}

func (r *ImportRuntime) Commit() error {
	if r == nil || r.session == nil {
		return nil
	}

	if r.linesSinceCommit > 0 || r.result.FilesProcessed > 0 {
		r.emit("committing")
	}
	if err := r.session.Commit(); err != nil {
		return err
	}

	r.session = nil
	r.linesSinceCommit = 0
	r.emit("completed")
	return nil
}

func (r *ImportRuntime) Rollback() error {
	if r == nil || r.session == nil {
		return nil
	}

	err := r.session.Rollback()
	r.session = nil
	return err
}

func (r *ImportRuntime) rotateSession() error {
	if r == nil || r.session == nil || r.linesSinceCommit < r.batchLineThreshold {
		return nil
	}

	r.emit("committing")
	if err := r.session.Commit(); err != nil {
		return err
	}

	session, err := r.store.BeginImportSession(r.ctx)
	if err != nil {
		r.session = nil
		return err
	}

	r.session = session
	r.linesSinceCommit = 0
	r.emit("running")
	return nil
}

func (r *ImportRuntime) emit(phase string) {
	if r == nil {
		return
	}

	progress := model.ImportProgress{
		Importer:       r.importer,
		CurrentFile:    r.currentFile,
		FilesProcessed: r.result.FilesProcessed,
		TotalLines:     r.result.TotalLines,
		Inserted:       r.result.Inserted,
		Updated:        r.result.Updated,
		Skipped:        r.result.Skipped,
		Unmatched:      r.result.Unmatched,
		ErrorLines:     r.result.ErrorLines,
		Phase:          phase,
	}
	EmitImportProgress(r.ctx, progress)
}

func EmitImportProgress(ctx context.Context, progress model.ImportProgress) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}

	wruntime.EventsEmit(ctx, importProgressEventName, progress)
}
