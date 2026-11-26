package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var (
	_ migration.ProgressSink     = (*M2Store)(nil)
	_ migration.CheckpointReader = (*M2Store)(nil)
)

// BatchCommitted mencerminkan checkpoint target ke job_batches; idempoten per (execution, batch).
func (s *M2Store) BatchCommitted(ctx context.Context, checkpoint migration.BatchCheckpoint) error {
	if !security.ValidID(checkpoint.ExecutionID) || checkpoint.Batch < 1 || checkpoint.Rows < 0 {
		return security.ErrInvalidInput
	}
	at := checkpoint.CommittedAt
	if at.IsZero() {
		at = time.Now()
	}
	statement, err := checkpointStatement(checkpoint)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `WITH job AS (
			SELECT id FROM jobs WHERE execution_id = $1 AND status IN ('running', 'uncertain')
				AND ($9::uuid IS NULL OR id = $9) ORDER BY queued_at DESC, id DESC LIMIT 1
		), recorded AS (
			INSERT INTO batch_statements (job_id, batch_no, statement)
			SELECT id, $2, $8::jsonb FROM job WHERE $8::jsonb IS NOT NULL ON CONFLICT DO NOTHING
		), inserted AS (
			INSERT INTO job_batches (execution_id, batch_no, job_id, cursor_from, cursor_to, scope_max, rows, committed_at)
			SELECT $1, $2, job.id, $3, $4, $5, $6, $7 FROM job ON CONFLICT DO NOTHING RETURNING job_id, rows
		)
		UPDATE jobs j SET batches_committed = j.batches_committed + CASE WHEN inserted.rows > 0 THEN 1 ELSE 0 END,
			rows_committed = j.rows_committed + inserted.rows, scope_max = COALESCE(j.scope_max, $5)
		FROM inserted WHERE j.id = inserted.job_id`,
		string(checkpoint.ExecutionID), checkpoint.Batch, checkpoint.From, checkpoint.To, checkpoint.ScopeMax, checkpoint.Rows, micro(at), statement,
		nullID(logging.OperationFromContext(ctx).JobID))
	return mapError(err)
}

func (s *M2Store) BatchReverted(ctx context.Context, checkpoint migration.BatchCheckpoint) error {
	if !security.ValidID(checkpoint.ExecutionID) || checkpoint.Batch < 1 || checkpoint.Rows < 0 {
		return security.ErrInvalidInput
	}
	at := checkpoint.CommittedAt
	if at.IsZero() {
		at = time.Now()
	}
	statement, err := checkpointStatement(checkpoint)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `WITH job AS (
			SELECT r.id FROM job_batches b JOIN jobs o ON o.id = b.job_id
			JOIN jobs r ON r.database_id = o.database_id AND r.status IN ('running', 'uncertain')
			WHERE b.execution_id = $1 AND b.batch_no = $2 AND ($6::uuid IS NULL OR r.id = $6)
			ORDER BY r.queued_at DESC, r.id DESC LIMIT 1
		), recorded AS (
			INSERT INTO batch_statements (job_id, batch_no, statement)
			SELECT id, $2, $5::jsonb FROM job WHERE $5::jsonb IS NOT NULL ON CONFLICT DO NOTHING
		), updated AS (
			UPDATE job_batches b SET reverted_at = $3, reverted_by_job_id = job.id FROM job
			WHERE b.execution_id = $1 AND b.batch_no = $2 AND b.reverted_at IS NULL RETURNING job.id AS job_id
		)
		UPDATE jobs j SET batches_reverted = j.batches_reverted + 1, rows_reverted = j.rows_reverted + $4
		FROM updated WHERE j.id = updated.job_id`, string(checkpoint.ExecutionID), checkpoint.Batch, micro(at), checkpoint.Rows, statement,
		nullID(logging.OperationFromContext(ctx).JobID))
	return mapError(err)
}

func checkpointStatement(checkpoint migration.BatchCheckpoint) (any, error) {
	if checkpoint.Statement.Query.Empty() {
		return nil, nil // Legacy progress callers provided no statement.
	}
	statement := checkpoint.Statement
	statement.Batch = &checkpoint.Batch
	raw, err := encodeStatements([]migration.ExecutedStatement{statement})
	if err != nil {
		return nil, err
	}
	return raw[1 : len(raw)-1], nil
}

func (s *M2Store) Checkpoints(ctx context.Context, databaseID, executionID security.ID, query migration.CheckpointQuery) (migration.CheckpointPage, error) {
	if !security.ValidID(databaseID) || !security.ValidID(executionID) || query.AfterBatch < 0 || query.BeforeBatch < 0 {
		return migration.CheckpointPage{}, security.ErrInvalidInput
	}
	if query.Limit == 0 {
		query.Limit = defaultPageLimit
	}
	if query.Limit < 1 || query.Limit > maxPageLimit {
		return migration.CheckpointPage{}, security.ErrInvalidInput
	}
	rows, err := s.pool.Query(ctx, `SELECT b.batch_no, b.cursor_from, b.cursor_to, b.scope_max, b.rows, b.committed_at, b.reverted_at
		FROM job_batches b JOIN jobs j ON j.id = b.job_id
		WHERE j.database_id = $1 AND b.execution_id = $2 AND ($3 = 0 OR b.batch_no > $3) AND ($4 = 0 OR b.batch_no < $4)
		ORDER BY CASE WHEN $5 THEN -b.batch_no ELSE b.batch_no END LIMIT $6`,
		string(databaseID), string(executionID), query.AfterBatch, query.BeforeBatch, query.Descending, query.Limit+1)
	items, err := collect(rows, err, func(rows pgx.Rows) (migration.BatchCheckpoint, error) {
		checkpoint := migration.BatchCheckpoint{ExecutionID: executionID}
		if err := rows.Scan(&checkpoint.Batch, &checkpoint.From, &checkpoint.To, &checkpoint.ScopeMax, &checkpoint.Rows,
			&checkpoint.CommittedAt, &checkpoint.RevertedAt); err != nil {
			return checkpoint, mapError(err)
		}
		checkpoint.CommittedAt, checkpoint.RevertedAt = utc(checkpoint.CommittedAt), optionalTime(checkpoint.RevertedAt)
		return checkpoint, nil
	})
	if err != nil {
		return migration.CheckpointPage{}, err
	}
	page := migration.CheckpointPage{Items: items, HasNext: len(items) > query.Limit}
	if page.HasNext {
		page.Items = items[:query.Limit]
	}
	if page.Items == nil {
		page.Items = []migration.BatchCheckpoint{}
	}
	return page, nil
}

// JobStatusCounts menghitung job per status; status tanpa job tidak ikut dikembalikan.
func (s *M2Store) JobStatusCounts(ctx context.Context) (map[migration.JobStatus]int64, error) {
	rows, err := s.pool.Query(ctx, "SELECT status, count(*) FROM jobs GROUP BY status")
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	counts := map[migration.JobStatus]int64{}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, mapError(err)
		}
		counts[migration.JobStatus(status)] = count
	}
	return counts, mapError(rows.Err())
}
