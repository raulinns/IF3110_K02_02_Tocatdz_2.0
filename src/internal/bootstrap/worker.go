package bootstrap

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/config"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	mysqltarget "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/mysql"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/metrics"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/targetdb"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/worker"
)

type RevalidatorFactory func(reader migration.RevalidationReader, clock security.Clock) (migration.Revalidator, error)

var DefaultRevalidator RevalidatorFactory = migration.NewRevalidator

var ErrRevalidatorMissing = errors.New("migration revalidator is not wired: worker cannot execute jobs safely")

type WorkerOptions struct {
	Owner       string
	Concurrency int
	Revalidator RevalidatorFactory
}

type WorkerRuntime struct {
	Worker  *worker.Worker
	targets *targetdb.Router
	pool    *pgxpool.Pool
	metrics *metrics.Registry
}

func BuildWorker(ctx context.Context, logger *slog.Logger, cfg config.Config, connectionKey security.Secret, opts WorkerOptions) (*WorkerRuntime, error) {
	if opts.Revalidator == nil {
		return nil, ErrRevalidatorMissing
	}
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	runtime := &WorkerRuntime{pool: pool}
	if err := runtime.build(ctx, logger, connectionKey, opts); err != nil {
		runtime.Close()
		return nil, err
	}
	return runtime, nil
}

func (r *WorkerRuntime) build(ctx context.Context, logger *slog.Logger, connectionKey security.Secret, opts WorkerOptions) error {
	if err := postgres.Migrate(ctx, r.pool); err != nil {
		return err
	}
	clock := security.SystemClock{}
	box, err := postgres.NewSecretBox(connectionKey)
	if err != nil {
		return err
	}
	postgresTargets, err := postgres.NewTargets(r.pool, box)
	if err != nil {
		return err
	}
	mysqlTargets, err := mysqltarget.NewTargets(r.pool, box)
	if err != nil {
		postgresTargets.Close()
		return err
	}
	if r.targets, err = targetdb.NewRouter(r.pool, postgresTargets, mysqlTargets); err != nil {
		mysqlTargets.Close()
		postgresTargets.Close()
		return err
	}
	store, err := postgres.NewM2Store(r.pool)
	if err != nil {
		return err
	}
	r.metrics = newMetrics(r.pool, r.targets, store)
	r.Worker, err = newWorker(store, r.targets, opts.Revalidator, clock, logger, opts.Owner, opts.Concurrency, r.metrics)
	return err
}

func newMetrics(pool *pgxpool.Pool, targets *targetdb.Router, store *postgres.M2Store) *metrics.Registry {
	registry := metrics.New()
	registry.WatchJobs(store)
	registry.WatchPool("platform", func() postgres.PoolStats { return postgres.PlatformPoolStats(pool) })
	registry.WatchPool("target", targets.PoolStats)
	return registry
}

func (r *WorkerRuntime) MetricsHandler() http.Handler { return r.metrics.Handler() }

func newM2Worker(store *postgres.M2Store, targets *targetdb.Router, clock security.Clock, logger *slog.Logger, owner string, concurrency int, registry *metrics.Registry) (*worker.Worker, error) {
	return newWorker(store, targets, DefaultRevalidator, clock, logger, owner, concurrency, registry)
}

func newWorker(store *postgres.M2Store, targets *targetdb.Router, factory RevalidatorFactory, clock security.Clock, logger *slog.Logger, owner string, concurrency int, registry *metrics.Registry) (*worker.Worker, error) {
	if factory == nil {
		return nil, ErrRevalidatorMissing
	}
	revalidator, err := factory(store, clock)
	if err != nil {
		return nil, err
	}
	runner, err := worker.NewModeRunner(clock)
	if err != nil {
		return nil, err
	}
	return worker.New(worker.Config{Store: store, Coordinator: targets, Revalidator: revalidator, Runner: runner,
		Progress: registry.Progress(store), Observer: registry, Clock: clock, Logger: logger, Owner: owner, Concurrency: concurrency})
}

func (r *WorkerRuntime) Run(ctx context.Context) { r.Worker.Run(ctx) }

func (r *WorkerRuntime) Close() {
	if r.targets != nil {
		r.targets.Close()
	}
	if r.pool != nil {
		r.pool.Close()
	}
}
