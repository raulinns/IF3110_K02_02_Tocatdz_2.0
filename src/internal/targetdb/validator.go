package targetdb

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type MigrationValidator struct {
	platform   *pgxpool.Pool
	postgresql *migration.PostgreSQLValidator
	mysql      *migration.MySQLValidator
}

var (
	_ migration.Validator         = (*MigrationValidator)(nil)
	_ migration.DatabaseValidator = (*MigrationValidator)(nil)
	_ migration.PlanValidator     = (*MigrationValidator)(nil)
)

func NewMigrationValidator(platform *pgxpool.Pool, postgresql *migration.PostgreSQLValidator, mysql *migration.MySQLValidator) (*MigrationValidator, error) {
	if platform == nil || postgresql == nil || mysql == nil {
		return nil, security.ErrInvalidInput
	}
	return &MigrationValidator{platform: platform, postgresql: postgresql, mysql: mysql}, nil
}

// Validate mempertahankan kontrak validator umum dan default aman PostgreSQL.
func (v *MigrationValidator) Validate(ctx context.Context, sql security.Secret) error {
	return v.postgresql.Validate(ctx, sql)
}

func (v *MigrationValidator) ValidateDatabase(ctx context.Context, id security.ID, sql security.Secret) error {
	if !security.ValidID(id) {
		return security.ErrInvalidInput
	}
	var raw string
	if err := v.platform.QueryRow(ctx, "SELECT engine FROM managed_databases WHERE id = $1", string(id)).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return security.ErrNotFound
		}
		return security.ErrUnavailable
	}
	engine, err := dbms.ParseEngine(raw)
	if err != nil {
		return security.ErrUnavailable
	}
	if engine == dbms.MySQL {
		return v.mysql.Validate(ctx, sql)
	}
	return v.postgresql.Validate(ctx, sql)
}

func (v *MigrationValidator) PlanStatement(ctx context.Context, sql security.Secret, index int, analyze bool) (security.Secret, error) {
	return v.postgresql.PlanStatement(ctx, sql, index, analyze)
}
