package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/worker"
)

func TestAttemptHistoryFailedSQLIsImmutableAndDoesNotMovePosition(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	sql := "CREATE TABLE public.attempt_failure(id int PRIMARY KEY); INSERT INTO public.attempt_failure VALUES (1),(1);"
	_, job := f.approvedJob(t, "", sql, "DROP TABLE public.attempt_failure;", migration.ChangeApply, "")
	f.process(t)
	if f.job(t, job.ID).Status != migration.JobFailed {
		t.Fatal("expected failed job")
	}
	if f.tableExists(t, "attempt_failure") {
		t.Fatal("failed transaction changed target")
	}
	for _, hide := range []bool{false, true} {
		page, err := f.store.ListHistory(ctx, f.database, migration.ExecutionQuery{PageQuery: migration.PageQuery{Page: 1, Limit: 25}, HideRollback: hide})
		must(t, err)
		if len(page.Items) != 1 {
			t.Fatalf("history entries=%d hide=%v", len(page.Items), hide)
		}
		entry := page.Items[0]
		if entry.Status != migration.JobFailed || entry.CommittedAt != nil || entry.PositionVersion != nil || !entry.SnapshotAvailable || entry.Definition != nil {
			t.Fatalf("invalid list projection: %+v", entry)
		}
		detail, err := f.store.FindHistory(ctx, f.database, entry.ID)
		must(t, err)
		if detail.Definition == nil || detail.Definition.UpSQL.Reveal() != sql {
			t.Fatal("attempt snapshot changed")
		}
		statements, err := f.store.HistoryStatements(ctx, f.database, entry.ID, migration.PageQuery{Page: 1, Limit: 25})
		must(t, err)
		if len(statements.Items) != 1 || statements.Items[0].Query.Reveal() != sql || statements.Items[0].Status != "failed" || statements.Items[0].SQLState != "23505" {
			t.Fatalf("missing failed SQL: %+v", statements)
		}
		_, err = f.store.FindHistory(ctx, newID(t), entry.ID)
		if !errors.Is(err, security.ErrNotFound) {
			t.Fatalf("cross-database read: %v", err)
		}
		if _, err = f.platform.Exec(ctx, "UPDATE job_attempts SET definition_snapshot='{}'::jsonb WHERE history_id=$1", string(entry.ID)); err == nil {
			t.Fatal("mutable attempt snapshot")
		}
		if _, err = f.platform.Exec(ctx, "DELETE FROM attempt_statements WHERE history_id=$1", string(entry.ID)); err == nil {
			t.Fatal("mutable statement journal")
		}
	}
	committed, err := f.store.ListExecutions(ctx, f.database, migration.ExecutionQuery{PageQuery: migration.PageQuery{Page: 1, Limit: 25}})
	must(t, err)
	if len(committed.Items) != 0 {
		t.Fatal("failed attempt entered committed execution stream")
	}
}

func TestAttemptHistoryHideRollbackKeepsFailuresAndForward(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	_, a := f.approvedJob(t, "", "CREATE TABLE public.history_a(id int);", "DROP TABLE public.history_a;", migration.ChangeApply, "")
	f.process(t)
	mid, b := f.approvedJob(t, "", "CREATE TABLE public.history_b(id int);", "DROP TABLE public.history_b;", migration.ChangeApply, "")
	f.process(t)
	_, rollback := f.approvedJob(t, mid, "", "", migration.ChangeRollback, b.ExecutionID)
	f.process(t)
	_, forward := f.approvedJob(t, mid, "", "", migration.ChangeForward, b.ExecutionID)
	f.process(t)
	_, failed := f.approvedJob(t, "", "SELECT 1/0;", "SELECT 1;", migration.ChangeApply, "")
	f.process(t)
	page, err := f.store.ListHistory(ctx, f.database, migration.ExecutionQuery{PageQuery: migration.PageQuery{Page: 1, Limit: 25}, HideRollback: true})
	must(t, err)
	if len(page.Items) != 3 {
		t.Fatalf("visible entries=%d, want 3", len(page.Items))
	}
	seen := map[security.ID]bool{}
	for _, e := range page.Items {
		seen[e.ExecutionID] = true
	}
	if !seen[a.ExecutionID] || !seen[forward.ExecutionID] || !seen[failed.ExecutionID] || seen[b.ExecutionID] || seen[rollback.ExecutionID] {
		t.Fatalf("wrong execution pairing: %v", seen)
	}
	paged, err := f.store.ListHistory(ctx, f.database, migration.ExecutionQuery{PageQuery: migration.PageQuery{Page: 1, Limit: 2}, HideRollback: true})
	must(t, err)
	if len(paged.Items) != 2 || !paged.HasNext {
		t.Fatal("filter must precede pagination")
	}
}

func TestFailedNormalRetryRequiresFreshApprovalAndPreservesHistory(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	clock := security.SystemClock{}
	accounts, err := NewStore(f.platform, clock)
	must(t, err)
	auth, err := security.NewBackendAuthorizer(accounts, accounts, clock, security.DefaultSessionPolicy())
	must(t, err)
	service, err := migration.NewWorkflowService(migration.WorkflowConfig{Store: f.store, Authorizer: auth, Engines: recoveryTestEngine{}, IDs: security.NewTokenGenerator(), Clock: clock})
	must(t, err)
	revalidator, err := migration.NewRevalidator(f.store, clock)
	must(t, err)
	f.worker, err = worker.New(worker.Config{Store: f.store, Coordinator: f.targets, Revalidator: revalidator, Runner: f.runner, Progress: f.store, Clock: clock, Owner: "history-retry-test"})
	must(t, err)
	definition, err := service.CreateDefinition(ctx, f.requester, migration.DefinitionInput{Title: "Retry immutable SQL", UpSQL: security.NewSecret("INSERT INTO public.users(id,email,note) VALUES (1,'retry@example.test','ok');"), DownSQL: security.NewSecret("DELETE FROM public.users WHERE id=1;"), Mode: migration.ModeNormal})
	must(t, err)
	change, err := service.Submit(ctx, f.requester, migration.SubmitChange{MigrationID: definition.ID, MigrationRevision: definition.Revision, Kind: migration.ChangeApply})
	must(t, err)
	change, err = service.Decide(ctx, f.leader, change.ID, change.Revision, f.group, migration.DecisionApprove, "")
	must(t, err)
	original, err := service.Enqueue(ctx, f.requester, change.ID, change.Revision)
	must(t, err)
	f.process(t)
	retry, err := service.RetryFailedJob(ctx, f.requester, original.ID)
	must(t, err)
	if retry.ID == change.ID || retry.RetryOfJobID != original.ID || retry.Status != migration.CRPendingReview {
		t.Fatalf("retry must create fresh review: %+v", retry)
	}
	if _, err := service.Enqueue(ctx, f.requester, retry.ID, retry.Revision); !errors.Is(err, security.ErrConflict) {
		t.Fatalf("approval bypass: %v", err)
	}
	if _, err := service.RetryFailedJob(ctx, f.requester, original.ID); !errors.Is(err, security.ErrConflict) {
		t.Fatalf("duplicate retry: %v", err)
	}
	retry, err = service.Decide(ctx, f.leader, retry.ID, retry.Revision, f.group, migration.DecisionApprove, "")
	must(t, err)
	replacement, err := service.Enqueue(ctx, f.requester, retry.ID, retry.Revision)
	must(t, err)
	if replacement.ID == original.ID || replacement.ExecutionID == original.ExecutionID {
		t.Fatal("retry reused an old execution identity")
	}
	// Fix only the external precondition, never the frozen SQL definition.
	exec(t, f.target, "DELETE FROM public.users WHERE id=1")
	f.process(t)
	page, err := f.store.ListHistory(ctx, f.database, migration.ExecutionQuery{PageQuery: migration.PageQuery{Page: 1, Limit: 25}})
	must(t, err)
	if len(page.Items) != 2 {
		t.Fatalf("failed attempts overwritten: %d", len(page.Items))
	}
	statuses := map[migration.JobStatus]int{}
	for _, entry := range page.Items {
		statuses[entry.Status]++
		if entry.Status == migration.JobFailed && entry.PositionVersion != nil {
			t.Fatal("failed source acquired a commit position")
		}
	}
	if statuses[migration.JobFailed] != 1 || statuses[migration.JobSucceeded] != 1 || f.job(t, replacement.ID).Status != migration.JobSucceeded {
		t.Fatal("replacement did not succeed independently of the failed source")
	}
}

func TestAttemptHistoryReconciliationAppendsWithoutRewritingUncertain(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	_, job := f.approvedJob(t, "", "CREATE TABLE public.history_uncertain(id int);", "DROP TABLE public.history_uncertain;", migration.ChangeApply, "")
	lease, err := f.store.ClaimJob(ctx, "lost-commit-response", time.Now(), time.Minute)
	must(t, err)
	must(t, f.targets.WithDatabaseLock(ctx, f.database, func(locked context.Context, target migration.LockedTarget) error {
		plan, err := f.revalidator.Revalidate(locked, lease.Job)
		if err != nil {
			return err
		}
		if err = f.store.StartRunning(locked, lease, plan); err != nil {
			return err
		}
		result, err := f.runner.Run(migration.WithStatementRecorder(locked, f.store, lease), plan, target, nil)
		if err != nil {
			return err
		}
		if result.Status != migration.JobSucceeded {
			t.Fatal("target commit failed")
		}
		return nil
	}))
	must(t, f.store.RecordOutcome(ctx, lease, migration.RunResult{Status: migration.JobUncertain, Error: &migration.JobFailure{Code: migration.JobErrorUncertain, Message: "commit outcome unknown"}}))
	before, err := f.store.ListHistory(ctx, f.database, migration.ExecutionQuery{})
	must(t, err)
	if len(before.Items) != 1 || before.Items[0].Status != migration.JobUncertain || before.Items[0].CommittedAt != nil {
		t.Fatal("unknown outcome was treated as a commit")
	}
	if f.worker.RecoverOnce(ctx) != 1 {
		t.Fatal("uncertain receipt was not reconciled")
	}
	if f.job(t, job.ID).Status != migration.JobSucceeded {
		t.Fatal("receipt was not projected to the job")
	}
	after, err := f.store.ListHistory(ctx, f.database, migration.ExecutionQuery{})
	must(t, err)
	if len(after.Items) != 1 || after.Items[0].ID != before.Items[0].ID || after.Items[0].Status != migration.JobUncertain || after.Items[0].ResolvedStatus != migration.JobSucceeded || after.Items[0].CommittedAt == nil {
		t.Fatalf("reconciliation rewrote or lost attempt: %+v", after.Items)
	}
	var count int
	must(t, f.platform.QueryRow(ctx, "SELECT count(*) FROM attempt_resolutions WHERE history_id=$1", string(after.Items[0].ID)).Scan(&count))
	if count != 1 {
		t.Fatal("resolution was not appended")
	}
}
