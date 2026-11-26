package api

import (
	"encoding/hex"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type workflowExecutionSummary struct {
	ID                   security.ID                  `json:"id"`
	DatabaseID           security.ID                  `json:"database_id"`
	MigrationID          security.ID                  `json:"migration_id"`
	MigrationRevision    uint64                       `json:"migration_revision"`
	Checksum             string                       `json:"checksum"`
	ChangeRequestID      *security.ID                 `json:"change_request_id"`
	JobID                *security.ID                 `json:"job_id"`
	Operation            migration.ChangeKind         `json:"operation"`
	Direction            migration.ExecutionDirection `json:"direction"`
	Status               string                       `json:"status"`
	RevertsExecutionID   *security.ID                 `json:"reverts_execution_id"`
	ForwardOfExecutionID *security.ID                 `json:"forward_of_execution_id"`
	ActorID              security.ID                  `json:"actor_id"`
	Actor                string                       `json:"actor"`
	StartedAt            time.Time                    `json:"started_at"`
	CommittedAt          time.Time                    `json:"committed_at"`
	PositionVersion      migration.PositionVersion    `json:"position_version"`
}

func executionSummaryResponse(e migration.CommittedExecutionSummary) workflowExecutionSummary {
	return workflowExecutionSummary{
		ID: e.ID, DatabaseID: e.DatabaseID, MigrationID: e.MigrationID, MigrationRevision: e.MigrationRevision,
		Checksum: hex.EncodeToString(e.Checksum[:]), ChangeRequestID: optionalValue(e.ChangeRequestID),
		JobID: optionalValue(e.JobID), Operation: e.Operation, Direction: e.Direction, Status: "committed",
		RevertsExecutionID: optionalValue(e.RevertsExecutionID), ForwardOfExecutionID: optionalValue(e.ForwardOfExecutionID),
		ActorID: e.ActorID, Actor: e.Actor, StartedAt: e.StartedAt.UTC(), CommittedAt: e.CommittedAt.UTC(),
		PositionVersion: e.PositionVersion,
	}
}

type workflowExecutionDetail struct {
	workflowExecutionSummary
	SQLSnapshot string `json:"sql_snapshot"`
}

func executionResponse(e migration.Execution) workflowExecutionDetail {
	sql := e.Definition.UpSQL
	if e.Direction == migration.DirectionDown {
		sql = e.Definition.DownSQL
	}
	return workflowExecutionDetail{
		workflowExecutionSummary: executionSummaryResponse(migration.CommittedExecutionSummary{
			ID: e.ID, DatabaseID: e.DatabaseID, MigrationID: e.MigrationID, MigrationRevision: e.MigrationRevision,
			Checksum: e.Checksum, ChangeRequestID: e.ChangeRequestID, JobID: e.JobID, Operation: e.Operation,
			Direction: e.Direction, RevertsExecutionID: e.RevertsExecutionID, ForwardOfExecutionID: e.ForwardOfExecutionID,
			ActorID: e.ActorID, Actor: e.Actor, StartedAt: e.StartedAt, CommittedAt: e.CommittedAt, PositionVersion: e.PositionVersion,
		}),
		SQLSnapshot: sql.Reveal(),
	}
}

type workflowPosition struct {
	Version             migration.PositionVersion `json:"version"`
	ActiveExecutionID   *security.ID              `json:"active_execution_id"`
	ActiveMigrationID   *security.ID              `json:"active_migration_id"`
	CanRollback         bool                      `json:"can_rollback"`
	RollbackExecutionID *security.ID              `json:"rollback_execution_id"`
	CanForward          bool                      `json:"can_forward"`
	ForwardExecutionID  *security.ID              `json:"forward_execution_id"`
	ForwardMigrationID  *security.ID              `json:"forward_migration_id"`
	BlockedByJobID      *security.ID              `json:"blocked_by_job_id"`
}

func positionResponse(p migration.PositionView) workflowPosition {
	return workflowPosition{
		Version: p.Version, ActiveExecutionID: optionalValue(p.Active.ID), ActiveMigrationID: optionalValue(p.Active.MigrationID),
		CanRollback: p.Rollback.ID != "", RollbackExecutionID: optionalValue(p.Rollback.ID),
		CanForward: p.Forward.ID != "", ForwardExecutionID: optionalValue(p.Forward.ID),
		ForwardMigrationID: optionalValue(p.Forward.MigrationID), BlockedByJobID: optionalValue(p.BlockedByJobID),
	}
}

type workflowExecutedStatement struct {
	Query  string   `json:"query"`
	Params []string `json:"params"`
	Batch  *int64   `json:"batch"`
}

func executedStatementResponse(s migration.ExecutedStatement) workflowExecutedStatement {
	params := make([]string, len(s.Params))
	for i := range params {
		params[i] = "[REDACTED]"
	}
	return workflowExecutedStatement{Query: migration.RedactStatement(s.Query), Params: params, Batch: s.Batch}
}

type workflowJobAttempt struct {
	Attempt    int                 `json:"attempt"`
	Status     migration.JobStatus `json:"status"`
	StartedAt  time.Time           `json:"started_at"`
	FinishedAt *time.Time          `json:"finished_at"`
	Error      *workflowJobFailure `json:"error"`
}

func jobAttemptResponse(a migration.JobAttempt) workflowJobAttempt {
	return workflowJobAttempt{
		Attempt: a.Attempt, Status: a.Status, StartedAt: a.StartedAt.UTC(), FinishedAt: a.FinishedAt, Error: jobFailureResponse(a.Error),
	}
}
