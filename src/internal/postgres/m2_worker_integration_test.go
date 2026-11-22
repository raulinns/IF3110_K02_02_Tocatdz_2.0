package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/worker"
)

// snapshotRevalidator menggantikan Revalidator domain (DOM-05) selama test.
type snapshotRevalidator struct {
	reader migration.RevalidationReader
	fail   error
}

func (r snapshotRevalidator) Revalidate(ctx context.Context, job migration.JobView) (migration.ExecutionPlan, error) {
	if r.fail != nil {
		return migration.ExecutionPlan{}, r.fail
	}
	snapshot, err := r.reader.ReadRevalidation(ctx, job.DatabaseID, job.ID)
	if err != nil {
		return migration.ExecutionPlan{}, err
	}
	plan := migration.ExecutionPlan{JobID: job.ID, ChangeRequestID: job.ChangeRequestID, DatabaseID: job.DatabaseID,
		MigrationID: snapshot.Definition.ID, ReceiptExecutionID: job.ExecutionID, ActorID: snapshot.ChangeRequest.RequestedBy,
		Kind: snapshot.ChangeRequest.Kind, Direction: migration.DirectionUp, Definition: snapshot.Definition,
		ExpectedPosition: snapshot.ChangeRequest.ExpectedPosition, TargetExecutionID: snapshot.ChangeRequest.TargetExecutionID,
		SourceJobID: snapshot.ChangeRequest.SourceJobID, ScopeMax: job.ScopeMax}
	switch snapshot.ChangeRequest.Kind {
	case migration.ChangeRollback, migration.ChangeRevertBatches:
		plan.Direction = migration.DirectionDown
	case migration.ChangeResume:
		if snapshot.SourcePlan != nil {
			plan.Direction = snapshot.SourcePlan.Direction
		}
	}
	if snapshot.SourceJob != nil {
		plan.SourceExecutionID = snapshot.SourceJob.ExecutionID
		if plan.ScopeMax == nil {
			plan.ScopeMax = snapshot.SourceJob.ScopeMax
		}
	}
	return plan, nil
}

type workerFixture struct {
	*targetFixtureEnv
	store             *M2Store
	requester, leader migration.Request
	group             security.ID
	revalidator       *snapshotRevalidator
	worker            *worker.Worker
	runner            worker.ModeRunner
	sum               byte
}

func newWorkerFixture(t *testing.T) *workerFixture {
	t.Helper()
	env := newTargetFixture(t)
	store, err := NewM2Store(env.platform)
	must(t, err)
	requester, leader := insertUser(t, env.platform, "requester", false), insertUser(t, env.platform, "leader", false)
	group := newID(t)
	exec(t, env.platform, "INSERT INTO groups (id, name) VALUES ($1, 'team')", string(group))
	exec(t, env.platform, "INSERT INTO group_memberships (group_id, user_id) VALUES ($1, $2), ($1, $3)", string(group), string(requester), string(leader))
	exec(t, env.platform, "UPDATE groups SET leader_user_id = $2 WHERE id = $1", string(group), string(leader))
	exec(t, env.platform, "INSERT INTO group_database_grants (group_id, database_id, access) VALUES ($1, $2, 'view_edit')", string(group), string(env.database))
	request := func(user security.ID) migration.Request {
		return migration.Request{Principal: security.Principal{UserID: user, SessionID: liveSession(t, env.platform, user), AuthVersion: 1, CSRFEpoch: 1},
			DatabaseID: env.database}
	}
	f := &workerFixture{targetFixtureEnv: env, store: store, requester: request(requester), leader: request(leader), group: group}
	f.revalidator = &snapshotRevalidator{reader: store}
	f.runner, err = worker.NewModeRunner(security.SystemClock{})
	must(t, err)
	f.worker = f.newWorker(t, "worker-1")
	return f
}

func (f *workerFixture) newWorker(t *testing.T, owner string) *worker.Worker {
	t.Helper()
	w, err := worker.New(worker.Config{Store: f.store, Coordinator: f.targets, Revalidator: f.revalidator, Runner: f.runner, Progress: f.store,
		Clock: security.SystemClock{}, Owner: owner, LeaseDuration: 5 * time.Second,
		Backoff: func(int) time.Duration { return 0 }})
	must(t, err)
	return w
}

func (f *workerFixture) approvedJob(t *testing.T, migrationID security.ID, up, down string, kind migration.ChangeKind, target security.ID) (security.ID, migration.JobView) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	if migrationID == "" {
		f.sum++
		migrationID = newID(t)
		input := migration.DefinitionInput{Title: "m", UpSQL: security.NewSecret(up), Mode: migration.ModeNormal}
		if down == "" {
			input.Irreversible, input.IrreversibleReason = true, "test"
		} else {
			input.DownSQL = security.NewSecret(down)
		}
		must(t, f.store.WithWorkflow(ctx, f.requester, func(tx migration.WorkflowTx) error {
			return tx.CreateDefinition(migration.MigrationDefinition{ID: migrationID, DatabaseID: f.database, Revision: 1, Checksum: checksum(f.sum),
				CreatedBy: f.requester.Principal.UserID, CreatedAt: now, UpdatedAt: now, DefinitionInput: input})
		}))
	}
	definition, err := f.store.FindDefinition(ctx, f.database, migrationID)
	must(t, err)
	cr := newID(t)
	must(t, f.store.WithWorkflow(ctx, f.requester, func(tx migration.WorkflowTx) error {
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
			SubmitChange: migration.SubmitChange{MigrationID: migrationID, MigrationRevision: definition.Revision, Kind: kind, TargetExecutionID: target},
			RequestedBy:  f.requester.Principal.UserID, ExpectedPosition: position.Version, CreatedAt: now})
	}))
	must(t, f.store.WithWorkflow(ctx, f.leader, func(tx migration.WorkflowTx) error {
		_, err := tx.RecordDecision(cr, 1, migration.ApprovalDecision{GroupID: f.group, ActorID: f.leader.Principal.UserID,
			Decision: migration.DecisionApprove, DecidedAt: now})
		return err
	}))
	var job migration.JobView
	must(t, f.store.WithWorkflow(ctx, f.requester, func(tx migration.WorkflowTx) error {
		var err error
		job, err = tx.EnqueueApproved(cr, 2, migration.JobSpec{ID: newID(t), ExecutionID: newID(t), MaxAttempts: 2, QueuedAt: now})
		return err
	}))
	return migrationID, job
}

func (f *workerFixture) process(t *testing.T) {
	t.Helper()
	worked, err := f.worker.ProcessOne(context.Background())
	must(t, err)
	if !worked {
		t.Fatal("worker found no job")
	}
}

func (f *workerFixture) job(t *testing.T, id security.ID) migration.JobView {
	t.Helper()
	job, err := f.store.FindJob(context.Background(), f.database, id)
	must(t, err)
	return job
}

func (f *workerFixture) tableExists(t *testing.T, name string) bool {
	t.Helper()
	var exists bool
	must(t, f.target.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", "public."+name).Scan(&exists))
	return exists
}

func TestM2WorkerAppliesRollsBackAndForwards(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	migrationID, job := f.approvedJob(t, "", "CREATE TABLE m2_demo (id int)", "DROP TABLE m2_demo", migration.ChangeApply, "")
	f.process(t)

	done := f.job(t, job.ID)
	if done.Status != migration.JobSucceeded || done.FinishedAt == nil || !f.tableExists(t, "m2_demo") {
		t.Fatalf("apply job = %s, table exists = %v", done.Status, f.tableExists(t, "m2_demo"))
	}
	request, err := f.store.FindChangeRequest(ctx, f.database, done.ChangeRequestID)
	must(t, err)
	if request.Status != migration.CRSucceeded {
		t.Fatalf("CR status = %s", request.Status)
	}
	attempts, err := f.store.ListAttempts(ctx, f.database, job.ID, migration.PageQuery{})
	must(t, err)
	if len(attempts.Items) != 1 || attempts.Items[0].Status != migration.JobSucceeded {
		t.Fatalf("attempts = %+v", attempts.Items)
	}
	position, err := f.store.ReadPosition(ctx, f.database)
	must(t, err)
	if position.Version != 1 || len(position.Active) != 1 || position.Active[0].ID != job.ExecutionID {
		t.Fatalf("position after apply = %+v", position)
	}
	applied, err := f.store.FindExecution(ctx, f.database, job.ExecutionID)
	must(t, err)
	if applied.Actor != "requester" || applied.Statements[0].Query.Reveal() != "CREATE TABLE m2_demo (id int)" {
		t.Fatalf("history row = %+v", applied)
	}

	_, rollback := f.approvedJob(t, migrationID, "", "", migration.ChangeRollback, job.ExecutionID)
	f.process(t)
	if got := f.job(t, rollback.ID); got.Status != migration.JobSucceeded || f.tableExists(t, "m2_demo") {
		t.Fatalf("rollback job = %s, table exists = %v", got.Status, f.tableExists(t, "m2_demo"))
	}
	reverted, err := f.store.FindExecution(ctx, f.database, rollback.ExecutionID)
	must(t, err)
	if reverted.RevertsExecutionID != job.ExecutionID || reverted.Direction != migration.DirectionDown {
		t.Fatalf("rollback history = %+v", reverted)
	}
	position, err = f.store.ReadPosition(ctx, f.database)
	must(t, err)
	if len(position.Active) != 0 || len(position.Forward) != 1 || position.Forward[0].ID != job.ExecutionID {
		t.Fatalf("position after rollback = %+v", position)
	}

	_, forward := f.approvedJob(t, migrationID, "", "", migration.ChangeForward, job.ExecutionID)
	f.process(t)
	if got := f.job(t, forward.ID); got.Status != migration.JobSucceeded || !f.tableExists(t, "m2_demo") {
		t.Fatalf("forward job = %s", got.Status)
	}
	hidden, err := f.store.ListExecutions(ctx, f.database, migration.ExecutionQuery{HideRollback: true})
	must(t, err)
	if len(hidden.Items) != 1 || hidden.Items[0].ID != forward.ExecutionID {
		t.Fatalf("hide rollback = %+v", hidden.Items)
	}
}

func TestM2WorkerSQLFailureRollsBackWithoutHistory(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	_, job := f.approvedJob(t, "", "CREATE TABLE m2_ok (id int); SELECT * FROM missing_table", "DROP TABLE m2_ok", migration.ChangeApply, "")
	f.process(t)

	failed := f.job(t, job.ID)
	if failed.Status != migration.JobFailed || failed.Error == nil || failed.Error.SQLState != "42P01" || failed.Error.Retryable {
		t.Fatalf("failed job = %+v", failed)
	}
	if f.tableExists(t, "m2_ok") {
		t.Fatal("failed transaction left a partial change")
	}
	attempts, err := f.store.ListAttempts(ctx, f.database, job.ID, migration.PageQuery{})
	must(t, err)
	if len(attempts.Items) != 1 || attempts.Items[0].Status != migration.JobFailed || attempts.Items[0].Error.Code != migration.JobErrorSQL {
		t.Fatalf("failed attempt = %+v", attempts.Items)
	}
	history, err := f.store.ListExecutions(ctx, f.database, migration.ExecutionQuery{})
	must(t, err)
	position, err := f.store.ReadPosition(ctx, f.database)
	must(t, err)
	if len(history.Items) != 0 || position.Version != 0 {
		t.Fatalf("history = %d rows, position version %d", len(history.Items), position.Version)
	}
	f.approvedJob(t, "", "CREATE TABLE m2_next (id int)", "DROP TABLE m2_next", migration.ChangeApply, "")
	f.process(t)
	if !f.tableExists(t, "m2_next") {
		t.Fatal("next migration did not run after a failure")
	}
}

func TestM2WorkerRevalidationFailureDispatchesNoSQL(t *testing.T) {
	f := newWorkerFixture(t)
	f.revalidator.fail = security.ErrForbidden
	_, job := f.approvedJob(t, "", "CREATE TABLE m2_never (id int)", "DROP TABLE m2_never", migration.ChangeApply, "")
	f.process(t)
	got := f.job(t, job.ID)
	if got.Status != migration.JobFailed || got.Error == nil || got.Error.Code != migration.JobErrorRevalidation || f.tableExists(t, "m2_never") {
		t.Fatalf("job = %+v, table exists = %v", got, f.tableExists(t, "m2_never"))
	}
}

// Worker crash setelah commit target; rekonsiliasi harus memakai receipt tanpa menjalankan SQL lagi.
func TestM2WorkerRecoveryCompletesFromReceiptWithoutDoubleCommit(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	_, queued := f.approvedJob(t, "", "CREATE TABLE m2_once (id int)", "DROP TABLE m2_once", migration.ChangeApply, "")
	lease, err := f.store.ClaimJob(ctx, "crashed-worker", time.Now(), time.Minute)
	must(t, err)
	must(t, f.targets.WithDatabaseLock(ctx, f.database, func(locked context.Context, target migration.LockedTarget) error {
		plan, err := f.revalidator.Revalidate(locked, lease.Job)
		if err != nil {
			return err
		}
		if err := f.store.StartRunning(locked, lease, plan); err != nil {
			return err
		}
		result, err := f.runner.Run(locked, plan, target, nil)
		if err != nil || result.Status != migration.JobSucceeded {
			t.Fatalf("run = %+v, %v", result, err)
		}
		return nil
	}))
	if !f.tableExists(t, "m2_once") {
		t.Fatal("target change missing")
	}
	exec(t, f.platform, "UPDATE jobs SET lease_until = now() - interval '1 second' WHERE id = $1", string(queued.ID))
	if resolved := f.worker.RecoverOnce(ctx); resolved != 1 {
		t.Fatalf("recovered %d jobs, want 1", resolved)
	}
	got := f.job(t, queued.ID)
	if got.Status != migration.JobSucceeded || got.Attempt != 1 {
		t.Fatalf("recovered job = %s attempt %d", got.Status, got.Attempt)
	}
	history, err := f.store.ListExecutions(ctx, f.database, migration.ExecutionQuery{})
	must(t, err)
	if len(history.Items) != 1 {
		t.Fatalf("history rows = %d, want 1", len(history.Items))
	}
	err = f.store.RecordOutcome(ctx, lease, migration.RunResult{Status: migration.JobFailed})
	if !errors.Is(err, security.ErrConflict) {
		t.Fatalf("stale lease outcome error = %v, want conflict", err)
	}
}

func TestM2WorkerRecoveryRequeuesJobThatNeverStarted(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	_, queued := f.approvedJob(t, "", "CREATE TABLE m2_later (id int)", "DROP TABLE m2_later", migration.ChangeApply, "")
	_, err := f.store.ClaimJob(ctx, "crashed-worker", time.Now(), time.Minute)
	must(t, err)
	exec(t, f.platform, "UPDATE jobs SET lease_until = now() - interval '1 second' WHERE id = $1", string(queued.ID))
	if resolved := f.worker.RecoverOnce(ctx); resolved != 1 {
		t.Fatalf("recovered %d jobs, want 1", resolved)
	}
	if got := f.job(t, queued.ID); got.Status != migration.JobQueued || got.Error == nil || got.Error.Code != migration.JobErrorInterrupted {
		t.Fatalf("requeued job = %+v", got)
	}
	f.process(t)
	if got := f.job(t, queued.ID); got.Status != migration.JobSucceeded || got.Attempt != 2 || !f.tableExists(t, "m2_later") {
		t.Fatalf("retried job = %s attempt %d", got.Status, got.Attempt)
	}
	attempts, err := f.store.ListAttempts(ctx, f.database, queued.ID, migration.PageQuery{})
	must(t, err)
	if len(attempts.Items) != 2 || attempts.Items[0].Status != migration.JobFailed || attempts.Items[1].Status != migration.JobSucceeded {
		t.Fatalf("attempts = %+v", attempts.Items)
	}
}

func TestM2WorkerClaimIsExclusive(t *testing.T) {
	f := newWorkerFixture(t)
	ctx := context.Background()
	f.approvedJob(t, "", "CREATE TABLE m2_claim (id int)", "DROP TABLE m2_claim", migration.ChangeApply, "")
	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, results[i] = f.store.ClaimJob(ctx, "w"+string(rune('a'+i)), time.Now(), time.Minute)
		}(i)
	}
	wg.Wait()
	claimed := 0
	for _, err := range results {
		switch {
		case err == nil:
			claimed++
		case !errors.Is(err, security.ErrNotFound):
			t.Fatalf("claim error = %v", err)
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed %d times, want exactly 1", claimed)
	}
	if _, err := f.store.ClaimJob(ctx, "late", time.Now(), time.Minute); !errors.Is(err, security.ErrNotFound) {
		t.Fatalf("second claim = %v", err)
	}
}
