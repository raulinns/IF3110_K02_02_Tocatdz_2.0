package targetdb

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/explorer"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	mysqltarget "github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/mysql"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/schemadiff"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// Router memilih implementasi target berdasarkan engine yang tersimpan bersama koneksi.
type Router struct {
	platform   *pgxpool.Pool
	postgresql *postgres.Targets
	mysql      *mysqltarget.Targets
}

var (
	_ explorer.Reader           = (*Router)(nil)
	_ migration.Coordinator     = (*Router)(nil)
	_ migration.PlanCoordinator = (*Router)(nil)
	_ schemadiff.Reader         = (*Router)(nil)
	_ dbms.CapabilityResolver   = (*Router)(nil)
)

func NewRouter(platform *pgxpool.Pool, postgresql *postgres.Targets, mysql *mysqltarget.Targets) (*Router, error) {
	if platform == nil || postgresql == nil || mysql == nil {
		return nil, security.ErrInvalidInput
	}
	return &Router{platform: platform, postgresql: postgresql, mysql: mysql}, nil
}

func (r *Router) engine(ctx context.Context, id security.ID) (dbms.Engine, error) {
	if !security.ValidID(id) {
		return "", security.ErrNotFound
	}
	var raw string
	if err := r.platform.QueryRow(ctx, "SELECT engine FROM managed_databases WHERE id = $1", string(id)).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", security.ErrNotFound
		}
		return "", security.ErrUnavailable
	}
	engine, err := dbms.ParseEngine(raw)
	if err != nil {
		return "", security.ErrUnavailable
	}
	return engine, nil
}

func (r *Router) Check(ctx context.Context, engine dbms.Engine, dsn security.Secret) error {
	normalized, err := dbms.ParseEngine(string(engine))
	if err != nil {
		return security.ErrInvalidInput
	}
	if normalized == dbms.MySQL {
		return r.mysql.Check(ctx, dsn)
	}
	return r.postgresql.Check(ctx, dsn)
}

func (r *Router) Capabilities(ctx context.Context, id security.ID) (dbms.Capabilities, error) {
	engine, err := r.engine(ctx, id)
	if err != nil {
		return dbms.Capabilities{}, err
	}
	return dbms.Features(engine), nil
}

func (r *Router) Schemas(ctx context.Context, id security.ID) ([]explorer.Schema, error) {
	engine, err := r.engine(ctx, id)
	if err != nil {
		return nil, err
	}
	if engine == dbms.MySQL {
		return r.mysql.Schemas(ctx, id)
	}
	return r.postgresql.Schemas(ctx, id)
}

func (r *Router) Table(ctx context.Context, id security.ID, schema, table string) (explorer.Table, error) {
	engine, err := r.engine(ctx, id)
	if err != nil {
		return explorer.Table{}, err
	}
	if engine == dbms.MySQL {
		return r.mysql.Table(ctx, id, schema, table)
	}
	return r.postgresql.Table(ctx, id, schema, table)
}

func (r *Router) Rows(ctx context.Context, id security.ID, schema, table string, options explorer.RowReadOptions) (explorer.RowBatch, error) {
	engine, err := r.engine(ctx, id)
	if err != nil {
		return explorer.RowBatch{}, err
	}
	if engine == dbms.MySQL {
		return r.mysql.Rows(ctx, id, schema, table, options)
	}
	return r.postgresql.Rows(ctx, id, schema, table, options)
}

func (r *Router) WithDatabaseLock(ctx context.Context, id security.ID, run func(context.Context, migration.LockedTarget) error) error {
	engine, err := r.engine(ctx, id)
	if err != nil {
		return err
	}
	if engine == dbms.MySQL {
		return r.mysql.WithDatabaseLock(ctx, id, run)
	}
	return r.postgresql.WithDatabaseLock(ctx, id, run)
}

func (r *Router) WithPlanLock(ctx context.Context, id security.ID, run func(context.Context, migration.PlanTarget) error) error {
	engine, err := r.engine(ctx, id)
	if err != nil {
		return err
	}
	if engine == dbms.MySQL {
		return security.ErrUnavailable
	}
	return r.postgresql.WithPlanLock(ctx, id, run)
}

func (r *Router) Snapshot(ctx context.Context, id security.ID, schema string) (schemadiff.Snapshot, error) {
	engine, err := r.engine(ctx, id)
	if err != nil {
		return schemadiff.Snapshot{}, err
	}
	if engine == dbms.MySQL {
		return schemadiff.Snapshot{}, security.ErrUnavailable
	}
	return r.postgresql.Snapshot(ctx, id, schema)
}

func (r *Router) Close() {
	r.mysql.Close()
	r.postgresql.Close()
}
