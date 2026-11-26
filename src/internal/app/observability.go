package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/tracing"
)

// SetupObservability memasang tracing OpenTelemetry dan logger JSON untuk satu proses.
// Span selalu ditulis ke stdout; OTLP/HTTP ikut aktif jika endpoint OTEL diisi.
// Fungsi yang dikembalikan mengosongkan antrean span dan dipanggil paling akhir saat proses berhenti.
func SetupObservability(ctx context.Context, service string) (*slog.Logger, func(), error) {
	shutdown, err := tracing.StartProcess(ctx, service, os.Stdout)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize tracing: %w", err)
	}
	logger := logging.New(os.Stdout)
	slog.SetDefault(logger)
	return logger, func() {
		if err := shutdown(); err != nil {
			logger.Error("trace shutdown failed", "event", "tracing.shutdown", "status", "failed")
		}
	}, nil
}
