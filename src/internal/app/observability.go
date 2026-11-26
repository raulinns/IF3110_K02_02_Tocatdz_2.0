package app

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/tracing"
)

// SetupObservability memasang logger JSON dan tracing OpenTelemetry untuk satu proses.
// Span selalu ditulis ke stdout; OTLP/HTTP ikut aktif jika OTEL_EXPORTER_OTLP_ENDPOINT diisi.
// Fungsi yang dikembalikan mengosongkan antrean span saat proses berhenti.
func SetupObservability(ctx context.Context, service string) (*slog.Logger, func(), error) {
	provider, shutdown, err := tracing.NewOpenTelemetry(ctx, service, os.Stdout)
	if err != nil {
		return nil, nil, err
	}
	tracing.Install(provider)
	logger := logging.New(os.Stdout)
	slog.SetDefault(logger)
	return logger, func() {
		flush, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := shutdown(flush); err != nil {
			logger.Warn("tracing shutdown failed", "event", "tracing.shutdown_failed", "error", err.Error())
		}
	}, nil
}
