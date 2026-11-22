package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// WithWorkflow mengunci guard database lalu memeriksa ulang session pemohon. Error dari fn
// me-rollback semua perubahan.
func (s *M2Store) WithWorkflow(ctx context.Context, request migration.Request, fn func(migration.WorkflowTx) error) error {
	if fn == nil || !security.ValidID(request.DatabaseID) {
		return security.ErrInvalidInput
	}
	return withTx(ctx, s.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var version int64
		var blocked string
		if err := tx.QueryRow(ctx, `SELECT position_version, COALESCE(blocked_by_job_id::text, '') FROM database_workflows
			WHERE database_id = $1 FOR UPDATE`, string(request.DatabaseID)).Scan(&version, &blocked); err != nil {
			return mapError(err)
		}
		if err := checkActor(ctx, tx, request); err != nil {
			return err
		}
		return fn(&workflowTx{ctx: ctx, tx: tx, request: request, version: migration.PositionVersion(version), blocked: security.ID(blocked)})
	})
}

type workflowTx struct {
	ctx     context.Context
	tx      pgx.Tx
	request migration.Request
	version migration.PositionVersion
	blocked security.ID
}

var _ migration.WorkflowTx = (*workflowTx)(nil)

func (t *workflowTx) database() security.ID { return t.request.DatabaseID }

func (t *workflowTx) Definition(migrationID security.ID) (migration.MigrationDefinition, error) {
	return findDefinition(t.ctx, t.tx, t.database(), migrationID, "")
}

func (t *workflowTx) ChangeRequest(changeRequestID security.ID) (migration.ChangeRequest, error) {
	return findChangeRequest(t.ctx, t.tx, t.database(), changeRequestID, "")
}

func (t *workflowTx) Position() (migration.Position, error) {
	return readPosition(t.ctx, t.tx, t.database())
}

func (t *workflowTx) Execution(executionID security.ID) (migration.Execution, error) {
	return findExecution(t.ctx, t.tx, t.database(), executionID)
}

func (t *workflowTx) RequiredApprovers() ([]migration.RequiredApproval, error) {
	return requiredApprovers(t.ctx, t.tx, t.database())
}

func (t *workflowTx) RequireAccess(access security.DatabaseAccess) error {
	rows, err := t.tx.Query(t.ctx, `SELECT x.access FROM group_database_grants x JOIN group_memberships m ON m.group_id = x.group_id
		WHERE m.user_id = $1 AND x.database_id = $2`, string(t.request.Principal.UserID), string(t.database()))
	grants, err := collect(rows, err, func(rows pgx.Rows) (security.DatabaseAccess, error) {
		var value string
		err := rows.Scan(&value)
		return security.DatabaseAccess(value), mapError(err)
	})
	if err != nil {
		return err
	}
	effective, err := security.EffectiveAccess(grants)
	if err != nil {
		return security.ErrUnavailable
	}
	switch access {
	case security.AccessViewOnly:
		if effective == security.AccessNone {
			return security.ErrForbidden
		}
	case security.AccessViewEdit:
		if effective != security.AccessViewEdit {
			return security.ErrForbidden
		}
	default:
		return security.ErrInvalidInput
	}
	return nil
}

func validDefinitionInput(input migration.DefinitionInput) bool {
	if input.Mode != migration.ModeNormal && input.Mode != migration.ModeBatch {
		return false
	}
	if (input.Mode == migration.ModeBatch) != (input.Batch != nil) {
		return false
	}
	return input.Irreversible == input.DownSQL.Empty()
}

func downArg(input migration.DefinitionInput) any {
	if input.Irreversible {
		return nil
	}
	return input.DownSQL.Reveal()
}

func (t *workflowTx) CreateDefinition(definition migration.MigrationDefinition) error {
	var zero migration.DefinitionChecksum
	if !security.ValidID(definition.ID) || definition.DatabaseID != t.database() || definition.Revision != 1 ||
		(definition.State != "" && definition.State != migration.DefinitionDraft) || definition.CreatedBy != t.request.Principal.UserID ||
		definition.Checksum == zero || !validDefinitionInput(definition.DefinitionInput) {
		return security.ErrInvalidInput
	}
	batch, err := encodeBatch(definition.Batch)
	if err != nil {
		return err
	}
	_, err = t.tx.Exec(t.ctx, `INSERT INTO migrations (id, database_id, title, sql, status, revision, created_by, created_at, updated_at,
		down_sql, irreversible, irreversible_reason, mode, batch_config, definition_checksum)
		VALUES ($1, $2, $3, $4, 'draft', 1, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		string(definition.ID), string(definition.DatabaseID), definition.Title, definition.UpSQL.Reveal(), string(definition.CreatedBy),
		micro(definition.CreatedAt), micro(definition.UpdatedAt), downArg(definition.DefinitionInput), definition.Irreversible,
		definition.IrreversibleReason, string(definition.Mode), batch, definition.Checksum[:])
	return mapError(err)
}

func (t *workflowTx) openChangeRequestFor(migrationID security.ID) (bool, error) {
	var open bool
	err := t.tx.QueryRow(t.ctx, `SELECT EXISTS (SELECT 1 FROM change_requests WHERE migration_id = $1
		AND status IN ('pending_review', 'approved', 'queued', 'running', 'uncertain'))`, string(migrationID)).Scan(&open)
	return open, mapError(err)
}

func (t *workflowTx) UpdateDefinition(migrationID security.ID, expectedRevision uint64, input migration.DefinitionInput, checksum migration.DefinitionChecksum, at time.Time) (migration.MigrationDefinition, error) {
	var zero migration.DefinitionChecksum
	if checksum == zero || !validDefinitionInput(input) {
		return migration.MigrationDefinition{}, security.ErrInvalidInput
	}
	current, err := findDefinition(t.ctx, t.tx, t.database(), migrationID, " FOR UPDATE")
	if err != nil {
		return migration.MigrationDefinition{}, err
	}
	if current.State != migration.DefinitionDraft || current.Revision != expectedRevision {
		return migration.MigrationDefinition{}, security.ErrConflict
	}
	if open, err := t.openChangeRequestFor(migrationID); err != nil {
		return migration.MigrationDefinition{}, err
	} else if open {
		return migration.MigrationDefinition{}, security.ErrConflict
	}
	batch, err := encodeBatch(input.Batch)
	if err != nil {
		return migration.MigrationDefinition{}, err
	}
	if _, err := t.tx.Exec(t.ctx, `UPDATE migrations SET title = $2, sql = $3, down_sql = $4, irreversible = $5, irreversible_reason = $6,
		mode = $7, batch_config = $8, definition_checksum = $9, updated_at = $10, revision = revision + 1 WHERE id = $1`,
		string(migrationID), input.Title, input.UpSQL.Reveal(), downArg(input), input.Irreversible, input.IrreversibleReason,
		string(input.Mode), batch, checksum[:], micro(at)); err != nil {
		return migration.MigrationDefinition{}, mapError(err)
	}
	return findDefinition(t.ctx, t.tx, t.database(), migrationID, "")
}

func validKind(kind migration.ChangeKind) bool {
	switch kind {
	case migration.ChangeApply, migration.ChangeRollback, migration.ChangeForward, migration.ChangeResume, migration.ChangeRevertBatches:
		return true
	}
	return false
}

// Barrier hanya meloloskan pemulihan dari job yang memblokir.
func (t *workflowTx) passesBarrier(sourceJobID security.ID) bool {
	return t.blocked == "" || sourceJobID == t.blocked
}

func (t *workflowTx) CreateChangeRequest(change migration.ChangeRequest) error {
	if !security.ValidID(change.ID) || change.DatabaseID != t.database() || !validKind(change.Kind) ||
		(change.Status != "" && change.Status != migration.CRPendingReview) || change.RequestedBy != t.request.Principal.UserID ||
		len(change.Approvals) == 0 {
		return security.ErrInvalidInput
	}
	for _, approval := range change.Approvals {
		if !security.ValidID(approval.GroupID) || !security.ValidID(approval.LeaderUserID) || approval.Decision != nil ||
			(approval.Access != security.AccessViewOnly && approval.Access != security.AccessViewEdit) {
			return security.ErrInvalidInput
		}
	}
	definition, err := findDefinition(t.ctx, t.tx, t.database(), change.MigrationID, " FOR UPDATE")
	if err != nil {
		return err
	}
	if definition.Revision != change.MigrationRevision || definition.Checksum != change.Checksum ||
		change.ExpectedPosition != t.version || !t.passesBarrier(change.SourceJobID) {
		return security.ErrConflict
	}
	// Definisi dibekukan saat submit.
	if definition.State == migration.DefinitionDraft {
		if _, err := t.tx.Exec(t.ctx, "UPDATE migrations SET frozen_at = $2 WHERE id = $1", string(change.MigrationID), micro(change.CreatedAt)); err != nil {
			return mapError(err)
		}
	}
	at := micro(change.CreatedAt)
	if _, err := t.tx.Exec(t.ctx, `INSERT INTO change_requests (id, database_id, migration_id, kind, status, revision, migration_revision,
		checksum, target_execution_id, source_job_id, expected_position_version, requested_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'pending_review', 1, $5, $6, $7, $8, $9, $10, $11, $11)`,
		string(change.ID), string(change.DatabaseID), string(change.MigrationID), string(change.Kind), int64(change.MigrationRevision),
		change.Checksum[:], nullID(change.TargetExecutionID), nullID(change.SourceJobID), int64(change.ExpectedPosition),
		string(change.RequestedBy), at); err != nil {
		return mapError(err)
	}
	for _, approval := range change.Approvals {
		if _, err := t.tx.Exec(t.ctx, `INSERT INTO change_request_approvals (change_request_id, group_id, group_name, leader_user_id, access)
			VALUES ($1, $2, $3, $4, $5)`, string(change.ID), string(approval.GroupID), approval.GroupName, string(approval.LeaderUserID),
			string(approval.Access)); err != nil {
			return mapError(err)
		}
	}
	return nil
}

func sameDecision(existing *migration.ApprovalDecision, decision migration.ApprovalDecision) bool {
	return existing.ActorID == decision.ActorID && existing.Decision == decision.Decision && existing.Note == decision.Note
}

func (t *workflowTx) RecordDecision(changeRequestID security.ID, expectedRevision uint64, decision migration.ApprovalDecision) (migration.ChangeRequest, error) {
	if decision.ActorID != t.request.Principal.UserID || !security.ValidID(decision.GroupID) ||
		(decision.Decision != migration.DecisionApprove && decision.Decision != migration.DecisionReject) {
		return migration.ChangeRequest{}, security.ErrInvalidInput
	}
	current, err := findChangeRequest(t.ctx, t.tx, t.database(), changeRequestID, " FOR UPDATE")
	if err != nil {
		return migration.ChangeRequest{}, err
	}
	var approval *migration.GroupApproval
	for i := range current.Approvals {
		if current.Approvals[i].GroupID == decision.GroupID {
			approval = &current.Approvals[i]
		}
	}
	if approval == nil {
		return migration.ChangeRequest{}, security.ErrForbidden
	}
	// Replay yang sama mengembalikan state saat ini.
	if approval.Decision != nil {
		if sameDecision(approval.Decision, decision) {
			return current, nil
		}
		return migration.ChangeRequest{}, security.ErrConflict
	}
	if current.Status != migration.CRPendingReview || current.Revision != expectedRevision {
		return migration.ChangeRequest{}, security.ErrConflict
	}
	// Kebijakan sementara tim: pemohon tidak boleh meng-approve CR miliknya.
	if approval.LeaderUserID != decision.ActorID || current.RequestedBy == decision.ActorID {
		return migration.ChangeRequest{}, security.ErrForbidden
	}
	if leader, err := isLeader(t.ctx, t.tx, decision.ActorID, decision.GroupID); err != nil {
		return migration.ChangeRequest{}, err
	} else if !leader {
		return migration.ChangeRequest{}, security.ErrForbidden
	}
	at := micro(decision.DecidedAt)
	if _, err := t.tx.Exec(t.ctx, `UPDATE change_request_approvals SET decision = $3, decided_by = $4, note = $5, decided_at = $6
		WHERE change_request_id = $1 AND group_id = $2`, string(changeRequestID), string(decision.GroupID), string(decision.Decision),
		string(decision.ActorID), decision.Note, at); err != nil {
		return migration.ChangeRequest{}, mapError(err)
	}
	if _, err := t.tx.Exec(t.ctx, `UPDATE change_requests c SET revision = revision + 1, updated_at = $2,
		status = CASE
			WHEN EXISTS (SELECT 1 FROM change_request_approvals a WHERE a.change_request_id = c.id AND a.decision = 'reject') THEN 'rejected'
			WHEN NOT EXISTS (SELECT 1 FROM change_request_approvals a WHERE a.change_request_id = c.id AND a.decision IS DISTINCT FROM 'approve') THEN 'approved'
			ELSE 'pending_review' END
		WHERE c.id = $1`, string(changeRequestID), at); err != nil {
		return migration.ChangeRequest{}, mapError(err)
	}
	return findChangeRequest(t.ctx, t.tx, t.database(), changeRequestID, "")
}

func allApproved(request migration.ChangeRequest) bool {
	for _, approval := range request.Approvals {
		if approval.Decision == nil || approval.Decision.Decision != migration.DecisionApprove {
			return false
		}
	}
	return len(request.Approvals) > 0
}

func (t *workflowTx) EnqueueApproved(changeRequestID security.ID, expectedRevision uint64, spec migration.JobSpec) (migration.JobView, error) {
	current, err := findChangeRequest(t.ctx, t.tx, t.database(), changeRequestID, " FOR UPDATE")
	if err != nil {
		return migration.JobView{}, err
	}
	if (spec.DatabaseID != "" && spec.DatabaseID != t.database()) || (spec.ChangeRequestID != "" && spec.ChangeRequestID != changeRequestID) {
		return migration.JobView{}, security.ErrConflict
	}
	// Execute ganda mengembalikan job yang sudah ada.
	existing, err := scanJob(t.tx.QueryRow(t.ctx, "SELECT "+jobColumns+jobFrom+" WHERE j.change_request_id = $1", string(changeRequestID)))
	if err == nil {
		if spec.MaxAttempts != 0 && spec.MaxAttempts != existing.MaxAttempts {
			return migration.JobView{}, security.ErrConflict
		}
		return existing, nil
	}
	if !errors.Is(err, security.ErrNotFound) {
		return migration.JobView{}, err
	}
	if spec.RequestedBy == "" {
		spec.RequestedBy = t.request.Principal.UserID
	}
	if !security.ValidID(spec.ID) || !security.ValidID(spec.ExecutionID) || spec.RequestedBy != t.request.Principal.UserID ||
		spec.MaxAttempts < 1 || spec.MaxAttempts > migration.MaxAttemptsLimit || spec.QueuedAt.IsZero() {
		return migration.JobView{}, security.ErrInvalidInput
	}
	if current.Status != migration.CRApproved || current.Revision != expectedRevision || !allApproved(current) {
		return migration.JobView{}, security.ErrConflict
	}
	definition, err := findDefinition(t.ctx, t.tx, t.database(), current.MigrationID, "")
	if err != nil {
		return migration.JobView{}, err
	}
	if definition.Revision != current.MigrationRevision || definition.Checksum != current.Checksum ||
		current.ExpectedPosition != t.version || !t.passesBarrier(current.SourceJobID) {
		return migration.JobView{}, security.ErrConflict
	}
	at := micro(spec.QueuedAt)
	if _, err := t.tx.Exec(t.ctx, `INSERT INTO jobs (id, database_id, change_request_id, execution_id, source_job_id, requested_by, status,
		max_attempts, run_after, request_id, trace_context, queued_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'queued', $7, $8, $9, $10, $8)`,
		string(spec.ID), string(t.database()), string(changeRequestID), string(spec.ExecutionID), nullID(current.SourceJobID),
		string(spec.RequestedBy), spec.MaxAttempts, at, spec.RequestID, spec.TraceContext.Reveal()); err != nil {
		return migration.JobView{}, mapError(err)
	}
	if _, err := t.tx.Exec(t.ctx, "UPDATE change_requests SET status = 'queued', revision = revision + 1, updated_at = $2 WHERE id = $1",
		string(changeRequestID), at); err != nil {
		return migration.JobView{}, mapError(err)
	}
	return findJob(t.ctx, t.tx, t.database(), spec.ID, "")
}
