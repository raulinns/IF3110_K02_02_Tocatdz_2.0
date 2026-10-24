package platformdb

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultStartupTimeout = 5 * time.Second

var (
	ErrURLRequired           = errors.New("platform database URL is required")
	ErrInvalidStartupTimeout = errors.New(
		"platform database startup timeout must not be negative",
	)
)

type Config struct {
	URL            string
	StartupTimeout time.Duration
}

type pingCloser interface {
	Ping(context.Context) error
	Close()
}

type poolOpener[T pingCloser] func(
	context.Context,
	string,
) (T, error)

func Open(
	ctx context.Context,
	cfg Config,
) (*pgxpool.Pool, error) {
	return openPool(
		ctx,
		cfg,
		func(
			ctx context.Context,
			url string,
		) (*pgxpool.Pool, error) {
			return pgxpool.New(ctx, url)
		},
	)
}

func OpenMigrated(
	ctx context.Context,
	cfg Config,
) (*pgxpool.Pool, error) {
	return openMigrated(ctx, cfg, Open, Migrate)
}

type closeOnly interface {
	Close()
}

func openMigrated[T closeOnly](
	ctx context.Context,
	cfg Config,
	opener func(context.Context, Config) (T, error),
	migrator func(context.Context, T) error,
) (T, error) {
	var zero T

	pool, err := opener(ctx, cfg)
	if err != nil {
		return zero, err
	}
	if err := migrator(ctx, pool); err != nil {
		pool.Close()
		return zero, newOperationError("migrate", err)
	}
	return pool, nil
}

func openPool[T pingCloser](
	ctx context.Context,
	cfg Config,
	opener poolOpener[T],
) (T, error) {
	var zero T

	databaseURL := strings.TrimSpace(cfg.URL)
	if databaseURL == "" {
		return zero, ErrURLRequired
	}

	if cfg.StartupTimeout < 0 {
		return zero, ErrInvalidStartupTimeout
	}

	startupTimeout := cfg.StartupTimeout
	if startupTimeout == 0 {
		startupTimeout = DefaultStartupTimeout
	}

	startupCtx, cancel := context.WithTimeout(
		ctx,
		startupTimeout,
	)
	defer cancel()

	pool, err := opener(startupCtx, databaseURL)
	if err != nil {
		return zero, newOperationError("open", err)
	}

	if err := pool.Ping(startupCtx); err != nil {
		pool.Close()

		return zero, newOperationError("ping", err)
	}

	return pool, nil
}

type operationError struct {
	operation string
	cause     error
}

func newOperationError(
	operation string,
	cause error,
) error {
	return &operationError{
		operation: operation,
		cause:     cause,
	}
}

func (e *operationError) Error() string {
	return e.operation + " platform database"
}

func (e *operationError) Unwrap() error {
	return e.cause
}
