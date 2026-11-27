package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var _ migration.AttemptHistoryStore = (*M2Store)(nil)
var _ migration.StatementRecorder = (*M2Store)(nil)

func resolveAttempt(ctx context.Context, tx pgx.Tx, lease migration.JobLease, status migration.JobStatus) error {
	_, err := tx.Exec(ctx, `INSERT INTO attempt_resolutions(history_id,status)
 SELECT history_id,$3 FROM job_attempts WHERE job_id=$1 AND attempt=$2
 AND status='uncertain' AND finished_at IS NOT NULL ON CONFLICT DO NOTHING`, string(lease.Job.ID), lease.Job.Attempt, string(status))
	return mapError(err)
}

func (s *M2Store) StartStatement(ctx context.Context, lease migration.JobLease, statement migration.ExecutedStatement) (int64, error) {
	if !validLease(lease) || statement.Query.Empty() {
		return 0, security.ErrInvalidInput
	}
	raw, err := encodeStatements([]migration.ExecutedStatement{statement})
	if err != nil {
		return 0, err
	}
	var id int64
	err = withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := fence(ctx, tx, lease, migration.JobRunning); err != nil {
			return err
		}
		return mapError(tx.QueryRow(ctx, `INSERT INTO attempt_statements(history_id,statement)
   SELECT history_id,$3::jsonb->0 FROM job_attempts WHERE job_id=$1 AND attempt=$2 AND finished_at IS NULL
   RETURNING id`, string(lease.Job.ID), lease.Job.Attempt, raw).Scan(&id))
	})
	return id, err
}

func (s *M2Store) FinishStatement(ctx context.Context, lease migration.JobLease, id int64, result error) error {
	status := "succeeded"
	var state any
	if result != nil {
		status = "failed"
		var sqlError interface{ SQLState() string }
		if errors.As(result, &sqlError) {
			code := sqlError.SQLState()
			valid := len(code) == 5
			for _, c := range code {
				valid = valid && (c >= '0' && c <= '9' || c >= 'A' && c <= 'Z')
			}
			if valid {
				state = code
			}
		}
	}
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if _, err := fence(ctx, tx, lease, migration.JobRunning); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO attempt_statement_results(statement_id,status,sqlstate)
   SELECT s.id,$4,$5 FROM attempt_statements s JOIN job_attempts a ON a.history_id=s.history_id
   WHERE s.id=$1 AND a.job_id=$2 AND a.attempt=$3 AND a.finished_at IS NULL
   ON CONFLICT DO NOTHING`, id, string(lease.Job.ID), lease.Job.Attempt, status, state)
		if err != nil {
			return mapError(err)
		}
		if tag.RowsAffected() != 1 {
			return security.ErrConflict
		}
		return nil
	})
}

// Successful attempts join their committed receipt exactly once. Failed retries
// keep their own status even when a later attempt shares the same execution ID.
const historyCTE = `WITH history AS (
 SELECT a.history_id AS id,a.database_id,a.started_at,
 jsonb_build_object('id',a.history_id,'database_id',a.database_id,'migration_id',a.migration_id,
 'change_request_id',a.change_request_id,'job_id',a.job_id,'execution_id',a.execution_id,
 'attempt',a.attempt,'revision',a.migration_revision,'operation',a.kind,'direction',a.direction,
 'status',a.status,'actor_id',a.actor_id,'actor',a.actor,'started_at',a.started_at,'finished_at',a.finished_at,
 'error_code',a.error_code,'error_message',a.error_message,'sqlstate',a.sqlstate,'retryable',a.retryable,
 'retry_of_job_id',c.retry_of_job_id,'checksum',COALESCE(encode(c.checksum,'hex'),''),
 'definition_snapshot',COALESCE(a.definition_snapshot,CASE WHEN e.id IS NOT NULL THEN jsonb_build_object('title',e.title,'up_sql',e.up_sql,'down_sql',e.down_sql,'irreversible',e.irreversible,'irreversible_reason',e.irreversible_reason,'mode',e.mode,'batch',e.batch_config) END),'resolved_status',COALESCE(r.status,a.status),
 'committed_at',e.committed_at,'position_version',e.position_version,
 'reverts_execution_id',e.reverts_execution_id,'forward_of_execution_id',e.forward_of_execution_id,
 'can_retry',COALESCE(r.status,a.status)='failed' AND j.status='failed' AND a.attempt=j.attempt AND m.mode='normal'
 AND j.execution_plan IS NOT NULL AND a.kind IN ('apply','rollback','forward')
 AND NOT EXISTS(SELECT 1 FROM executions done WHERE done.id=a.execution_id)
 AND NOT EXISTS(SELECT 1 FROM change_requests retry WHERE retry.retry_of_job_id=a.job_id AND retry.status<>'rejected'),
 'hidden_pair',e.id IS NOT NULL AND (e.reverts_execution_id IS NOT NULL OR
 EXISTS(SELECT 1 FROM executions down WHERE down.database_id=e.database_id AND down.reverts_execution_id=e.id))) AS record
 FROM job_attempts a JOIN jobs j ON j.id=a.job_id JOIN change_requests c ON c.id=a.change_request_id
 JOIN migrations m ON m.id=a.migration_id
 LEFT JOIN attempt_resolutions r ON r.history_id=a.history_id
 LEFT JOIN executions e ON e.id=a.execution_id AND e.job_id=a.job_id
 AND (a.status='succeeded' OR r.status='succeeded')
 WHERE a.database_id=$1
 UNION ALL
 SELECT e.id,e.database_id,e.started_at,
 (to_jsonb(e)-'statements')||jsonb_build_object('checksum',COALESCE(encode(e.checksum,'hex'),''),'execution_id',e.id,'revision',e.migration_revision,'attempt',0,
 'status','succeeded','resolved_status','succeeded','finished_at',e.committed_at,'can_retry',false,
 'definition_snapshot',jsonb_build_object('title',e.title,'up_sql',e.up_sql,'down_sql',e.down_sql,
 'irreversible',e.irreversible,'irreversible_reason',e.irreversible_reason,'mode',e.mode,'batch',e.batch_config),
 'hidden_pair',e.reverts_execution_id IS NOT NULL OR
 EXISTS(SELECT 1 FROM executions down WHERE down.database_id=e.database_id AND down.reverts_execution_id=e.id))
 FROM executions e WHERE e.database_id=$1 AND NOT EXISTS(SELECT 1 FROM job_attempts a WHERE a.job_id=e.job_id)
 ) `

func scanHistory(row pgx.Row) (migration.HistoryEntry, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return migration.HistoryEntry{}, mapError(err)
	}
	var v struct {
		SnapshotAvailable    bool                         `json:"snapshot_available"`
		Checksum             string                       `json:"checksum"`
		ActorID              security.ID                  `json:"actor_id"`
		ID                   security.ID                  `json:"id"`
		DatabaseID           security.ID                  `json:"database_id"`
		MigrationID          security.ID                  `json:"migration_id"`
		ChangeRequestID      security.ID                  `json:"change_request_id"`
		JobID                security.ID                  `json:"job_id"`
		ExecutionID          security.ID                  `json:"execution_id"`
		RetryOfJobID         security.ID                  `json:"retry_of_job_id"`
		Attempt              int                          `json:"attempt"`
		Revision             uint64                       `json:"revision"`
		Operation            migration.ChangeKind         `json:"operation"`
		Direction            migration.ExecutionDirection `json:"direction"`
		Status               migration.JobStatus          `json:"status"`
		ResolvedStatus       migration.JobStatus          `json:"resolved_status"`
		Actor                string                       `json:"actor"`
		StartedAt            time.Time                    `json:"started_at"`
		FinishedAt           *time.Time                   `json:"finished_at"`
		CommittedAt          *time.Time                   `json:"committed_at"`
		PositionVersion      *migration.PositionVersion   `json:"position_version"`
		CanRetry             bool                         `json:"can_retry"`
		RevertsExecutionID   security.ID                  `json:"reverts_execution_id"`
		ForwardOfExecutionID security.ID                  `json:"forward_of_execution_id"`
		ErrorCode            migration.JobErrorCode       `json:"error_code"`
		ErrorMessage         string                       `json:"error_message"`
		SQLState             string                       `json:"sqlstate"`
		Retryable            bool                         `json:"retryable"`
		Snapshot             *struct {
			Title        string                  `json:"title"`
			Up           string                  `json:"up_sql"`
			Down         *string                 `json:"down_sql"`
			Irreversible bool                    `json:"irreversible"`
			Reason       string                  `json:"irreversible_reason"`
			Mode         migration.ExecutionMode `json:"mode"`
			Batch        json.RawMessage         `json:"batch"`
		} `json:"definition_snapshot"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return migration.HistoryEntry{}, security.ErrUnavailable
	}
	e := migration.HistoryEntry{SnapshotAvailable: v.SnapshotAvailable || v.Snapshot != nil, Checksum: v.Checksum, ActorID: v.ActorID, ID: v.ID, DatabaseID: v.DatabaseID, MigrationID: v.MigrationID, ChangeRequestID: v.ChangeRequestID, JobID: v.JobID, ExecutionID: v.ExecutionID,
		RetryOfJobID: v.RetryOfJobID, Attempt: v.Attempt, Revision: v.Revision, Operation: v.Operation, Direction: v.Direction, Status: v.Status, ResolvedStatus: v.ResolvedStatus,
		Actor: v.Actor, StartedAt: v.StartedAt, FinishedAt: v.FinishedAt, CommittedAt: v.CommittedAt, PositionVersion: v.PositionVersion, CanRetry: v.CanRetry,
		RevertsExecutionID: v.RevertsExecutionID, ForwardOfExecutionID: v.ForwardOfExecutionID}
	if v.ErrorCode != "" {
		e.Error = &migration.JobFailure{Code: v.ErrorCode, Message: v.ErrorMessage, SQLState: v.SQLState, Retryable: v.Retryable}
	}
	if v.Snapshot != nil {
		b, err := decodeBatch(v.Snapshot.Batch)
		if err != nil {
			return e, err
		}
		d := migration.DefinitionInput{Title: v.Snapshot.Title, UpSQL: security.NewSecret(v.Snapshot.Up), Irreversible: v.Snapshot.Irreversible, IrreversibleReason: v.Snapshot.Reason, Mode: v.Snapshot.Mode, Batch: b}
		if v.Snapshot.Down != nil {
			d.DownSQL = security.NewSecret(*v.Snapshot.Down)
		}
		e.Definition = &d
	}
	return e, nil
}

func (s *M2Store) ListHistory(ctx context.Context, db security.ID, query migration.ExecutionQuery) (migration.Page[migration.HistoryEntry], error) {
	page, offset, err := pageWindow(query.PageQuery)
	if err != nil {
		return migration.Page[migration.HistoryEntry]{}, err
	}
	rows, err := s.pool.Query(ctx, historyCTE+`SELECT (record - 'definition_snapshot') || jsonb_build_object('snapshot_available',record->'definition_snapshot' IS NOT NULL AND record->'definition_snapshot' <> 'null'::jsonb) FROM history WHERE (NOT $2 OR NOT (record->>'hidden_pair')::boolean)
 ORDER BY started_at DESC,id DESC LIMIT $3 OFFSET $4`, string(db), query.HideRollback, page.Limit+1, offset)
	entries, err := collect(rows, err, func(r pgx.Rows) (migration.HistoryEntry, error) { return scanHistory(r) })
	if err != nil {
		return migration.Page[migration.HistoryEntry]{}, err
	}
	return finishPage(entries, page), nil
}

func (s *M2Store) FindHistory(ctx context.Context, db, id security.ID) (migration.HistoryEntry, error) {
	return scanHistory(s.pool.QueryRow(ctx, historyCTE+`SELECT record FROM history WHERE id=$2`, string(db), string(id)))
}

func (s *M2Store) HistoryStatements(ctx context.Context, db, id security.ID, query migration.PageQuery) (migration.Page[migration.HistoryStatement], error) {
	page, offset, err := pageWindow(query)
	if err != nil {
		return migration.Page[migration.HistoryStatement]{}, err
	}
	entry, err := s.FindHistory(ctx, db, id)
	if err != nil {
		return migration.Page[migration.HistoryStatement]{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT s.id,s.statement,COALESCE(r.status,'started'),COALESCE(r.sqlstate,''),s.started_at,r.finished_at
 FROM attempt_statements s LEFT JOIN attempt_statement_results r ON r.statement_id=s.id
 WHERE s.history_id=$1 ORDER BY s.id LIMIT $2 OFFSET $3`, string(id), page.Limit+1, offset)
	entries, err := collect(rows, err, func(r pgx.Rows) (migration.HistoryStatement, error) {
		var v migration.HistoryStatement
		var raw []byte
		if err := r.Scan(&v.Ordinal, &raw, &v.Status, &v.SQLState, &v.StartedAt, &v.FinishedAt); err != nil {
			return v, mapError(err)
		}
		statement, err := decodeStatement(raw)
		v.ExecutedStatement = statement
		return v, err
	})
	if err != nil {
		return migration.Page[migration.HistoryStatement]{}, err
	}
	// Old successful executions retain their historical statements; old failed
	// attempts without captured SQL remain explicitly unavailable, never invented.
	var captured bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM attempt_statements WHERE history_id=$1)", string(id)).Scan(&captured); err != nil {
		return migration.Page[migration.HistoryStatement]{}, mapError(err)
	}
	if !captured && entry.CommittedAt != nil {
		old, err := s.ListExecutedStatements(ctx, db, entry.ExecutionID, page)
		if err != nil {
			return migration.Page[migration.HistoryStatement]{}, err
		}
		for i, stmt := range old.Items {
			entries = append(entries, migration.HistoryStatement{ExecutedStatement: stmt, Ordinal: int64(offset + i + 1), Status: "succeeded", StartedAt: entry.StartedAt, FinishedAt: entry.FinishedAt})
		}
		result := finishPage(entries, page)
		result.HasNext = old.HasNext
		return result, nil
	}
	return finishPage(entries, page), nil
}
