package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// ServeMetrics melayani /metrics di alamat terpisah dari API publik. Alamat kosong berarti dimatikan.
// Fungsi mengembalikan fungsi berhenti yang menunggu listener selesai.
func ServeMetrics(addr string, handler http.Handler, logger *slog.Logger) func() {
	if addr == "" {
		return func() {}
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", handler)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics listener stopped", "event", "metrics.stopped", "error", err.Error())
		}
	}()
	logger.Info("metrics listener started", "event", "metrics.start", "address", addr)
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		<-done
	}
}
