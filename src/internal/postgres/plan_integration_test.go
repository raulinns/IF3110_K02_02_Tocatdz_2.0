package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestPlansUseRealPostgreSQLAndLeaveNoChanges(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()
	exec(t, f.target, "CREATE TABLE public.plan_items(id integer PRIMARY KEY, label text); INSERT INTO public.plan_items SELECT i, 'item' FROM generate_series(1,1000) AS i; ANALYZE public.plan_items;")
	pool, err := f.targets.pool(ctx, f.database)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, f.target, "ALTER TABLE public.plan_items OWNER TO "+pgx.Identifier{pool.Config().ConnConfig.User}.Sanitize())
	id := f.draft(t, "Plan", "-- é;\nSELECT * FROM public.plan_items WHERE id=2; SELECT i.id FROM public.plan_items i JOIN public.users j ON j.id=i.id WHERE i.id<4;")
	options := migration.PlanOptions{Revision: 1, Statement: 1}
	plan, err := f.service.Analyze(ctx, f.request, id, options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Plan.Index != "plan_items_pkey" || plan.ExecutionMS != nil {
		t.Fatalf("expected index estimate: %+v", plan.Plan)
	}
	options.Analyze = true
	options.Statement = 2
	plan, err = f.service.Analyze(ctx, f.request, id, options)
	if err != nil || plan.ExecutionMS == nil {
		t.Fatalf("ANALYZE failed: %v", err)
	}
	joined := false
	var walk func(migration.PlanNode)
	walk = func(n migration.PlanNode) {
		if n.Join != "" {
			joined = true
		}
		for _, child := range n.Plans {
			walk(child)
		}
	}
	walk(plan.Plan)
	if !joined || plan.Plan.ActualRows == nil || *plan.Plan.ActualRows != 3 {
		t.Fatalf("join metrics missing: %+v", plan.Plan)
	}
	record, _ := f.ledger.Find(ctx, f.database, id)
	if record.Status != migration.DraftStatus || record.Revision != 1 || record.Version != 0 {
		t.Fatal("analysis changed draft")
	}
	dml := f.draft(t, "Mutation", "UPDATE public.plan_items SET label='changed' WHERE id=2;")
	if _, err := f.service.Analyze(ctx, f.request, dml, migration.PlanOptions{Revision: 1, Statement: 1}); err != nil {
		t.Fatal("DML estimate failed", err)
	}
	if _, err := f.service.Analyze(ctx, f.request, dml, migration.PlanOptions{Revision: 1, Statement: 1, Analyze: true}); !errors.Is(err, security.ErrInvalidInput) {
		t.Fatal("DML ANALYZE accepted")
	}
	var label string
	var receipts int
	if err := f.target.QueryRow(ctx, "SELECT label FROM public.plan_items WHERE id=2").Scan(&label); err != nil || label != "item" {
		t.Fatal("plan changed target data")
	}
	if err := f.target.QueryRow(ctx, "SELECT count(*) FROM tocatdz_internal.migration_receipts").Scan(&receipts); err != nil || receipts != 0 {
		t.Fatal("analysis wrote receipts")
	}
}

func TestPlanReadOnlyGuardsViewsAndRecoversAfterErrors(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()
	exec(t, f.target, "CREATE SEQUENCE public.plan_sequence; CREATE VIEW public.sequence_view AS SELECT nextval('public.plan_sequence') AS id;")
	pool, err := f.targets.pool(ctx, f.database)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, f.target, "ALTER SEQUENCE public.plan_sequence OWNER TO "+pgx.Identifier{pool.Config().ConnConfig.User}.Sanitize())
	exec(t, f.target, "ALTER VIEW public.sequence_view OWNER TO "+pgx.Identifier{pool.Config().ConnConfig.User}.Sanitize())
	id := f.draft(t, "Hidden write", "SELECT * FROM public.sequence_view;")
	_, err = f.service.Analyze(ctx, f.request, id, migration.PlanOptions{Revision: 1, Statement: 1, Analyze: true})
	var reason migration.PlanRejection
	if !errors.As(err, &reason) || reason != "analyze" {
		t.Fatalf("hidden mutation not blocked by read-only transaction: %v", err)
	}
	var called bool
	if err := f.target.QueryRow(ctx, "SELECT is_called FROM public.plan_sequence").Scan(&called); err != nil || called {
		t.Fatal("ANALYZE advanced sequence")
	}
	broken := f.draft(t, "Missing relation", "SELECT * FROM public.plan_missing;")
	if _, err := f.service.Analyze(ctx, f.request, broken, migration.PlanOptions{Revision: 1, Statement: 1}); err == nil {
		t.Fatal("missing table returned a plan")
	}
	ok := f.draft(t, "Normal execute", "CREATE TABLE public.after_plan(id integer);")
	result, err := f.service.Execute(ctx, f.request, ok, 1)
	if err != nil || result.Status != migration.Applied || result.Version != 1 {
		t.Fatal("analysis leaked transaction settings or reserved version", err)
	}
}

func TestPlanLockTimeoutReleasesConnection(t *testing.T) {
	f := newMigrationFixture(t)
	ctx := context.Background()
	blocker, err := f.target.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err := blocker.Exec(ctx, "LOCK TABLE public.users IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	id := f.draft(t, "Wait for table lock", "SELECT * FROM public.users;")
	_, err = f.service.Analyze(ctx, f.request, id, migration.PlanOptions{Revision: 1, Statement: 1})
	var reason migration.PlanRejection
	if !errors.As(err, &reason) || reason != "timeout" {
		t.Fatalf("lock timeout not handled safely: %v", err)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Analyze(ctx, f.request, id, migration.PlanOptions{Revision: 1, Statement: 1}); err != nil {
		t.Fatal("analysis did not recover after lock release", err)
	}
}
