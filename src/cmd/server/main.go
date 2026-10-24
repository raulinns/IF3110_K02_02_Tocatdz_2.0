package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/app"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/bootstrap"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb"
)

type databasePool interface{ Close() }
type databaseOpener func(context.Context, platformdb.Config) (databasePool, error)

type applicationResources struct {
	platform *bootstrap.Platform
	stop     context.CancelFunc
	done     <-chan struct{}
}

func (r *applicationResources) Close() {
	r.stop()
	<-r.done
	r.platform.Close()
}

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// .env hanya untuk pengembangan lokal; variabel environment yang sudah diset tetap diutamakan.
	for _, path := range []string{".env", "../.env"} {
		if err := config.LoadDotEnv(path); err != nil {
			return err
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	sec, err := config.LoadSecurity(cfg.AppEnv)
	if err != nil {
		return err
	}

	logger := logging.New(cfg.AppEnv, os.Stdout)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return runApplication(ctx, cfg, logger, server, func(ctx context.Context, _ platformdb.Config) (databasePool, error) {
		platform, err := bootstrap.Build(ctx, logger, cfg, sec)
		if err != nil {
			return nil, err
		}
		server.Handler = platform.Handler
		workerCtx, stopWorker := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { defer close(done); platform.RunWorker(workerCtx) }()
		return &applicationResources{platform: platform, stop: stopWorker, done: done}, nil
	})
}

func runApplication(ctx context.Context, cfg config.Config, logger *slog.Logger, server app.HTTPServer, openDatabase databaseOpener) error {
	resources, err := openDatabase(ctx, platformdb.Config{URL: cfg.DatabaseURL})
	if err != nil {
		return fmt.Errorf("open platform database: %w", err)
	}
	defer resources.Close()

	logger.Info(
		"starting HTTP server",
		"environment",
		cfg.AppEnv,
		"address",
		cfg.HTTPAddr,
	)

	if err := app.Run(ctx, server); err != nil {
		return err
	}

	logger.Info("HTTP server stopped gracefully")

	return nil
}
