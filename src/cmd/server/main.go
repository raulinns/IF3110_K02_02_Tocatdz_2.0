package main

import (
	"context"
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
)

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

	platform, err := bootstrap.Build(ctx, logger, cfg, sec)
	if err != nil {
		return err
	}
	defer platform.Close()

	go platform.RunWorker(ctx)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           platform.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Info(
		"starting HTTP server",
		"environment",
		cfg.AppEnv,
		"address",
		server.Addr,
		"public_origin",
		sec.PublicOrigin,
	)

	if err := app.Run(ctx, server); err != nil {
		return err
	}

	logger.Info("HTTP server stopped gracefully")

	return nil
}
