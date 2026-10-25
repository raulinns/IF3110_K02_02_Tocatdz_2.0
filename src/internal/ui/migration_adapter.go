package ui

import (
	"context"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type MigrationAdapter struct{ service migration.Application }

var _ MigrationService = (*MigrationAdapter)(nil)

func NewMigrationAdapter(service migration.Application) (*MigrationAdapter, error) {
	if missingDependency(service) {
		return nil, security.ErrInvalidInput
	}
	return &MigrationAdapter{service: service}, nil
}
func migrationRequest(request DatabaseRequest) migration.Request {
	return migration.Request{Principal: request.Principal, DatabaseID: request.DatabaseID}
}
func migrationSummary(record migration.Summary) MigrationSummary {
	return MigrationSummary{ID: record.ID, Title: record.Title, Status: MigrationStatus(record.Status), Version: record.Version, Actor: record.Actor, ExecutedAt: record.ExecutedAt, UpdatedAt: record.UpdatedAt}
}
func (a *MigrationAdapter) History(ctx context.Context, req DatabaseRequest, query migration.HistoryQuery) (MigrationHistoryPage, error) {
	page, err := a.service.History(ctx, migrationRequest(req), query)
	if err != nil {
		return MigrationHistoryPage{}, err
	}
	result := MigrationHistoryPage{Items: make([]MigrationSummary, 0, len(page.Items)), Page: page.Page, Limit: page.Limit, HasNext: page.HasNext}
	for _, item := range page.Items {
		result.Items = append(result.Items, migrationSummary(item))
	}
	return result, nil
}
func (a *MigrationAdapter) Migration(ctx context.Context, req DatabaseRequest, id security.ID) (MigrationDetail, error) {
	record, err := a.service.Migration(ctx, migrationRequest(req), id)
	if err != nil {
		return MigrationDetail{}, err
	}
	return MigrationDetail{MigrationSummary: migrationSummary(record.Summary), SQL: record.SQL.Reveal(), Revision: record.Revision, FailureReason: record.Failure.Message()}, nil
}
func (a *MigrationAdapter) CreateDraft(ctx context.Context, req DatabaseRequest, draft MigrationDraft) (security.ID, error) {
	return a.service.CreateDraft(ctx, migrationRequest(req), migration.Draft{Title: draft.Title, SQL: security.NewSecret(draft.SQL)})
}
func (a *MigrationAdapter) UpdateDraft(ctx context.Context, req DatabaseRequest, id security.ID, draft MigrationDraft, revision uint64) error {
	return a.service.UpdateDraft(ctx, migrationRequest(req), id, migration.Draft{Title: draft.Title, SQL: security.NewSecret(draft.SQL)}, revision)
}
func (a *MigrationAdapter) Execute(ctx context.Context, req DatabaseRequest, id security.ID, revision uint64) error {
	result, err := a.service.Execute(ctx, migrationRequest(req), id, revision)
	if err != nil {
		return err
	}
	if result.MigrationID != id || (result.Status != migration.Applied && result.Status != migration.Failed) {
		return migration.ErrExecutionUncertain
	}
	return nil
}

func (a *MigrationAdapter) SupportsPlans() bool {
	planner, ok := a.service.(migration.PlanApplication)
	return ok && planner.SupportsPlans()
}
func (a *MigrationAdapter) Analyze(ctx context.Context, req DatabaseRequest, id security.ID, options migration.PlanOptions) (migration.PlanResult, error) {
	planner, ok := a.service.(migration.PlanApplication)
	if !ok || !planner.SupportsPlans() {
		return migration.PlanResult{}, security.ErrUnavailable
	}
	return planner.Analyze(ctx, migrationRequest(req), id, options)
}
