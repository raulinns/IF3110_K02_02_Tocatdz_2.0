package platformdb

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLockID int64 = 7241511001

//go:embed migrations/*.up.sql
var embeddedMigrations embed.FS

type migration struct {
	Version  int64
	Name     string
	SQL      string
	Checksum string
}

func loadMigrations(source fs.FS) ([]migration, error) {
	names, err := fs.Glob(source, "migrations/*.up.sql")
	if err != nil {
		return nil, fmt.Errorf("list platform migrations: %w", err)
	}

	result := make([]migration, 0, len(names))
	versions := make(map[int64]struct{}, len(names))
	for _, name := range names {
		base := path.Base(name)
		separator := strings.IndexByte(base, '_')
		if separator <= 0 {
			return nil, fmt.Errorf("invalid platform migration filename %q", base)
		}
		version, err := strconv.ParseInt(base[:separator], 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid platform migration version in %q", base)
		}
		if _, duplicate := versions[version]; duplicate {
			return nil, fmt.Errorf("duplicate platform migration version %d", version)
		}
		versions[version] = struct{}{}

		contents, err := fs.ReadFile(source, name)
		if err != nil {
			return nil, fmt.Errorf("read platform migration %q: %w", base, err)
		}
		digest := sha256.Sum256(contents)
		result = append(result, migration{
			Version:  version,
			Name:     base,
			SQL:      string(contents),
			Checksum: hex.EncodeToString(digest[:]),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Version < result[j].Version
	})
	return result, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := loadMigrations(embeddedMigrations)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS platform_schema_migrations (
    version BIGINT PRIMARY KEY,
    name TEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)
	if err != nil {
		return fmt.Errorf("create platform migration ledger: %w", err)
	}

	for _, item := range migrations {
		if err := applyMigration(ctx, pool, item); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, item migration) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin platform migration %d: %w", item.Version, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("lock platform migrations: %w", err)
	}

	var checksum string
	err = tx.QueryRow(
		ctx,
		"SELECT checksum FROM platform_schema_migrations WHERE version = $1",
		item.Version,
	).Scan(&checksum)
	switch {
	case err == nil:
		if checksum != item.Checksum {
			return fmt.Errorf("platform migration %d checksum mismatch", item.Version)
		}
		return tx.Commit(ctx)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read platform migration %d: %w", item.Version, err)
	}

	if _, err := tx.Exec(ctx, item.SQL); err != nil {
		return fmt.Errorf("apply platform migration %d: %w", item.Version, err)
	}
	if _, err := tx.Exec(
		ctx,
		"INSERT INTO platform_schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
		item.Version,
		item.Name,
		item.Checksum,
	); err != nil {
		return fmt.Errorf("record platform migration %d: %w", item.Version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit platform migration %d: %w", item.Version, err)
	}
	return nil
}
