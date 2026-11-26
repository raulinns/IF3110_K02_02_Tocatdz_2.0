package api

import (
	"encoding/hex"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type definitionFields struct {
	Title              string                  `json:"title" json_required:"true"`
	UpSQL              string                  `json:"up_sql" json_required:"true"`
	DownSQL            *string                 `json:"down_sql" json_required:"true"`
	Irreversible       bool                    `json:"irreversible" json_required:"true"`
	IrreversibleReason string                  `json:"irreversible_reason" json_required:"true"`
	Mode               migration.ExecutionMode `json:"mode" json_required:"true"`
	Batch              *workflowBatch          `json:"batch" json_required:"true"`
}

type workflowBatch struct {
	Schema     string  `json:"schema" json_required:"true"`
	Table      string  `json:"table" json_required:"true"`
	KeyColumn  string  `json:"key_column" json_required:"true"`
	BatchSize  int     `json:"batch_size" json_required:"true"`
	ScopeWhere *string `json:"scope_where" json_required:"true"`
}

func (f definitionFields) domain() (migration.DefinitionInput, error) {
	if f.Irreversible != (f.DownSQL == nil) {
		return migration.DefinitionInput{}, &ValidationError{Fields: map[string][]string{"down_sql": {"Gunakan null hanya untuk migration irreversible."}}}
	}
	input := migration.DefinitionInput{
		Title: f.Title, UpSQL: security.NewSecret(f.UpSQL), DownSQL: inputSecret(f.DownSQL),
		Irreversible: f.Irreversible, IrreversibleReason: f.IrreversibleReason, Mode: f.Mode,
	}
	if b := f.Batch; b != nil {
		input.Batch = &migration.BatchConfig{
			Schema: b.Schema, Table: b.Table, KeyColumn: b.KeyColumn,
			BatchSize: b.BatchSize, ScopeWhere: inputSecret(b.ScopeWhere),
		}
	}
	return input, nil
}

func inputSecret(value *string) security.Secret {
	if value == nil {
		return security.Secret{}
	}
	return security.NewSecret(*value)
}

func optionalValue[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

type definitionMeta struct {
	ID               security.ID               `json:"id"`
	DatabaseID       security.ID               `json:"database_id"`
	State            migration.DefinitionState `json:"state"`
	Revision         uint64                    `json:"revision"`
	Checksum         string                    `json:"checksum"`
	CreatedBy        security.ID               `json:"created_by"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
	FirstExecutionAt *time.Time                `json:"first_execution_at"`
}

type workflowDefinition struct {
	definitionMeta
	definitionFields
}

func definitionResponse(d migration.MigrationDefinition) workflowDefinition {
	response := workflowDefinition{
		definitionMeta: definitionMeta{
			ID: d.ID, DatabaseID: d.DatabaseID, State: d.State, Revision: d.Revision, Checksum: hex.EncodeToString(d.Checksum[:]),
			CreatedBy: d.CreatedBy, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(), FirstExecutionAt: d.FirstExecutionAt,
		},
		definitionFields: definitionFields{
			Title: d.Title, UpSQL: d.UpSQL.Reveal(), DownSQL: optionalValue(d.DownSQL.Reveal()),
			Irreversible: d.Irreversible, IrreversibleReason: d.IrreversibleReason, Mode: d.Mode,
		},
	}
	if b := d.Batch; b != nil {
		response.Batch = &workflowBatch{Schema: b.Schema, Table: b.Table, KeyColumn: b.KeyColumn,
			BatchSize: b.BatchSize, ScopeWhere: optionalValue(b.ScopeWhere.Reveal())}
	}
	return response
}

type workflowDefinitionSummary struct {
	definitionMeta
	Title        string                  `json:"title"`
	Irreversible bool                    `json:"irreversible"`
	Mode         migration.ExecutionMode `json:"mode"`
}

func definitionSummaryResponse(d migration.MigrationDefinitionSummary) workflowDefinitionSummary {
	return workflowDefinitionSummary{
		definitionMeta: definitionMeta{
			ID: d.ID, DatabaseID: d.DatabaseID, State: d.State, Revision: d.Revision, Checksum: hex.EncodeToString(d.Checksum[:]),
			CreatedBy: d.CreatedBy, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(), FirstExecutionAt: d.FirstExecutionAt,
		},
		Title: d.Title, Irreversible: d.Irreversible, Mode: d.Mode,
	}
}

type workflowApproval struct {
	GroupID   security.ID             `json:"group_id"`
	GroupName string                  `json:"group_name"`
	LeaderID  security.ID             `json:"leader_user_id"`
	Access    security.DatabaseAccess `json:"access"`
	Decision  *migration.Decision     `json:"decision"`
	ActorID   *security.ID            `json:"actor_id"`
	Note      string                  `json:"note"`
	DecidedAt *time.Time              `json:"decided_at"`
}

type workflowChangeResponse struct {
	ID                security.ID               `json:"id"`
	DatabaseID        security.ID               `json:"database_id"`
	MigrationID       security.ID               `json:"migration_id"`
	MigrationRevision uint64                    `json:"migration_revision"`
	Checksum          string                    `json:"checksum"`
	Revision          uint64                    `json:"revision"`
	Kind              migration.ChangeKind      `json:"kind"`
	Status            migration.CRStatus        `json:"status"`
	RequestedBy       security.ID               `json:"requested_by"`
	TargetExecutionID *security.ID              `json:"target_execution_id"`
	SourceJobID       *security.ID              `json:"source_job_id"`
	ExpectedPosition  migration.PositionVersion `json:"expected_position_version"`
	Approvals         []workflowApproval        `json:"approvals"`
	CreatedAt         time.Time                 `json:"created_at"`
	UpdatedAt         time.Time                 `json:"updated_at"`
}

func changeResponse(change migration.ChangeRequest) workflowChangeResponse {
	approvals := make([]workflowApproval, 0, len(change.Approvals))
	for _, approval := range change.Approvals {
		item := workflowApproval{GroupID: approval.GroupID, GroupName: approval.GroupName,
			LeaderID: approval.LeaderUserID, Access: approval.Access}
		if vote := approval.Decision; vote != nil {
			at := vote.DecidedAt.UTC()
			item.Decision, item.ActorID, item.Note, item.DecidedAt = &vote.Decision, &vote.ActorID, vote.Note, &at
		}
		approvals = append(approvals, item)
	}
	return workflowChangeResponse{
		ID: change.ID, DatabaseID: change.DatabaseID, MigrationID: change.MigrationID,
		MigrationRevision: change.MigrationRevision, Checksum: hex.EncodeToString(change.Checksum[:]), Revision: change.Revision,
		Kind: change.Kind, Status: change.Status, RequestedBy: change.RequestedBy, TargetExecutionID: optionalValue(change.TargetExecutionID),
		SourceJobID: optionalValue(change.SourceJobID), ExpectedPosition: change.ExpectedPosition,
		Approvals: approvals, CreatedAt: change.CreatedAt.UTC(), UpdatedAt: change.UpdatedAt.UTC(),
	}
}

type workflowJobProgress struct {
	BatchesCommitted int64  `json:"batches_committed"`
	RowsCommitted    int64  `json:"rows_committed"`
	BatchesReverted  int64  `json:"batches_reverted"`
	RowsReverted     int64  `json:"rows_reverted"`
	TotalBatches     *int64 `json:"total_batches"`
}

type workflowJobFailure struct {
	Code      migration.JobErrorCode `json:"code"`
	Message   string                 `json:"message"`
	SQLState  *string                `json:"sqlstate"`
	Retryable bool                   `json:"retryable"`
}

type workflowJobResponse struct {
	ID              security.ID             `json:"id"`
	DatabaseID      security.ID             `json:"database_id"`
	ChangeRequestID security.ID             `json:"change_request_id"`
	ExecutionID     security.ID             `json:"execution_id"`
	MigrationID     security.ID             `json:"migration_id"`
	Kind            migration.ChangeKind    `json:"kind"`
	Mode            migration.ExecutionMode `json:"mode"`
	Status          migration.JobStatus     `json:"status"`
	Attempt         int                     `json:"attempt"`
	MaxAttempts     int                     `json:"max_attempts"`
	RunAfter        time.Time               `json:"run_after"`
	QueuedAt        time.Time               `json:"queued_at"`
	StartedAt       *time.Time              `json:"started_at"`
	FinishedAt      *time.Time              `json:"finished_at"`
	Partial         bool                    `json:"partial"`
	Progress        workflowJobProgress     `json:"progress"`
	Error           *workflowJobFailure     `json:"error"`
}

func jobResponse(job migration.JobView) workflowJobResponse {
	p := job.Progress
	response := workflowJobResponse{
		ID: job.ID, DatabaseID: job.DatabaseID, ChangeRequestID: job.ChangeRequestID, ExecutionID: job.ExecutionID,
		MigrationID: job.MigrationID, Kind: job.Kind, Mode: job.Mode, Status: job.Status, Attempt: job.Attempt,
		MaxAttempts: job.MaxAttempts, RunAfter: job.RunAfter.UTC(), QueuedAt: job.QueuedAt.UTC(), StartedAt: job.StartedAt,
		FinishedAt: job.FinishedAt, Partial: job.Partial,
		Progress: workflowJobProgress{p.BatchesCommitted, p.RowsCommitted, p.BatchesReverted, p.RowsReverted, p.TotalBatches},
	}
	if failure := job.Error; failure != nil {
		response.Error = &workflowJobFailure{Code: failure.Code, Message: failure.Message,
			SQLState: optionalValue(failure.SQLState), Retryable: failure.Retryable}
	}
	return response
}
