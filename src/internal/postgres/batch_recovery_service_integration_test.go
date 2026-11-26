package postgres

import (
	"context"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/dbms"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/worker"
)

type recoveryTestEngine struct{}

func (recoveryTestEngine) Engine(context.Context, security.ID) (dbms.Engine, error) {
	return dbms.PostgreSQL, nil
}

func TestBatchRecoveryServiceWithRealStoreAndRevalidator(t *testing.T) {
	for _, kind := range []migration.ChangeKind{migration.ChangeResume, migration.ChangeRevertBatches} {
		t.Run(string(kind), func(t *testing.T) {
			f := newWorkerFixture(t)
			ctx := context.Background()
			seedItems(t, f.target, 350)
			exec(t, f.target, "INSERT INTO public.fail_on VALUES (250)")
			clock := security.SystemClock{}
			accounts, err := NewStore(f.platform, clock)
			must(t, err)
			auth, err := security.NewBackendAuthorizer(accounts, accounts, clock, security.DefaultSessionPolicy())
			must(t, err)
			service, err := migration.NewWorkflowService(migration.WorkflowConfig{
				Store: f.store, Authorizer: auth, Engines: recoveryTestEngine{}, IDs: security.NewTokenGenerator(), Clock: clock,
			})
			must(t, err)
			revalidator, err := migration.NewRevalidator(f.store, clock)
			must(t, err)
			f.worker, err = worker.New(worker.Config{Store: f.store, Coordinator: f.targets, Revalidator: revalidator,
				Runner: f.runner, Progress: f.store, Clock: clock, Owner: "real-recovery-test"})
			must(t, err)
			definition, err := service.CreateDefinition(ctx, f.requester, migration.DefinitionInput{
				Title: "Recovery", UpSQL: security.NewSecret("amount = amount + 1"), DownSQL: security.NewSecret("amount = amount - 1"),
				Mode: migration.ModeBatch, Batch: &migration.BatchConfig{Schema: "public", Table: "items", KeyColumn: "id", BatchSize: 100},
			})
			must(t, err)
			queue := func(kind migration.ChangeKind, source security.ID) migration.JobView {
				t.Helper()
				change, err := service.Submit(ctx, f.requester, migration.SubmitChange{
					MigrationID: definition.ID, MigrationRevision: definition.Revision, Kind: kind, SourceJobID: source,
				})
				must(t, err)
				change, err = service.Decide(ctx, f.leader, change.ID, change.Revision, f.group, migration.DecisionApprove, "")
				must(t, err)
				job, err := service.Enqueue(ctx, f.requester, change.ID, change.Revision)
				must(t, err)
				return job
			}
			original := queue(migration.ChangeApply, "")
			f.process(t)
			if f.job(t, original.ID).Status != migration.JobPartial {
				t.Fatal("expected partial migration before recovery")
			}
			exec(t, f.target, "DELETE FROM public.fail_on")
			recovery := queue(kind, original.ID)
			if (recovery.ExecutionID == original.ExecutionID) != (kind == migration.ChangeResume) {
				t.Fatal("unexpected recovery receipt identity")
			}
			f.process(t)
			if f.job(t, recovery.ID).Status != migration.JobSucceeded {
				t.Fatal("recovery did not complete")
			}
			changed, _ := f.shifted(t)
			wantRows := int64(0)
			wantStatements := 2
			if kind == migration.ChangeResume {
				wantRows, wantStatements = 350, 4
			}
			if changed != wantRows {
				t.Fatalf("changed rows = %d, want %d", changed, wantRows)
			}
			statements, err := f.store.ListExecutedStatements(ctx, f.database, recovery.ExecutionID, migration.PageQuery{Page: 1, Limit: 2})
			must(t, err)
			if len(statements.Items) != 2 || statements.HasNext != (wantStatements > 2) || statements.Items[0].Batch == nil {
				t.Fatal("batch history was not paginated by checkpoint")
			}
			// Restart-time repair must be idempotent, including statements.
			var count int
			must(t, f.platform.QueryRow(ctx, "SELECT count(*) FROM batch_statements b JOIN jobs j ON j.id=b.job_id WHERE j.execution_id=$1", string(recovery.ExecutionID)).Scan(&count))
			if count < wantStatements {
				t.Fatalf("stored statements=%d want at least %d", count, wantStatements)
			}
		})
	}
}
