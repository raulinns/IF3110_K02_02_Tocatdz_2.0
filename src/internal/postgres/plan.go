package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var _ migration.PlanCoordinator = (*Targets)(nil)

func (t *Targets) WithPlanLock(ctx context.Context, id security.ID, run func(context.Context, migration.PlanTarget) error) error {
	return t.WithDatabaseLock(ctx, id, func(ctx context.Context, target migration.LockedTarget) error {
		planner, ok := target.(migration.PlanTarget)
		if !ok {
			return security.ErrUnavailable
		}
		return run(ctx, planner)
	})
}

// Transaksi baca saja dan rollback juga membatasi efek query yang tersembunyi di view atau CTE.
func (l *lockedTarget) Explain(ctx context.Context, query migration.PlanQuery) ([]byte, error) {
	if query.SQL.Empty() || len(query.SQL.Reveal()) > migration.MaxSQLBytes {
		return nil, security.ErrInvalidInput
	}
	tx, err := l.conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, security.ErrUnavailable
	}
	closed := false
	defer func() {
		if !closed {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), migration.CleanupTimeout)
			defer cancel()
			_ = tx.Rollback(cleanup)
		}
	}()
	for _, sql := range []string{
		"SET LOCAL statement_timeout = '5s'",
		"SET LOCAL lock_timeout = '1s'",
		"SET LOCAL idle_in_transaction_session_timeout = '3s'",
		"SET LOCAL work_mem = '4MB'",
		"SET LOCAL search_path = pg_catalog, public",
	} {
		if _, err := tx.Exec(ctx, sql); err != nil {
			return nil, security.ErrUnavailable
		}
	}
	prefix := "EXPLAIN (FORMAT JSON, VERBOSE TRUE, COSTS TRUE, SUMMARY TRUE) "
	if query.Analyze {
		prefix = "EXPLAIN (ANALYZE TRUE, FORMAT JSON, VERBOSE TRUE, COSTS TRUE, SUMMARY TRUE) "
	}
	var raw []byte
	if err := tx.QueryRow(ctx, prefix+query.SQL.Reveal(), pgx.QueryExecModeExec).Scan(&raw); err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) {
			switch pgerr.Code {
			case "57014", "55P03":
				return nil, migration.PlanRejection("timeout")
			case "25006":
				return nil, migration.PlanRejection("analyze")
			case "42601", "42P01", "42703", "42883", "42804", "42501", "22012":
				return nil, migration.PlanRejection("query")
			}
		}
		return nil, security.ErrUnavailable
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), migration.CleanupTimeout)
	defer cancel()
	if err := tx.Rollback(cleanup); err != nil {
		return nil, security.ErrUnavailable
	}
	closed = true
	if len(raw) > migration.MaxPlanBytes {
		return nil, migration.PlanRejection("large")
	}
	return raw, nil
}
