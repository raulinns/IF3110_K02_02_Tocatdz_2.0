package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const shutdownTimeout = 10 * time.Second

type HTTPServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

func Run(ctx context.Context, server HTTPServer) error {
	serveResult := make(chan error, 1)

	go func() {
		serveResult <- server.ListenAndServe()
	}()

	select {
	case err := <-serveResult:
		return normalizeServeError(err)

	case <-ctx.Done():
		return shutdown(server, serveResult)
	}
}

func shutdown(
	server HTTPServer,
	serveResult <-chan error,
) error {
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		shutdownTimeout,
	)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}

	select {
	case err := <-serveResult:
		return normalizeServeError(err)

	case <-shutdownCtx.Done():
		return fmt.Errorf(
			"wait for HTTP server shutdown: %w",
			shutdownCtx.Err(),
		)
	}
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return fmt.Errorf("serve HTTP: %w", err)
}
