package dbms

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	DefaultStartupTimeout  = 5 * time.Second
	DefaultMaxOpenConns    = 10
	DefaultMaxIdleConns    = 5
	DefaultConnMaxLifetime = 30 * time.Minute
	DefaultConnMaxIdleTime = 5 * time.Minute
)

var (
	ErrDriverRequired        = errors.New("database driver is required")
	ErrDSNRequired           = errors.New("database DSN is required")
	ErrInvalidStartupTimeout = errors.New("database startup timeout must not be negative")
)

type SQLConfig struct {
	DSN            string
	StartupTimeout time.Duration
}

type SQLConnector interface {
	Adapter
	Open(context.Context, SQLConfig) (*sql.DB, error)
}

type sqlPingCloser interface {
	PingContext(context.Context) error
	Close() error
}

type sqlPoolOpener[T sqlPingCloser] func(driverName, dsn string) (T, error)

func OpenSQLPool(ctx context.Context, driverName string, cfg SQLConfig) (*sql.DB, error) {
	pool, err := openSQLPool(ctx, driverName, cfg, sql.Open)
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(DefaultMaxOpenConns)
	pool.SetMaxIdleConns(DefaultMaxIdleConns)
	pool.SetConnMaxLifetime(DefaultConnMaxLifetime)
	pool.SetConnMaxIdleTime(DefaultConnMaxIdleTime)
	return pool, nil
}

func openSQLPool[T sqlPingCloser](
	ctx context.Context,
	driverName string,
	cfg SQLConfig,
	opener sqlPoolOpener[T],
) (T, error) {
	var zero T

	driverName = strings.TrimSpace(driverName)
	if driverName == "" {
		return zero, ErrDriverRequired
	}
	dsn := strings.TrimSpace(cfg.DSN)
	if dsn == "" {
		return zero, ErrDSNRequired
	}
	if cfg.StartupTimeout < 0 {
		return zero, ErrInvalidStartupTimeout
	}

	timeout := cfg.StartupTimeout
	if timeout == 0 {
		timeout = DefaultStartupTimeout
	}

	pool, err := opener(driverName, dsn)
	if err != nil {
		return zero, newSQLOperationError("open", err)
	}

	startupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := pool.PingContext(startupCtx); err != nil {
		_ = pool.Close()
		return zero, newSQLOperationError("ping", err)
	}

	return pool, nil
}

type sqlOperationError struct {
	operation string
	cause     error
}

func newSQLOperationError(operation string, cause error) error {
	return &sqlOperationError{operation: operation, cause: cause}
}

func (e *sqlOperationError) Error() string {
	return e.operation + " target database"
}

func (e *sqlOperationError) Unwrap() error {
	return e.cause
}
