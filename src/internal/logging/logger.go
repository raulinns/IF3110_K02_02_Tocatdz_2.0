package logging

import (
	"io"
	"log/slog"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
)

// New keeps the existing server and worker constructor signature.
func New(_ string, output io.Writer) *slog.Logger { return logging.New(output) }
