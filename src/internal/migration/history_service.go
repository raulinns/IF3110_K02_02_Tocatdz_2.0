package migration

import (
	"context"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func (s *WorkflowService) historyStore(ctx context.Context, req Request) (AttemptHistoryStore, error) {
	if err := s.access(ctx, req, security.ActionViewMigration); err != nil {
		return nil, err
	}
	store, ok := s.deps.Store.(AttemptHistoryStore)
	if !ok {
		return nil, security.ErrUnavailable
	}
	return store, nil
}
func (s *WorkflowService) ListHistory(ctx context.Context, req Request, q ExecutionQuery) (Page[HistoryEntry], error) {
	store, err := s.historyStore(ctx, req)
	if err != nil {
		return Page[HistoryEntry]{}, err
	}
	return store.ListHistory(ctx, req.DatabaseID, q)
}
func (s *WorkflowService) FindHistory(ctx context.Context, req Request, id security.ID) (HistoryEntry, error) {
	store, err := s.historyStore(ctx, req)
	if err != nil {
		return HistoryEntry{}, err
	}
	if !security.ValidID(id) {
		return HistoryEntry{}, security.ErrNotFound
	}
	return store.FindHistory(ctx, req.DatabaseID, id)
}
func (s *WorkflowService) HistoryStatements(ctx context.Context, req Request, id security.ID, q PageQuery) (Page[HistoryStatement], error) {
	store, err := s.historyStore(ctx, req)
	if err != nil {
		return Page[HistoryStatement]{}, err
	}
	if !security.ValidID(id) {
		return Page[HistoryStatement]{}, security.ErrNotFound
	}
	return store.HistoryStatements(ctx, req.DatabaseID, id, q)
}

func validChangeDefinition(input SubmitChange, d MigrationDefinition) bool {
	if input.RetryOfJobID != "" {
		return security.ValidID(input.RetryOfJobID) && d.Mode == ModeNormal && d.State == DefinitionFrozen && input.SourceJobID == ""
	}
	return validSubmissionState(input.Kind, d)
}

func checkRetrySubmission(tx WorkflowTx, c ChangeRequest, d MigrationDefinition) error {
	if !security.ValidID(c.RetryOfJobID) || c.SourceJobID != "" || d.Mode != ModeNormal {
		return security.ErrConflict
	}
	source, plan, err := recoverySource(tx, c.RetryOfJobID)
	if err != nil {
		return err
	}
	if !validRetryPlan(source, plan, c, d) {
		return security.ErrConflict
	}
	return nil
}

func validRetryPlan(source JobView, plan *ExecutionPlan, c ChangeRequest, d MigrationDefinition) bool {
	if plan == nil || source.ID != c.RetryOfJobID || plan.JobID != source.ID || plan.DatabaseID != c.DatabaseID ||
		source.Status != JobFailed || source.DatabaseID != c.DatabaseID || source.MigrationID != d.ID || source.Mode != ModeNormal ||
		source.Kind != c.Kind || plan.Kind != c.Kind || plan.Definition.Revision != d.Revision || plan.Definition.Checksum != c.Checksum ||
		plan.TargetExecutionID != c.TargetExecutionID || plan.SourceJobID != "" || ChecksumDefinition(plan.Definition.DefinitionInput) != c.Checksum {
		return false
	}
	return true
}

// Manual retry is a fresh CR with current approvers, never an enqueue bypass.
func (s *WorkflowService) RetryFailedJob(ctx context.Context, req Request, id security.ID) (ChangeRequest, error) {
	if err := s.writeAccess(ctx, req, security.ActionSubmitMigration); err != nil {
		return ChangeRequest{}, err
	}
	if !security.ValidID(id) {
		return ChangeRequest{}, security.ErrNotFound
	}
	source, err := s.deps.Store.FindJob(ctx, req.DatabaseID, id)
	if err != nil {
		return ChangeRequest{}, safeError(err)
	}
	if source.Status != JobFailed || source.Mode != ModeNormal {
		return ChangeRequest{}, security.ErrConflict
	}
	switch source.Kind {
	case ChangeApply, ChangeRollback, ChangeForward:
	default:
		return ChangeRequest{}, security.ErrConflict
	}
	original, err := s.deps.Store.FindChangeRequest(ctx, req.DatabaseID, source.ChangeRequestID)
	if err != nil {
		return ChangeRequest{}, safeError(err)
	}
	return s.Submit(ctx, req, SubmitChange{MigrationID: original.MigrationID, MigrationRevision: original.MigrationRevision,
		Kind: original.Kind, TargetExecutionID: original.TargetExecutionID, RetryOfJobID: id})
}
