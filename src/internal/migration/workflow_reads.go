package migration

import (
	"context"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func workflowRead[T any](ctx context.Context, s *WorkflowService, req Request, action security.DatabaseAction, read func() (T, error)) (T, error) {
	var zero T
	if err := s.access(ctx, req, action); err != nil {
		return zero, err
	}
	value, err := read()
	if err != nil {
		return zero, safeError(err)
	}
	if err := s.access(ctx, req, action); err != nil {
		return zero, err
	}
	return value, nil
}

func (s *WorkflowService) ListDefinitions(ctx context.Context, req Request, page PageQuery) (Page[MigrationDefinitionSummary], error) {
	return workflowRead(ctx, s, req, security.ActionViewMigration, func() (Page[MigrationDefinitionSummary], error) {
		return s.deps.Store.ListDefinitions(ctx, req.DatabaseID, page)
	})
}

func (s *WorkflowService) FindDefinition(ctx context.Context, req Request, id security.ID) (MigrationDefinition, error) {
	return workflowRead(ctx, s, req, security.ActionViewMigration, func() (MigrationDefinition, error) {
		return s.deps.Store.FindDefinition(ctx, req.DatabaseID, id)
	})
}

func (s *WorkflowService) ListChangeRequests(ctx context.Context, req Request, query ChangeRequestQuery) (Page[ChangeRequest], error) {
	return workflowRead(ctx, s, req, security.ActionViewMigration, func() (Page[ChangeRequest], error) {
		return s.deps.Store.ListChangeRequests(ctx, req.DatabaseID, query)
	})
}

func (s *WorkflowService) FindChangeRequest(ctx context.Context, req Request, id security.ID) (ChangeRequest, error) {
	return workflowRead(ctx, s, req, security.ActionViewMigration, func() (ChangeRequest, error) {
		return s.deps.Store.FindChangeRequest(ctx, req.DatabaseID, id)
	})
}

// FindJob resolves the database after HTTP authentication, then applies its grant
// before reading job details.
func (s *WorkflowService) FindJob(ctx context.Context, actor security.Principal, id security.ID) (JobView, error) {
	req, err := s.jobRequest(ctx, actor, id)
	if err != nil {
		return JobView{}, err
	}
	return workflowRead(ctx, s, req, security.ActionViewJob, func() (JobView, error) {
		return s.deps.Store.FindJob(ctx, req.DatabaseID, id)
	})
}

func (s *WorkflowService) jobRequest(ctx context.Context, actor security.Principal, id security.ID) (Request, error) {
	if !security.ValidID(actor.UserID) || !security.ValidID(actor.SessionID) || actor.AuthVersion == 0 || actor.CSRFEpoch == 0 {
		return Request{}, security.ErrUnauthenticated
	}
	databaseID, err := s.deps.Store.JobDatabase(ctx, id)
	if err != nil {
		return Request{}, safeError(err)
	}
	return Request{Principal: actor, DatabaseID: databaseID}, nil
}

func (s *WorkflowService) ListAttempts(ctx context.Context, actor security.Principal, id security.ID, page PageQuery) (Page[JobAttempt], error) {
	req, err := s.jobRequest(ctx, actor, id)
	if err != nil {
		return Page[JobAttempt]{}, err
	}
	return workflowRead(ctx, s, req, security.ActionViewJob, func() (Page[JobAttempt], error) {
		return s.deps.Store.ListAttempts(ctx, req.DatabaseID, id, page)
	})
}

func (s *WorkflowService) ListJobs(ctx context.Context, req Request, query JobQuery) (Page[JobView], error) {
	return workflowRead(ctx, s, req, security.ActionViewJob, func() (Page[JobView], error) {
		return s.deps.Store.ListJobs(ctx, req.DatabaseID, query)
	})
}

func (s *WorkflowService) ListExecutions(ctx context.Context, req Request, query ExecutionQuery) (Page[CommittedExecutionSummary], error) {
	return workflowRead(ctx, s, req, security.ActionViewHistory, func() (Page[CommittedExecutionSummary], error) {
		return s.deps.Store.ListExecutions(ctx, req.DatabaseID, query)
	})
}

func (s *WorkflowService) FindExecution(ctx context.Context, req Request, id security.ID) (Execution, error) {
	return workflowRead(ctx, s, req, security.ActionViewHistory, func() (Execution, error) {
		return s.deps.Store.FindExecution(ctx, req.DatabaseID, id)
	})
}

func (s *WorkflowService) ListExecutedStatements(ctx context.Context, req Request, id security.ID, page PageQuery) (Page[ExecutedStatement], error) {
	return workflowRead(ctx, s, req, security.ActionViewHistory, func() (Page[ExecutedStatement], error) {
		return s.deps.Store.ListExecutedStatements(ctx, req.DatabaseID, id, page)
	})
}

type PositionView struct {
	Version                   PositionVersion
	Active, Rollback, Forward CommittedExecutionSummary
	BlockedByJobID            security.ID
}

func (s *WorkflowService) Position(ctx context.Context, req Request) (PositionView, error) {
	return workflowRead(ctx, s, req, security.ActionViewHistory, func() (PositionView, error) {
		position, err := s.deps.Store.ReadPosition(ctx, req.DatabaseID)
		if err != nil {
			return PositionView{}, err
		}
		view := PositionView{Version: position.Version, BlockedByJobID: position.BlockedByJobID,
			Active: historyTarget(position, ChangeRollback)}
		if view.BlockedByJobID != "" {
			return view, nil
		}
		view.Forward = historyTarget(position, ChangeForward)
		if view.Active.ID != "" {
			snapshot, err := s.deps.Store.FindExecution(ctx, req.DatabaseID, view.Active.ID)
			if err != nil {
				return PositionView{}, err
			}
			// Committed executions are immutable, so a separate read cannot
			// change the snapshot described by the selected position.
			if !snapshot.Definition.Irreversible && !snapshot.Definition.DownSQL.Empty() {
				view.Rollback = view.Active
			}
		}
		return view, nil
	})
}
