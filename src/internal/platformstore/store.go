package platformstore

import (
	"context"
	"errors"
	"math"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	platformsqlc "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/platformdb/sqlc"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Store struct {
	pool    *pgxpool.Pool
	queries *platformsqlc.Queries
}

var (
	_ security.CredentialRepository = (*Store)(nil)
	_ security.SessionRepository    = (*Store)(nil)
	_ security.AccessRepository     = (*Store)(nil)
	_ explorer.Directory            = (*Store)(nil)
)

func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, security.ErrInvalidInput
	}
	return &Store{pool: pool, queries: platformsqlc.New(pool)}, nil
}

func repositoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, pgx.ErrNoRows):
		return security.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return security.ErrConflict
		case "23503":
			return security.ErrNotFound
		}
	}
	return security.ErrUnavailable
}

func checkedInt64(value uint64) (int64, error) {
	if value == 0 || value > math.MaxInt64 {
		return 0, security.ErrInvalidInput
	}
	return int64(value), nil
}

func checkedUint64(value int64) (uint64, error) {
	if value <= 0 {
		return 0, security.ErrUnavailable
	}
	return uint64(value), nil
}
