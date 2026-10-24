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
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/web"
)

type databasePool interface {
	Close()
}

type databaseOpener func(
	context.Context,
	platformdb.Config,
) (databasePool, error)

func main() {
	if err := run(); err != nil {
		slog.Error(
			"application stopped with an error",
			"error",
			err,
		)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := logging.New(cfg.AppEnv, os.Stdout)
	slog.SetDefault(logger)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           web.NewRouter(logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	return runApplication(
		ctx,
		cfg,
		logger,
		server,
		func(
			ctx context.Context,
			cfg platformdb.Config,
		) (databasePool, error) {
			return platformdb.OpenMigrated(ctx, cfg)
		},
	)
}

func runApplication(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	server app.HTTPServer,
	openDatabase databaseOpener,
) error {
	pool, err := openDatabase(
		ctx,
		platformdb.Config{
			URL: cfg.DatabaseURL,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"open platform database: %w",
			err,
		)
	}
	defer pool.Close()

	logger.Info("platform database connected")

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
