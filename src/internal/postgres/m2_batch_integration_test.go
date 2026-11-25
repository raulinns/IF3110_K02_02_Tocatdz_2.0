package postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/targetbatch"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/worker"
)

func runtimeRole(t *testing.T, target *pgxpool.Pool) string {
	t.Helper()
	var oid uint32
	must(t, target.QueryRow(context.Background(), "SELECT oid FROM pg_database WHERE datname = current_database()").Scan(&oid))
	return "tocatdz_exec_" + strconv.FormatUint(uint64(oid), 10)
}

func seedItems(t *testing.T, target *pgxpool.Pool, rows int) {
	t.Helper()
	role := runtimeRole(t, target)
	for _, statement := range []string{
		"CREATE TABLE public.items (id bigint PRIMARY KEY, amount bigint NOT NULL, grp int NOT NULL)",
		"INSERT INTO public.items SELECT g, g, g % 7 FROM generate_series(1, " + strconv.Itoa(rows) + ") g",
		"CREATE TABLE public.fail_on (id bigint PRIMARY KEY)",
		`CREATE FUNCTION public.fail_check() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF EXISTS (SELECT 1 FROM public.fail_on WHERE id = NEW.id) THEN RAISE EXCEPTION 'injected failure' USING ERRCODE = '22012'; END IF;
		RETURN NEW; END $$`,
		"CREATE TRIGGER items_fail BEFORE UPDATE ON public.items FOR EACH ROW EXECUTE FUNCTION public.fail_check()",
		`ALTER TABLE public.items OWNER TO "` + role + `"`,
		`ALTER TABLE public.fail_on OWNER TO "` + role + `"`,
	} {
		exec(t, target, statement)
	}
}

func (f *workerFixture) shifted(t *testing.T) (changed int64, total int64) {
	t.Helper()
	must(t, f.target.QueryRow(context.Background(), "SELECT count(*) FILTER (WHERE amount <> id), COALESCE(sum(amount - id), 0) FROM public.items").Scan(&changed, &total))
	return changed, total
}

func (f *workerFixture) batchDefinition(t *testing.T, up, down, key string, size int) security.ID {
	t.Helper()
	f.sum++
	id := newID(t)
	now := time.Now().UTC()
	input := migration.DefinitionInput{Title: "batch", UpSQL: security.NewSecret(up), DownSQL: security.NewSecret(down), Mode: migration.ModeBatch,
		Batch: &migration.BatchConfig{Schema: "public", Table: "items", KeyColumn: key, BatchSize: size}}
	must(t, f.store.WithWorkflow(context.Background(), f.requester, func(tx migration.WorkflowTx) error {
		return tx.CreateDefinition(migration.MigrationDefinition{ID: id, DatabaseID: f.database, Revision: 1, Checksum: checksum(f.sum),
			CreatedBy: f.requester.Principal.UserID, CreatedAt: now, UpdatedAt: now, DefinitionInput: input})
	}))
	return id
}

func (f *workerFixture) change(t *testing.T, migrationID security.ID, kind migration.ChangeKind, target, source, executionID security.ID) (migration.JobView, error) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	definition, err := f.store.FindDefinition(ctx, f.database, migrationID)
	must(t, err)
	cr := newID(t)
	err = f.store.WithWorkflow(ctx, f.requester, func(tx migration.WorkflowTx) error {
		approvers, err := tx.RequiredApprovers()
		if err != nil {
			return err
		}
		position, err := tx.Position()
		if err != nil {
			return err
		}
		approvals := make([]migration.GroupApproval, len(approvers))
		for i, approver := range approvers {
			approvals[i] = migration.GroupApproval{RequiredApproval: approver}
		}
		return tx.CreateChangeRequest(migration.ChangeRequest{ID: cr, DatabaseID: f.database, Approvals: approvals, Checksum: definition.Checksum,
			SubmitChange: migration.SubmitChange{MigrationID: migrationID, MigrationRevision: definition.Revision, Kind: kind,
				TargetExecutionID: target, SourceJobID: source},
			RequestedBy: f.requester.Principal.UserID, ExpectedPosition: position.Version, CreatedAt: now})
	})
	if err != nil {
		return migration.JobView{}, err
	}
	must(t, f.store.WithWorkflow(ctx, f.leader, func(tx migration.WorkflowTx) error {
		_, err := tx.RecordDecision(cr, 1, migration.ApprovalDecision{GroupID: f.group, ActorID: f.leader.Principal.UserID,
			Decision: migration.DecisionApprove, DecidedAt: now})
		return err
	}))
	if executionID == "" {
		executionID = newID(t)
	}
	var job migration.JobView
	must(t, f.store.WithWorkflow(ctx, f.requester, func(tx migration.WorkflowTx) error {
		var err error
		job, err = tx.EnqueueApproved(cr, 2, migration.JobSpec{ID: newID(t), ExecutionID: executionID, MaxAttempts: 2, QueuedAt: now})
		return err
	}))
	return job, nil
}

func TestM2BatchMigrationCommitsEveryBatch(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	seedItems(t, f.target, 1050)
	migrationID := f.batchDefinition(t, "amount = amount + 1", "amount = amount - 1", "id", 100)
	job, err := f.change(t, migrationID, migration.ChangeApply, "", "", "")
	must(t, err)
	f.process(t)

	done := f.job(t, job.ID)
	if done.Status != migration.JobSucceeded || done.Progress.BatchesCommitted != 11 || done.Progress.RowsCommitted != 1050 ||
		done.ScopeMax == nil || *done.ScopeMax != "1050" {
		t.Fatalf("batch job = %+v progress %+v", done.Status, done.Progress)
	}
	if changed, total := f.shifted(t); changed != 1050 || total != 1050 {
		t.Fatalf("changed rows = %d, total shift = %d", changed, total)
	}
	page, err := f.store.Checkpoints(ctx, f.database, job.ExecutionID, migration.CheckpointQuery{Limit: 3, Descending: true})
	must(t, err)
	if len(page.Items) != 3 || !page.HasNext || page.Items[0].Batch != 11 || page.Items[0].Rows != 50 || page.Items[0].To != "1050" {
		t.Fatalf("checkpoint mirror = %+v", page)
	}
	execution, err := f.store.FindExecution(ctx, f.database, job.ExecutionID)
	must(t, err)
	if execution.Definition.Mode != migration.ModeBatch || execution.Definition.Batch.BatchSize != 100 {
		t.Fatalf("history snapshot = %+v", execution.Definition)
	}

	rollback, err := f.change(t, migrationID, migration.ChangeRollback, job.ExecutionID, "", "")
	must(t, err)
	f.process(t)
	if got := f.job(t, rollback.ID); got.Status != migration.JobSucceeded || got.Progress.BatchesReverted != 11 {
		t.Fatalf("rollback job = %s progress %+v", got.Status, got.Progress)
	}
	if changed, _ := f.shifted(t); changed != 0 {
		t.Fatalf("rows still changed after rollback = %d", changed)
	}
}

func TestM2BatchFailureIsPartialAndResumeSkipsCommittedBatches(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	seedItems(t, f.target, 1050)
	exec(t, f.target, "INSERT INTO public.fail_on VALUES (550)")
	migrationID := f.batchDefinition(t, "amount = amount + 1", "amount = amount - 1", "id", 100)
	job, err := f.change(t, migrationID, migration.ChangeApply, "", "", "")
	must(t, err)
	f.process(t)

	partial := f.job(t, job.ID)
	if partial.Status != migration.JobPartial || !partial.Partial || partial.Progress.BatchesCommitted != 5 || partial.Error == nil || partial.Error.SQLState != "22012" {
		t.Fatalf("failed batch job = %+v", partial)
	}
	if changed, total := f.shifted(t); changed != 500 || total != 500 {
		t.Fatalf("after failure: changed %d, total %d", changed, total)
	}
	position, err := f.store.ReadPosition(ctx, f.database)
	must(t, err)
	if position.BlockedByJobID != job.ID || len(position.Active) != 0 {
		t.Fatalf("position = %+v", position)
	}
	other := f.batchDefinition(t, "grp = grp", "grp = grp", "id", 100)
	if _, err := f.change(t, other, migration.ChangeApply, "", "", ""); !errors.Is(err, security.ErrConflict) {
		t.Fatalf("change behind barrier error = %v, want conflict", err)
	}

	exec(t, f.target, "DELETE FROM public.fail_on")
	resume, err := f.change(t, migrationID, migration.ChangeResume, "", job.ID, job.ExecutionID)
	must(t, err)
	f.process(t)
	if got := f.job(t, resume.ID); got.Status != migration.JobSucceeded {
		t.Fatalf("resume job = %+v", got)
	}
	if changed, total := f.shifted(t); changed != 1050 || total != 1050 {
		t.Fatalf("after resume: changed %d, total %d", changed, total)
	}
	position, err = f.store.ReadPosition(ctx, f.database)
	must(t, err)
	if position.BlockedByJobID != "" || len(position.Active) != 1 || position.Active[0].ID != job.ExecutionID || position.Active[0].Operation != migration.ChangeResume {
		t.Fatalf("position after resume = %+v", position)
	}
}

func TestM2BatchRevertCompensatesInReverseOrder(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	seedItems(t, f.target, 1050)
	exec(t, f.target, "INSERT INTO public.fail_on VALUES (750)")
	migrationID := f.batchDefinition(t, "amount = amount + 1", "amount = amount - 1", "id", 100)
	job, err := f.change(t, migrationID, migration.ChangeApply, "", "", "")
	must(t, err)
	f.process(t)
	if got := f.job(t, job.ID); got.Status != migration.JobPartial || got.Progress.BatchesCommitted != 7 {
		t.Fatalf("partial job = %+v", got)
	}

	revert, err := f.change(t, migrationID, migration.ChangeRevertBatches, "", job.ID, "")
	must(t, err)
	f.process(t)
	got := f.job(t, revert.ID)
	if got.Status != migration.JobSucceeded || got.Progress.BatchesReverted != 7 || got.Progress.RowsReverted != 700 {
		t.Fatalf("revert job = %+v progress %+v", got.Status, got.Progress)
	}
	if changed, _ := f.shifted(t); changed != 0 {
		t.Fatalf("rows still changed after revert = %d", changed)
	}
	var order []int64
	rows, err := f.target.Query(ctx, "SELECT batch_no FROM tocatdz_internal.batch_checkpoints WHERE direction = 'down' ORDER BY recorded_at, batch_no DESC")
	must(t, err)
	for rows.Next() {
		var batch int64
		must(t, rows.Scan(&batch))
		order = append(order, batch)
	}
	rows.Close()
	if len(order) != 7 || order[0] != 7 || order[6] != 1 {
		t.Fatalf("revert order = %v", order)
	}
	position, err := f.store.ReadPosition(ctx, f.database)
	must(t, err)
	if position.BlockedByJobID != "" || len(position.Active) != 0 {
		t.Fatalf("position after revert = %+v", position)
	}
	history, err := f.store.FindExecution(ctx, f.database, revert.ExecutionID)
	must(t, err)
	if history.Operation != migration.ChangeRevertBatches || history.Direction != migration.DirectionDown || history.RevertsExecutionID != "" {
		t.Fatalf("revert history = %+v", history)
	}
	mirror, err := f.store.Checkpoints(ctx, f.database, job.ExecutionID, migration.CheckpointQuery{})
	must(t, err)
	for _, checkpoint := range mirror.Items {
		if checkpoint.Rows > 0 && checkpoint.RevertedAt == nil {
			t.Fatalf("mirrored batch %d not marked reverted", checkpoint.Batch)
		}
	}
}

func TestM2BatchRejectsUnsafeConfiguration(t *testing.T) {
	f := newWorkerFixture(t)
	seedItems(t, f.target, 300)
	for _, tc := range []struct{ up, key string }{
		{"amount = amount + 1", "grp"},
		{"id = id + 1000", "id"},
		{"amount = amount + 1", "nope"},
	} {
		migrationID := f.batchDefinition(t, tc.up, "amount = amount - 1", tc.key, 100)
		job, err := f.change(t, migrationID, migration.ChangeApply, "", "", "")
		must(t, err)
		f.process(t)
		got := f.job(t, job.ID)
		if got.Status != migration.JobFailed || got.Error == nil || got.Error.Retryable {
			t.Fatalf("unsafe batch %+v -> %+v", tc, got)
		}
	}
	if changed, _ := f.shifted(t); changed != 0 {
		t.Fatalf("rejected batch changed %d rows", changed)
	}
}

func TestM2BatchCheckpointsCannotBeForged(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	must(t, f.targets.WithDatabaseLock(ctx, f.database, func(locked context.Context, target migration.LockedTarget) error {
		bt := target.(targetbatch.Target)
		tx, err := bt.BeginBatch(locked)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		if _, err := tx.UpdateKeys(locked, "INSERT INTO tocatdz_internal.batch_checkpoints VALUES (gen_random_uuid(),'up',1,NULL,1,1,1,'{1}',true,now()) RETURNING 1"); err == nil {
			t.Fatal("runtime role wrote checkpoints directly")
		}
		return nil
	}))
	must(t, f.targets.WithDatabaseLock(ctx, f.database, func(locked context.Context, target migration.LockedTarget) error {
		tx, err := target.(targetbatch.Target).BeginBatch(locked)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.Background())
		if _, err := tx.UpdateKeys(locked, "SELECT 1::bigint FROM tocatdz_internal.write_checkpoint(gen_random_uuid(),'up',1,NULL,1,1,1,'{1}',true,'\\x00')"); err == nil {
			t.Fatal("checkpoint with a forged proof was accepted")
		}
		return nil
	}))
}

func TestM2WorkerRunsDifferentDatabasesInParallel(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	second := addTarget(t, f.targetFixtureEnv, "gudang")
	exec(t, f.platform, "INSERT INTO group_database_grants (group_id, database_id, access) VALUES ($1, $2, 'view_edit')", string(f.group), string(second))
	first := f.database
	jobs := map[security.ID]security.ID{}
	for _, database := range []security.ID{first, second} {
		f.database, f.requester.DatabaseID, f.leader.DatabaseID = database, database, database
		_, job := f.approvedJob(t, "", "SELECT pg_sleep(1); CREATE TABLE parallel_done (id int)", "DROP TABLE parallel_done", migration.ChangeApply, "")
		jobs[database] = job.ID
	}
	w, err := workerWithConcurrency(f, 2)
	must(t, err)
	runCtx, cancel := context.WithCancel(ctx)
	started := time.Now()
	finished := make(chan struct{})
	go func() { w.Run(runCtx); close(finished) }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		done := 0
		for database, id := range jobs {
			job, err := f.store.FindJob(ctx, database, id)
			must(t, err)
			if job.Status == migration.JobSucceeded {
				done++
			}
		}
		if done == len(jobs) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	elapsed := time.Since(started)
	cancel()
	<-finished
	if elapsed >= 1900*time.Millisecond {
		t.Fatalf("two databases took %v; they did not run in parallel", elapsed)
	}
}

// Benchmark opsional: TEST_LARGE_BATCH_ROWS=500000.
func TestM2BatchLargeDatasetBenchmark(t *testing.T) {
	raw := os.Getenv("TEST_LARGE_BATCH_ROWS")
	if raw == "" {
		t.Skip("TEST_LARGE_BATCH_ROWS is not set")
	}
	rows, err := strconv.Atoi(raw)
	must(t, err)
	f := newWorkerFixture(t)
	seedItems(t, f.target, rows)
	var full time.Duration
	tx, err := f.target.Begin(context.Background())
	must(t, err)
	exec(t, f.target, "ALTER TABLE public.items DISABLE TRIGGER items_fail")
	started := time.Now()
	_, err = tx.Exec(context.Background(), "UPDATE public.items SET amount = amount + 1")
	full = time.Since(started)
	must(t, err)
	must(t, tx.Rollback(context.Background()))
	migrationID := f.batchDefinition(t, "amount = amount + 1", "amount = amount - 1", "id", 10_000)
	job, err := f.change(t, migrationID, migration.ChangeApply, "", "", "")
	must(t, err)
	started = time.Now()
	f.process(t)
	batch := time.Since(started)
	got := f.job(t, job.ID)
	t.Logf("rows=%d full_update=%v batch_job=%v batches=%d rows_committed=%d", rows, full, batch, got.Progress.BatchesCommitted, got.Progress.RowsCommitted)
	if got.Status != migration.JobSucceeded || got.Progress.RowsCommitted != int64(rows) {
		t.Fatalf("large batch job = %+v", got)
	}
}

func addTarget(t *testing.T, env *targetFixtureEnv, name string) security.ID {
	t.Helper()
	ctx := context.Background()
	box, err := NewSecretBox(testKey(9))
	must(t, err)
	runtimeDSN, err := PrepareManagedTarget(ctx, env.platform, security.NewSecret(createDatabase(t)), box)
	must(t, err)
	id := newID(t)
	sealed, err := box.Seal(id, runtimeDSN)
	must(t, err)
	exec(t, env.platform, "INSERT INTO managed_databases (id, name, dsn_ciphertext) VALUES ($1, $2, $3)", string(id), name, sealed)
	return id
}

func workerWithConcurrency(f *workerFixture, concurrency int) (*worker.Worker, error) {
	return worker.New(worker.Config{Store: f.store, Coordinator: f.targets, Revalidator: f.revalidator, Runner: f.runner, Progress: f.store,
		Clock: security.SystemClock{}, Owner: "parallel", Concurrency: concurrency, LeaseDuration: 5 * time.Second,
		PollInterval: 20 * time.Millisecond, Backoff: func(int) time.Duration { return 0 }})
}
