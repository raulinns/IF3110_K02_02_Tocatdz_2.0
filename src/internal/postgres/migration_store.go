package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// MigrationStore adalah ledger migration di database platform; eksekusi SQL target ada di Targets.
type MigrationStore struct{ pool *pgxpool.Pool }

var (
	_ migration.Repository        = (*MigrationStore)(nil)
	_ migration.PendingRepository = (*MigrationStore)(nil)
)

func NewMigrationStore(pool *pgxpool.Pool) (*MigrationStore, error) {
	if pool == nil {
		return nil, security.ErrInvalidInput
	}
	return &MigrationStore{pool: pool}, nil
}

const summaryColumns = `id::text, database_id::text, title, status, COALESCE(version, 0), COALESCE(actor_id::text, ''), COALESCE(actor, ''),
	executed_at, updated_at`

func scanSummary(row pgx.Row, extra ...any) (migration.Summary, error) {
	var summary migration.Summary
	var id, database, status, actorID string
	var executed *time.Time
	targets := append([]any{&id, &database, &summary.Title, &status, &summary.Version, &actorID, &summary.Actor, &executed, &summary.UpdatedAt}, extra...)
	if err := row.Scan(targets...); err != nil {
		return migration.Summary{}, mapError(err)
	}
	summary.ID, summary.DatabaseID, summary.Status, summary.ActorID = security.ID(id), security.ID(database), migration.Status(status), security.ID(actorID)
	summary.UpdatedAt = utc(summary.UpdatedAt)
	if executed != nil {
		summary.ExecutedAt = utc(*executed)
	}
	return summary, nil
}

func (s *MigrationStore) History(ctx context.Context, database security.ID, opts migration.HistoryOptions) ([]migration.Summary, error) {
	if !security.ValidID(database) || opts.FetchLimit < 1 || opts.Offset < 0 {
		return nil, security.ErrInvalidInput
	}
	// Urutan sama dengan migration.historyBefore: draft dulu, lalu versi terbaru.
	rows, err := s.pool.Query(ctx, "SELECT "+summaryColumns+` FROM migrations WHERE database_id = $1
		ORDER BY (status = 'draft') DESC, COALESCE(version, 0) DESC, updated_at DESC, id LIMIT $2 OFFSET $3`, string(database), opts.FetchLimit, opts.Offset)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []migration.Summary
	for rows.Next() {
		summary, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	return result, mapError(rows.Err())
}

func findMigration(ctx context.Context, q querier, database, id security.ID, suffix string) (migration.Migration, error) {
	if !security.ValidID(database) || !security.ValidID(id) {
		return migration.Migration{}, security.ErrNotFound
	}
	var record migration.Migration
	var sql, execution, failure string
	var revision int64
	var checksum []byte
	summary, err := scanSummary(q.QueryRow(ctx, "SELECT "+summaryColumns+`, sql, revision, COALESCE(execution_id::text, ''), checksum, failure
		FROM migrations WHERE id = $1 AND database_id = $2`+suffix, string(id), string(database)), &sql, &revision, &execution, &checksum, &failure)
	if err != nil {
		return migration.Migration{}, err
	}
	record.Summary, record.SQL, record.Revision, record.ExecutionID, record.Failure = summary, security.NewSecret(sql), uint64(revision), security.ID(execution), migration.Failure(failure)
	if len(checksum) == 32 {
		record.Checksum = [32]byte(checksum)
	}
	return record, nil
}

func (s *MigrationStore) Find(ctx context.Context, database, id security.ID) (migration.Migration, error) {
	return findMigration(ctx, s.pool, database, id, "")
}

// Mutasi ledger memeriksa epoch akun dan session pelaksana di transaksi yang sama dengan perubahan data.
func checkActor(ctx context.Context, tx pgx.Tx, request migration.Request) error {
	var active, sessionLive bool
	var version int64
	err := tx.QueryRow(ctx, `SELECT u.active, u.auth_version, s.revoked_at IS NULL FROM users u JOIN sessions s ON s.user_id = u.id
		WHERE u.id = $1 AND s.id = $2 FOR SHARE OF u`, string(request.Principal.UserID), string(request.Principal.SessionID)).Scan(&active, &version, &sessionLive)
	if errors.Is(err, pgx.ErrNoRows) {
		return security.ErrUnauthenticated
	}
	if err != nil {
		return mapError(err)
	}
	if !active || !sessionLive || uint64(version) != request.Principal.AuthVersion {
		return security.ErrUnauthenticated
	}
	return nil
}

func (s *MigrationStore) Create(ctx context.Context, request migration.Request, record migration.Migration) error {
	if record.DatabaseID != request.DatabaseID || record.Status != migration.DraftStatus || record.Revision != 1 {
		return security.ErrInvalidInput
	}
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if err := checkActor(ctx, tx, request); err != nil {
			return err
		}
		at := micro(record.UpdatedAt)
		_, err := tx.Exec(ctx, `INSERT INTO migrations (id, database_id, title, sql, status, revision, created_by, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'draft', 1, $5, $6, $6)`, string(record.ID), string(record.DatabaseID), record.Title, record.SQL.Reveal(),
			string(request.Principal.UserID), at)
		return mapError(err)
	})
}

func (s *MigrationStore) Update(ctx context.Context, request migration.Request, id security.ID, revision uint64, draft migration.Draft, at time.Time) error {
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if err := checkActor(ctx, tx, request); err != nil {
			return err
		}
		current, err := findMigration(ctx, tx, request.DatabaseID, id, " FOR UPDATE")
		if err != nil {
			return err
		}
		if current.Status != migration.DraftStatus || current.Revision != revision {
			return security.ErrConflict
		}
		_, err = tx.Exec(ctx, "UPDATE migrations SET title = $2, sql = $3, updated_at = $4, revision = revision + 1 WHERE id = $1",
			string(id), draft.Title, draft.SQL.Reveal(), micro(at))
		return mapError(err)
	})
}

func (s *MigrationStore) Reserve(ctx context.Context, request migration.Request, claim migration.Claim) (migration.Migration, error) {
	var reserved migration.Migration
	err := withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if err := checkActor(ctx, tx, request); err != nil {
			return err
		}
		current, err := findMigration(ctx, tx, request.DatabaseID, claim.MigrationID, " FOR UPDATE")
		if err != nil {
			return err
		}
		if current.Status != migration.DraftStatus || current.Revision != claim.Revision || migration.Checksum(current.SQL) != claim.Checksum {
			return security.ErrConflict
		}
		var pending bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM migrations WHERE database_id = $1 AND status = 'pending')", string(request.DatabaseID)).Scan(&pending); err != nil {
			return mapError(err)
		}
		if pending {
			return migration.ErrExecutionUncertain
		}
		at := micro(claim.At)
		// Versi dialokasikan di bawah lock koordinator; constraint unik (database_id, version) menjadi pengaman terakhir.
		_, err = tx.Exec(ctx, `UPDATE migrations SET status = 'pending',
			version = (SELECT COALESCE(max(version), 0) + 1 FROM migrations WHERE database_id = $2),
			execution_id = $3, checksum = $4, actor_id = $5, actor = $6, executed_at = $7, updated_at = $7, revision = revision + 1
			WHERE id = $1`, string(claim.MigrationID), string(request.DatabaseID), string(claim.ExecutionID), claim.Checksum[:],
			string(request.Principal.UserID), claim.Actor, at)
		if err != nil {
			return mapError(err)
		}
		reserved, err = findMigration(ctx, tx, request.DatabaseID, claim.MigrationID, "")
		return err
	})
	return reserved, err
}

func (s *MigrationStore) Finish(ctx context.Context, key migration.ExecutionKey, status migration.Status, failure migration.Failure, at time.Time) error {
	if status != migration.Applied && status != migration.Failed {
		return security.ErrInvalidInput
	}
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		current, err := findMigration(ctx, tx, key.DatabaseID, key.MigrationID, " FOR UPDATE")
		if errors.Is(err, security.ErrNotFound) {
			return security.ErrConflict
		}
		if err != nil {
			return err
		}
		if current.ExecutionID != key.ExecutionID || current.Checksum != key.Checksum || current.Version != key.Version {
			return security.ErrConflict
		}
		if current.Status != migration.Pending {
			if current.Status == status && current.Failure == failure {
				return nil
			}
			return security.ErrConflict
		}
		_, err = tx.Exec(ctx, "UPDATE migrations SET status = $2, failure = $3, updated_at = $4, revision = revision + 1 WHERE id = $1",
			string(key.MigrationID), string(status), string(failure), micro(at))
		return mapError(err)
	})
}

func (s *MigrationStore) Pending(ctx context.Context, query migration.PendingQuery) ([]migration.ExecutionKey, error) {
	if query.Limit < 1 || query.Limit > 1000 || (query.After != "" && !security.ValidID(query.After)) {
		return nil, security.ErrInvalidInput
	}
	after := string(query.After)
	if after == "" {
		after = "00000000-0000-0000-0000-000000000000"
	}
	rows, err := s.pool.Query(ctx, `SELECT database_id::text, id::text, execution_id::text, version, checksum FROM migrations
		WHERE status = 'pending' AND id > $1::uuid ORDER BY id LIMIT $2`, after, query.Limit)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var result []migration.ExecutionKey
	for rows.Next() {
		var database, id, execution string
		var key migration.ExecutionKey
		var checksum []byte
		if err := rows.Scan(&database, &id, &execution, &key.Version, &checksum); err != nil {
			return nil, mapError(err)
		}
		if len(checksum) != 32 {
			return nil, security.ErrUnavailable
		}
		key.DatabaseID, key.MigrationID, key.ExecutionID, key.Checksum = security.ID(database), security.ID(id), security.ID(execution), [32]byte(checksum)
		result = append(result, key)
	}
	return result, mapError(rows.Err())
}
