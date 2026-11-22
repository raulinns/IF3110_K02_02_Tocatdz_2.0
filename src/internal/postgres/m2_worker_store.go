package postgres

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

var (
	_ migration.WorkerStore         = (*M2Store)(nil)
	_ migration.ReconciliationStore = (*M2Store)(nil)
	_ migration.RevalidationReader  = (*M2Store)(nil)
)

// Urutan lock: guard database, job, lalu CR (sama dengan WorkflowStore) agar tidak deadlock.

const maxOwnerLength = 200

// Rencana eksekusi disimpan tanpa SQL; definisi beku dimuat ulang lalu dicek revisi dan checksum-nya.

type planJSON struct {
	JobID              string  `json:"job_id"`
	ChangeRequestID    string  `json:"change_request_id"`
	DatabaseID         string  `json:"database_id"`
	MigrationID        string  `json:"migration_id"`
	MigrationRevision  uint64  `json:"migration_revision"`
	Checksum           string  `json:"checksum"`
	ReceiptExecutionID string  `json:"receipt_execution_id"`
	ActorID            string  `json:"actor_id"`
	Kind               string  `json:"kind"`
	Direction          string  `json:"direction"`
	ExpectedPosition   uint64  `json:"expected_position"`
	TargetExecutionID  string  `json:"target_execution_id,omitempty"`
	SourceJobID        string  `json:"source_job_id,omitempty"`
	SourceExecutionID  string  `json:"source_execution_id,omitempty"`
	ScopeMax           *string `json:"scope_max,omitempty"`
}

func encodePlan(plan migration.ExecutionPlan) (string, error) {
	raw, err := json.Marshal(planJSON{
		JobID: string(plan.JobID), ChangeRequestID: string(plan.ChangeRequestID), DatabaseID: string(plan.DatabaseID),
		MigrationID: string(plan.MigrationID), MigrationRevision: plan.Definition.Revision, Checksum: hex.EncodeToString(plan.Definition.Checksum[:]),
		ReceiptExecutionID: string(plan.ReceiptExecutionID), ActorID: string(plan.ActorID), Kind: string(plan.Kind),
		Direction: string(plan.Direction), ExpectedPosition: uint64(plan.ExpectedPosition), TargetExecutionID: string(plan.TargetExecutionID),
		SourceJobID: string(plan.SourceJobID), SourceExecutionID: string(plan.SourceExecutionID), ScopeMax: plan.ScopeMax,
	})
	if err != nil {
		return "", security.ErrInvalidInput
	}
	return string(raw), nil
}

func decodePlan(ctx context.Context, q querier, raw []byte) (*migration.ExecutionPlan, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value planJSON
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, security.ErrUnavailable
	}
	definition, err := findDefinition(ctx, q, security.ID(value.DatabaseID), security.ID(value.MigrationID), "")
	if err != nil {
		return nil, err
	}
	if definition.Revision != value.MigrationRevision || hex.EncodeToString(definition.Checksum[:]) != value.Checksum {
		return nil, security.ErrUnavailable
	}
	return &migration.ExecutionPlan{
		JobID: security.ID(value.JobID), ChangeRequestID: security.ID(value.ChangeRequestID), DatabaseID: security.ID(value.DatabaseID),
		MigrationID: security.ID(value.MigrationID), ReceiptExecutionID: security.ID(value.ReceiptExecutionID), ActorID: security.ID(value.ActorID),
		Kind: migration.ChangeKind(value.Kind), Direction: migration.ExecutionDirection(value.Direction), Definition: definition,
		ExpectedPosition: migration.PositionVersion(value.ExpectedPosition), TargetExecutionID: security.ID(value.TargetExecutionID),
		SourceJobID: security.ID(value.SourceJobID), SourceExecutionID: security.ID(value.SourceExecutionID), ScopeMax: value.ScopeMax,
	}, nil
}

type guardRow struct {
	version int64
	blocked string
}

func lockWorkflowGuard(ctx context.Context, tx pgx.Tx, databaseID security.ID) (guardRow, error) {
	var guard guardRow
	err := tx.QueryRow(ctx, `SELECT position_version, COALESCE(blocked_by_job_id::text, '') FROM database_workflows
		WHERE database_id = $1 FOR UPDATE`, string(databaseID)).Scan(&guard.version, &guard.blocked)
	return guard, mapError(err)
}

type fencedJob struct {
	status, changeRequest, execution, source string
	attempt, maxAttempts                     int
}

// fence mengunci job hanya jika lease token masih milik pemanggil.
func fence(ctx context.Context, tx pgx.Tx, lease migration.JobLease, statuses ...migration.JobStatus) (fencedJob, error) {
	var job fencedJob
	err := tx.QueryRow(ctx, `SELECT status, change_request_id::text, execution_id::text, COALESCE(source_job_id::text, ''), attempt, max_attempts
		FROM jobs WHERE id = $1 AND database_id = $2 AND lease_owner = $3 AND lease_token = $4 FOR UPDATE`,
		string(lease.Job.ID), string(lease.Job.DatabaseID), lease.Owner, int64(lease.Token)).
		Scan(&job.status, &job.changeRequest, &job.execution, &job.source, &job.attempt, &job.maxAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, security.ErrConflict
	}
	if err != nil {
		return job, mapError(err)
	}
	for _, status := range statuses {
		if job.status == string(status) {
			return job, nil
		}
	}
	return job, security.ErrConflict
}

func validLease(lease migration.JobLease) bool {
	return security.ValidID(lease.Job.ID) && security.ValidID(lease.Job.DatabaseID) && lease.Owner != "" &&
		len(lease.Owner) <= maxOwnerLength && lease.Token > 0
}

func failureArgs(failure *migration.JobFailure) (code, message, sqlstate any, retryable bool) {
	if failure == nil {
		return nil, "", nil, false
	}
	if failure.SQLState != "" {
		sqlstate = failure.SQLState
	}
	message = failure.Message
	if len(failure.Message) > 1000 {
		message = failure.Message[:1000]
	}
	return string(failure.Code), message, sqlstate, failure.Retryable
}

// ClaimJob mengambil satu job dengan SKIP LOCKED dan melewati database yang sibuk atau tertahan barrier.
// ErrNotFound berarti tidak ada job.
func (s *M2Store) ClaimJob(ctx context.Context, owner string, now time.Time, leaseDuration time.Duration) (migration.JobLease, error) {
	if owner == "" || len(owner) > maxOwnerLength || leaseDuration <= 0 || now.IsZero() {
		return migration.JobLease{}, security.ErrInvalidInput
	}
	var lease migration.JobLease
	err := withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var id, database string
		var token int64
		var until time.Time
		err := tx.QueryRow(ctx, `WITH candidate AS (
				SELECT j.id FROM jobs j JOIN database_workflows w ON w.database_id = j.database_id
				WHERE j.status = 'queued' AND j.run_after <= $2 AND j.attempt < j.max_attempts
					AND NOT EXISTS (SELECT 1 FROM jobs r WHERE r.database_id = j.database_id AND r.status IN ('running', 'uncertain'))
					AND (w.blocked_by_job_id IS NULL OR j.source_job_id = w.blocked_by_job_id)
				ORDER BY j.run_after, j.queued_at, j.id
				LIMIT 1 FOR UPDATE OF j SKIP LOCKED
			)
			UPDATE jobs j SET status = 'running', attempt = j.attempt + 1, lease_owner = $1, lease_token = j.lease_token + 1,
				lease_until = $3, started_at = COALESCE(j.started_at, $2), finished_at = NULL
			FROM candidate WHERE j.id = candidate.id
			RETURNING j.id::text, j.database_id::text, j.lease_token, j.lease_until`, owner, micro(now), micro(now.Add(leaseDuration))).
			Scan(&id, &database, &token, &until)
		if err != nil {
			return mapError(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO job_attempts (job_id, attempt, database_id, change_request_id, migration_id, migration_revision,
				execution_id, kind, actor_id, actor, worker, lease_token, status, started_at)
			SELECT j.id, j.attempt, j.database_id, j.change_request_id, c.migration_id, c.migration_revision, j.execution_id, c.kind,
				j.requested_by, u.username, $2, j.lease_token, 'running', $3
			FROM jobs j JOIN change_requests c ON c.id = j.change_request_id JOIN users u ON u.id = j.requested_by WHERE j.id = $1`,
			id, owner, micro(now)); err != nil {
			return mapError(err)
		}
		job, err := findJob(ctx, tx, security.ID(database), security.ID(id), "")
		if err != nil {
			return err
		}
		lease = migration.JobLease{Job: job, Owner: owner, Token: uint64(token), LeaseUntil: utc(until)}
		return nil
	})
	return lease, err
}

func (s *M2Store) RenewLease(ctx context.Context, lease migration.JobLease, until time.Time) (migration.JobLease, error) {
	if !validLease(lease) || until.IsZero() {
		return migration.JobLease{}, security.ErrInvalidInput
	}
	tag, err := s.pool.Exec(ctx, `UPDATE jobs SET lease_until = $5 WHERE id = $1 AND database_id = $2 AND lease_owner = $3 AND lease_token = $4
		AND status IN ('running', 'uncertain')`, string(lease.Job.ID), string(lease.Job.DatabaseID), lease.Owner, int64(lease.Token), micro(until))
	if err != nil {
		return migration.JobLease{}, mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return migration.JobLease{}, security.ErrConflict
	}
	lease.LeaseUntil = micro(until)
	return lease, nil
}

// StartRunning mengecek ulang CR, approval, definisi, dan posisi sebelum SQL dikirim ke target.
func (s *M2Store) StartRunning(ctx context.Context, lease migration.JobLease, plan migration.ExecutionPlan) error {
	if !validLease(lease) || plan.JobID != lease.Job.ID || plan.DatabaseID != lease.Job.DatabaseID ||
		plan.ChangeRequestID != lease.Job.ChangeRequestID || plan.MigrationID != plan.Definition.ID ||
		(plan.Direction != migration.DirectionUp && plan.Direction != migration.DirectionDown) {
		return security.ErrInvalidInput
	}
	encoded, err := encodePlan(plan)
	if err != nil {
		return err
	}
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		guard, err := lockWorkflowGuard(ctx, tx, plan.DatabaseID)
		if err != nil {
			return err
		}
		job, err := fence(ctx, tx, lease, migration.JobRunning)
		if err != nil {
			return err
		}
		if job.execution != string(plan.ReceiptExecutionID) {
			return security.ErrConflict
		}
		request, err := findChangeRequest(ctx, tx, plan.DatabaseID, plan.ChangeRequestID, " FOR UPDATE")
		if err != nil {
			return err
		}
		definition, err := findDefinition(ctx, tx, plan.DatabaseID, plan.MigrationID, "")
		if err != nil {
			return err
		}
		if request.Status != migration.CRQueued || !allApproved(request) || request.MigrationID != plan.MigrationID ||
			request.Kind != plan.Kind || request.Checksum != plan.Definition.Checksum || request.MigrationRevision != plan.Definition.Revision ||
			definition.Checksum != plan.Definition.Checksum || definition.Revision != plan.Definition.Revision ||
			definition.State != migration.DefinitionFrozen || migration.PositionVersion(guard.version) != plan.ExpectedPosition ||
			request.ExpectedPosition != plan.ExpectedPosition || (guard.blocked != "" && guard.blocked != job.source) {
			return security.ErrConflict
		}
		if _, err := tx.Exec(ctx, "UPDATE jobs SET execution_plan = $2, scope_max = COALESCE(scope_max, $3) WHERE id = $1",
			string(plan.JobID), encoded, plan.ScopeMax); err != nil {
			return mapError(err)
		}
		if _, err := tx.Exec(ctx, "UPDATE change_requests SET status = 'running', revision = revision + 1, updated_at = now() WHERE id = $1",
			string(plan.ChangeRequestID)); err != nil {
			return mapError(err)
		}
		if _, err := tx.Exec(ctx, "UPDATE migrations SET first_execution_at = now() WHERE id = $1 AND first_execution_at IS NULL",
			string(plan.MigrationID)); err != nil {
			return mapError(err)
		}
		_, err = tx.Exec(ctx, "UPDATE job_attempts SET direction = $2 WHERE job_id = $1 AND finished_at IS NULL", string(plan.JobID), string(plan.Direction))
		return mapError(err)
	})
}

// CompleteCommitted mencatat history, posisi, job, CR, dan attempt dalam satu transaksi; replay idempoten.
func (s *M2Store) CompleteCommitted(ctx context.Context, lease migration.JobLease, execution migration.Execution) error {
	if !validLease(lease) || execution.JobID != lease.Job.ID || execution.DatabaseID != lease.Job.DatabaseID ||
		execution.ChangeRequestID != lease.Job.ChangeRequestID || !security.ValidID(execution.ID) || !security.ValidID(execution.ActorID) ||
		execution.CommittedAt.IsZero() || execution.StartedAt.IsZero() {
		return security.ErrInvalidInput
	}
	statements, err := encodeStatements(execution.Statements)
	if err != nil {
		return err
	}
	batch, err := encodeBatch(execution.Definition.Batch)
	if err != nil {
		return err
	}
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		guard, err := lockWorkflowGuard(ctx, tx, execution.DatabaseID)
		if err != nil {
			return err
		}
		var existingJob string
		err = tx.QueryRow(ctx, "SELECT COALESCE(job_id::text, '') FROM executions WHERE id = $1", string(execution.ID)).Scan(&existingJob)
		if err == nil {
			if existingJob == string(execution.JobID) {
				return nil
			}
			return security.ErrConflict
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return mapError(err)
		}
		job, err := fence(ctx, tx, lease, migration.JobRunning, migration.JobUncertain)
		if err != nil {
			return err
		}
		request, err := findChangeRequest(ctx, tx, execution.DatabaseID, execution.ChangeRequestID, " FOR UPDATE")
		if err != nil {
			return err
		}
		if job.execution != string(execution.ID) || request.MigrationID != execution.MigrationID || request.Kind != execution.Operation ||
			request.MigrationRevision != execution.MigrationRevision || request.Checksum != execution.Checksum {
			return security.ErrConflict
		}
		version := guard.version + 1
		at := micro(execution.CommittedAt)
		tag, err := tx.Exec(ctx, `INSERT INTO executions (id, database_id, migration_id, change_request_id, job_id, migration_revision, checksum,
				operation, direction, reverts_execution_id, forward_of_execution_id, title, up_sql, down_sql, irreversible, irreversible_reason,
				mode, batch_config, statements, actor_id, actor, started_at, committed_at, position_version)
			SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, u.id, u.username, $21, $22, $23
			FROM users u WHERE u.id = $20`,
			string(execution.ID), string(execution.DatabaseID), string(execution.MigrationID), string(execution.ChangeRequestID),
			string(execution.JobID), int64(execution.MigrationRevision), execution.Checksum[:], string(execution.Operation),
			string(execution.Direction), nullID(execution.RevertsExecutionID), nullID(execution.ForwardOfExecutionID),
			execution.Definition.Title, execution.Definition.UpSQL.Reveal(), downArg(execution.Definition),
			execution.Definition.Irreversible, execution.Definition.IrreversibleReason, string(execution.Definition.Mode), batch,
			statements, string(execution.ActorID), micro(execution.StartedAt), at, version)
		if err != nil {
			return mapError(err)
		}
		if tag.RowsAffected() != 1 {
			return security.ErrInvalidInput
		}
		if _, err := tx.Exec(ctx, `UPDATE database_workflows SET position_version = $2, updated_at = now(),
			blocked_by_job_id = CASE WHEN blocked_by_job_id IN ($3::uuid, $4::uuid) THEN NULL ELSE blocked_by_job_id END
			WHERE database_id = $1`, string(execution.DatabaseID), version, string(execution.JobID), nullID(security.ID(job.source))); err != nil {
			return mapError(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET status = 'succeeded', finished_at = $2, lease_owner = NULL, lease_until = NULL, partial = false
			WHERE id = $1`, string(execution.JobID), at); err != nil {
			return mapError(err)
		}
		if _, err := tx.Exec(ctx, "UPDATE change_requests SET status = 'succeeded', revision = revision + 1, updated_at = $2 WHERE id = $1",
			string(execution.ChangeRequestID), at); err != nil {
			return mapError(err)
		}
		_, err = tx.Exec(ctx, `UPDATE job_attempts SET status = 'succeeded', finished_at = $2, statements = COALESCE(statements, $3::jsonb)
			WHERE job_id = $1 AND finished_at IS NULL`, string(execution.JobID), at, statements)
		return mapError(err)
	})
}

// Partial, partially_reverted, dan uncertain memasang barrier pada database.
func (s *M2Store) RecordOutcome(ctx context.Context, lease migration.JobLease, result migration.RunResult) error {
	switch result.Status {
	case migration.JobFailed, migration.JobPartial, migration.JobUncertain, migration.JobPartiallyReverted:
	default:
		return security.ErrInvalidInput
	}
	if !validLease(lease) || result.Execution != nil {
		return security.ErrInvalidInput
	}
	failure := result.Error
	if failure == nil && result.Status == migration.JobFailed {
		failure = &migration.JobFailure{Code: migration.JobErrorInterrupted, Message: "execution interrupted"}
	}
	code, message, sqlstate, retryable := failureArgs(failure)
	barrier := result.Status != migration.JobFailed
	partial := result.Status == migration.JobPartial || result.Status == migration.JobPartiallyReverted ||
		(result.Status == migration.JobUncertain && (result.Progress.BatchesCommitted > 0 || result.Progress.BatchesReverted > 0))
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		guard, err := lockWorkflowGuard(ctx, tx, lease.Job.DatabaseID)
		if err != nil {
			return err
		}
		job, err := fence(ctx, tx, lease, migration.JobRunning, migration.JobUncertain)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET status = $2, finished_at = CASE WHEN $2 = 'uncertain' THEN NULL ELSE now() END,
				lease_owner = NULL, lease_until = NULL, partial = $3, batches_committed = $4, rows_committed = $5, batches_reverted = $6,
				rows_reverted = $7, total_batches = COALESCE($8, total_batches), last_error_code = $9, last_error_message = $10,
				last_sqlstate = $11, last_error_retryable = $12
			WHERE id = $1`, string(lease.Job.ID), string(result.Status), partial, result.Progress.BatchesCommitted, result.Progress.RowsCommitted,
			result.Progress.BatchesReverted, result.Progress.RowsReverted, result.Progress.TotalBatches, code, message, sqlstate, retryable); err != nil {
			return mapError(err)
		}
		if _, err := tx.Exec(ctx, "UPDATE change_requests SET status = $2, revision = revision + 1, updated_at = now() WHERE id = $1",
			job.changeRequest, string(result.Status)); err != nil {
			return mapError(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE job_attempts SET status = $2, error_code = $3, error_message = $4, sqlstate = $5, retryable = $6,
				finished_at = now() WHERE job_id = $1 AND finished_at IS NULL`,
			string(lease.Job.ID), string(result.Status), code, message, sqlstate, retryable); err != nil {
			return mapError(err)
		}
		switch {
		case barrier:
			_, err = tx.Exec(ctx, `UPDATE database_workflows SET blocked_by_job_id = $2, position_version = position_version + 1, updated_at = now()
				WHERE database_id = $1`, string(lease.Job.DatabaseID), string(lease.Job.ID))
		case guard.blocked == string(lease.Job.ID):
			_, err = tx.Exec(ctx, `UPDATE database_workflows SET blocked_by_job_id = NULL, position_version = position_version + 1, updated_at = now()
				WHERE database_id = $1`, string(lease.Job.DatabaseID))
		}
		return mapError(err)
	})
}

func (s *M2Store) ScheduleRetry(ctx context.Context, lease migration.JobLease, failure migration.JobFailure, runAfter time.Time) error {
	if !validLease(lease) || runAfter.IsZero() || failure.Code == "" {
		return security.ErrInvalidInput
	}
	code, message, sqlstate, _ := failureArgs(&failure)
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		guard, err := lockWorkflowGuard(ctx, tx, lease.Job.DatabaseID)
		if err != nil {
			return err
		}
		job, err := fence(ctx, tx, lease, migration.JobRunning, migration.JobUncertain)
		if err != nil {
			return err
		}
		if job.attempt >= job.maxAttempts {
			return security.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE jobs SET status = 'queued', run_after = $2, lease_owner = NULL, lease_until = NULL, finished_at = NULL,
				last_error_code = $3, last_error_message = $4, last_sqlstate = $5, last_error_retryable = true WHERE id = $1`,
			string(lease.Job.ID), micro(runAfter), code, message, sqlstate); err != nil {
			return mapError(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE change_requests SET status = 'queued', revision = revision + 1, updated_at = now()
			WHERE id = $1 AND status IN ('running', 'uncertain')`, job.changeRequest); err != nil {
			return mapError(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE job_attempts SET status = 'failed', error_code = $2, error_message = $3, sqlstate = $4, retryable = true,
				finished_at = now() WHERE job_id = $1 AND finished_at IS NULL`, string(lease.Job.ID), code, message, sqlstate); err != nil {
			return mapError(err)
		}
		if guard.blocked == string(lease.Job.ID) {
			_, err = tx.Exec(ctx, `UPDATE database_workflows SET blocked_by_job_id = NULL, position_version = position_version + 1, updated_at = now()
				WHERE database_id = $1`, string(lease.Job.DatabaseID))
		}
		return mapError(err)
	})
}

const recoverableCondition = `((j.status = 'running' AND j.lease_until < now()) OR
	(j.status = 'uncertain' AND (j.lease_until IS NULL OR j.lease_until < now())))`

// ListRecoverable: lease kedaluwarsa (worker mati) dan job uncertain.
func (s *M2Store) ListRecoverable(ctx context.Context, query migration.PageQuery) (migration.Page[migration.JobView], error) {
	page, offset, err := pageWindow(query)
	if err != nil {
		return migration.Page[migration.JobView]{}, err
	}
	rows, err := s.pool.Query(ctx, "SELECT "+jobColumns+jobFrom+" WHERE "+recoverableCondition+
		" ORDER BY j.queued_at, j.id LIMIT $1 OFFSET $2", page.Limit+1, offset)
	items, err := collect(rows, err, func(rows pgx.Rows) (migration.JobView, error) { return scanJob(rows) })
	if err != nil {
		return migration.Page[migration.JobView]{}, err
	}
	return finishPage(items, page), nil
}

func (s *M2Store) ClaimRecovery(ctx context.Context, databaseID, jobID security.ID, owner string, leaseDuration time.Duration) (migration.JobLease, error) {
	if !security.ValidID(databaseID) || !security.ValidID(jobID) || owner == "" || len(owner) > maxOwnerLength || leaseDuration <= 0 {
		return migration.JobLease{}, security.ErrInvalidInput
	}
	var lease migration.JobLease
	err := withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var token int64
		var until time.Time
		err := tx.QueryRow(ctx, `UPDATE jobs j SET lease_owner = $3, lease_token = j.lease_token + 1,
				lease_until = now() + make_interval(secs => $4)
			WHERE j.id = $1 AND j.database_id = $2 AND `+recoverableCondition+` RETURNING j.lease_token, j.lease_until`,
			string(jobID), string(databaseID), owner, leaseDuration.Seconds()).Scan(&token, &until)
		if errors.Is(err, pgx.ErrNoRows) {
			return security.ErrConflict
		}
		if err != nil {
			return mapError(err)
		}
		job, err := findJob(ctx, tx, databaseID, jobID, "")
		if err != nil {
			return err
		}
		lease = migration.JobLease{Job: job, Owner: owner, Token: uint64(token), LeaseUntil: utc(until)}
		return nil
	})
	return lease, err
}

// RecoveryPlan bernilai nil jika SQL belum pernah dikirim ke target.
func (s *M2Store) RecoveryPlan(ctx context.Context, databaseID, jobID security.ID) (*migration.ExecutionPlan, error) {
	if !security.ValidID(databaseID) || !security.ValidID(jobID) {
		return nil, security.ErrNotFound
	}
	var raw []byte
	if err := s.pool.QueryRow(ctx, "SELECT execution_plan FROM jobs WHERE id = $1 AND database_id = $2", string(jobID), string(databaseID)).Scan(&raw); err != nil {
		return nil, mapError(err)
	}
	return decodePlan(ctx, s.pool, raw)
}

func (s *M2Store) ReadRevalidation(ctx context.Context, databaseID, jobID security.ID) (migration.RevalidationSnapshot, error) {
	var snapshot migration.RevalidationSnapshot
	if !security.ValidID(databaseID) || !security.ValidID(jobID) {
		return snapshot, security.ErrNotFound
	}
	err := withTx(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT 1 FROM database_workflows WHERE database_id = $1 FOR SHARE", string(databaseID)); err != nil {
			return mapError(err)
		}
		var err error
		if snapshot.Job, err = findJob(ctx, tx, databaseID, jobID, ""); err != nil {
			return err
		}
		if snapshot.ChangeRequest, err = findChangeRequest(ctx, tx, databaseID, snapshot.Job.ChangeRequestID, ""); err != nil {
			return err
		}
		if snapshot.Definition, err = findDefinition(ctx, tx, databaseID, snapshot.ChangeRequest.MigrationID, ""); err != nil {
			return err
		}
		if snapshot.Position, err = readPosition(ctx, tx, databaseID); err != nil {
			return err
		}
		requester := snapshot.ChangeRequest.RequestedBy
		var id string
		var version int64
		if err := tx.QueryRow(ctx, "SELECT id::text, username, active, platform_admin, auth_version FROM users WHERE id = $1", string(requester)).
			Scan(&id, &snapshot.Requester.Username, &snapshot.Requester.Active, &snapshot.Requester.PlatformAdmin, &version); err != nil {
			return mapError(err)
		}
		snapshot.Requester.ID, snapshot.Requester.AuthVersion = security.ID(id), uint64(version)
		rows, err := tx.Query(ctx, `SELECT x.access FROM group_database_grants x JOIN group_memberships m ON m.group_id = x.group_id
			WHERE m.user_id = $1 AND x.database_id = $2`, string(requester), string(databaseID))
		grants, err := collect(rows, err, func(rows pgx.Rows) (security.DatabaseAccess, error) {
			var value string
			err := rows.Scan(&value)
			return security.DatabaseAccess(value), mapError(err)
		})
		if err != nil {
			return err
		}
		if snapshot.RequesterAccess, err = security.EffectiveAccess(grants); err != nil {
			return security.ErrUnavailable
		}
		if snapshot.CurrentApprovers, err = requiredApprovers(ctx, tx, databaseID); err != nil {
			return err
		}
		if source := snapshot.ChangeRequest.SourceJobID; source != "" {
			job, err := findJob(ctx, tx, databaseID, source, "")
			if err != nil {
				return err
			}
			snapshot.SourceJob = &job
			var raw []byte
			if err := tx.QueryRow(ctx, "SELECT execution_plan FROM jobs WHERE id = $1", string(source)).Scan(&raw); err != nil {
				return mapError(err)
			}
			if snapshot.SourcePlan, err = decodePlan(ctx, tx, raw); err != nil {
				return err
			}
		}
		if target := snapshot.ChangeRequest.TargetExecutionID; target != "" {
			execution, err := findExecution(ctx, tx, databaseID, target)
			if err != nil {
				return err
			}
			snapshot.TargetExecution = &execution
		}
		return nil
	})
	return snapshot, err
}
