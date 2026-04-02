package importer

import (
	"context"

	"COM3D2TranslateTool/internal/model"
)

type Importer interface {
	Name() string
	Import(ctx context.Context, req model.ImportRequest) (model.ImportResult, error)
}
