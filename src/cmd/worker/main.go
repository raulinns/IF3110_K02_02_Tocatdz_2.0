// Command worker menjalankan job migration dari queue.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/app"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/bootstrap"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
)

const defaultConcurrency = 2

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run() error {
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
	options, err := workerOptions(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger, flushTraces, err := app.SetupObservability(ctx, "tocatdz-worker")
	if err != nil {
		return err
	}
	defer flushTraces()
	runtime, err := bootstrap.BuildWorker(ctx, logger, cfg, sec.ConnectionKey, options)
	if err != nil {
		return err
	}
	defer runtime.Close()
	defer app.ServeMetrics(os.Getenv("METRICS_ADDR"), runtime.MetricsHandler(), logger)()
	logger.Info("starting worker", "event", "worker.start", "worker", options.Owner, "concurrency", options.Concurrency)
	runtime.Run(ctx)
	logger.Info("worker stopped gracefully", "event", "worker.stop", "worker", options.Owner)
	return nil
}

func workerOptions(getenv func(string) string) (bootstrap.WorkerOptions, error) {
	options := bootstrap.WorkerOptions{Owner: getenv("WORKER_ID"), Concurrency: defaultConcurrency, Revalidator: bootstrap.DefaultRevalidator}
	if options.Owner == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "worker"
		}
		options.Owner = host + "-" + strconv.Itoa(os.Getpid())
	}
	if raw := getenv("WORKER_CONCURRENCY"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 64 {
			return options, fmt.Errorf("WORKER_CONCURRENCY must be an integer from 1 to 64")
		}
		options.Concurrency = value
	}
	return options, nil
}
