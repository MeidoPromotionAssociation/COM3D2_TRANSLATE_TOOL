package exporter

import (
	"context"

	"COM3D2TranslateTool/internal/model"
)

type Exporter interface {
	Name() string
	Export(ctx context.Context, req model.ExportRequest) (model.ExportResult, error)
}
