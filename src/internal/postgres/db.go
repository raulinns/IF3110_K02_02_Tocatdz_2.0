package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

const (
	queryTimeout   = 5 * time.Second
	schemaLockKey  = 7215004401
	adminLockKey   = 7215004402
	connectTimeout = 5 * time.Second
)

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid DATABASE_URL")
	}
	cfg.MaxConns = 10
	cfg.ConnConfig.ConnectTimeout = connectTimeout
	cfg.ConnConfig.RuntimeParams["application_name"] = "tocatdz-platform"
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("cannot create platform database pool")
	}
	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, errors.New("platform database unavailable")
	}
	return pool, nil
}

// Migrate menerapkan file schema/*.sql berurutan; advisory lock mencegah dua instance menjalankannya bersamaan.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrate schema: %w", security.ErrUnavailable)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(schemaLockKey)); err != nil {
		return fmt.Errorf("migrate schema lock: %w", security.ErrUnavailable)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", int64(schemaLockKey))
	}()
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("migrate schema table: %w", security.ErrUnavailable)
	}
	names, err := fs.Glob(schemaFiles, "schema/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var exists bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)", name).Scan(&exists); err != nil {
			return fmt.Errorf("migrate schema check: %w", security.ErrUnavailable)
		}
		if exists {
			continue
		}
		body, err := schemaFiles.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("migrate schema begin: %w", security.ErrUnavailable)
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", strings.TrimPrefix(name, "schema/"), err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (name) VALUES ($1)", name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record %s: %w", name, security.ErrUnavailable)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", name, security.ErrUnavailable)
		}
	}
	return nil
}

// Error driver tidak pernah diteruskan apa adanya; hanya kategori aman dari paket security.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	for _, passthrough := range []error{context.Canceled, context.DeadlineExceeded, security.ErrNotFound, security.ErrConflict, security.ErrInvalidInput,
		security.ErrUnauthenticated, security.ErrForbidden, security.ErrUnavailable, security.ErrResultTooLarge} {
		if errors.Is(err, passthrough) {
			return passthrough
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return security.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", "40001", "40P01", "55P03":
			return security.ErrConflict
		case "23503", "23514", "22P02", "22001":
			return security.ErrInvalidInput
		}
	}
	return security.ErrUnavailable
}

func withTx(ctx context.Context, pool *pgxpool.Pool, opts pgx.TxOptions, run func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return mapError(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := run(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapError(err)
	}
	return nil
}

func utc(value time.Time) time.Time { return value.UTC() }

func ids(values []security.ID) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}
