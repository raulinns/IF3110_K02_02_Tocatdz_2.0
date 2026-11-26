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
	if !security.ValidID(actor.UserID) || !security.ValidID(actor.SessionID) || actor.AuthVersion == 0 || actor.CSRFEpoch == 0 {
		return JobView{}, security.ErrUnauthenticated
	}
	databaseID, err := s.deps.Store.JobDatabase(ctx, id)
	if err != nil {
		return JobView{}, safeError(err)
	}
	req := Request{Principal: actor, DatabaseID: databaseID}
	return workflowRead(ctx, s, req, security.ActionViewJob, func() (JobView, error) {
		return s.deps.Store.FindJob(ctx, databaseID, id)
	})
}
