package postgres

import (
	"context"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
)

func TestAttemptHistoryBatchKeepsFailedDispatchAfterPartialCommit(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	seedItems(t, f.target, 350)
	exec(t, f.target, "INSERT INTO public.fail_on VALUES (250)")
	mid := f.batchDefinition(t, "amount = amount + 1", "amount = amount - 1", "id", 100)
	source, err := f.change(t, mid, migration.ChangeApply, "", "", "")
	must(t, err)
	f.process(t)
	page, err := f.store.ListHistory(ctx, f.database, migration.ExecutionQuery{})
	must(t, err)
	if len(page.Items) != 1 || page.Items[0].Status != migration.JobPartial || page.Items[0].CanRetry || page.Items[0].CommittedAt != nil {
		t.Fatal("partial attempt incorrectly treated as a full commit or normal retry")
	}
	detail, err := f.store.FindHistory(ctx, f.database, page.Items[0].ID)
	must(t, err)
	if detail.Definition == nil || detail.Definition.Batch == nil || detail.Definition.Batch.BatchSize != 100 {
		t.Fatal("batch snapshot was not preserved")
	}
	statements, err := f.store.HistoryStatements(ctx, f.database, detail.ID, migration.PageQuery{Page: 1, Limit: 2})
	must(t, err)
	if len(statements.Items) != 2 || !statements.HasNext || statements.Items[0].Status != "succeeded" || statements.Items[1].Status != "succeeded" {
		t.Fatal("committed batch dispatches missing")
	}
	failed, err := f.store.HistoryStatements(ctx, f.database, detail.ID, migration.PageQuery{Page: 2, Limit: 2})
	must(t, err)
	if len(failed.Items) != 1 || failed.Items[0].Status != "failed" || failed.Items[0].SQLState != "22012" || failed.Items[0].Batch == nil || *failed.Items[0].Batch != 3 {
		t.Fatalf("failed batch dispatch missing: %+v", failed.Items)
	}
	exec(t, f.target, "DELETE FROM public.fail_on")
	recovery, err := f.change(t, mid, migration.ChangeResume, "", source.ID, source.ExecutionID)
	must(t, err)
	f.process(t)
	if f.job(t, recovery.ID).Status != migration.JobSucceeded {
		t.Fatal("batch resume failed")
	}
	page, err = f.store.ListHistory(ctx, f.database, migration.ExecutionQuery{})
	must(t, err)
	if len(page.Items) != 2 {
		t.Fatal("batch recovery overwrote original partial attempt")
	}
	for _, entry := range page.Items {
		if entry.JobID == source.ID && (entry.Status != migration.JobPartial || entry.CommittedAt != nil) {
			t.Fatal("shared receipt promoted partial source into commit")
		}
	}
}
