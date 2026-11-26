package worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type fakeStore struct {
	mu        sync.Mutex
	lease     *migration.JobLease
	calls     []string
	outcome   migration.RunResult
	failure   migration.JobFailure
	plan      *migration.ExecutionPlan
	startErr  error
	renewErr  error
	recovered []migration.JobView
}

func (s *fakeStore) record(call string) { s.mu.Lock(); s.calls = append(s.calls, call); s.mu.Unlock() }

func (s *fakeStore) ClaimJob(context.Context, string, time.Time, time.Duration) (migration.JobLease, error) {
	if s.lease == nil {
		return migration.JobLease{}, security.ErrNotFound
	}
	lease := *s.lease
	s.lease = nil
	return lease, nil
}
func (s *fakeStore) RenewLease(_ context.Context, lease migration.JobLease, _ time.Time) (migration.JobLease, error) {
	return lease, s.renewErr
}
func (s *fakeStore) StartRunning(context.Context, migration.JobLease, migration.ExecutionPlan) error {
	s.record("start")
	return s.startErr
}
func (s *fakeStore) CompleteCommitted(context.Context, migration.JobLease, migration.Execution) error {
	s.record("complete")
	return nil
}
func (s *fakeStore) RecordOutcome(_ context.Context, _ migration.JobLease, result migration.RunResult) error {
	s.record("outcome")
	s.outcome = result
	return nil
}
func (s *fakeStore) ScheduleRetry(_ context.Context, _ migration.JobLease, failure migration.JobFailure, _ time.Time) error {
	s.record("retry")
	s.failure = failure
	return nil
}
func (s *fakeStore) ListRecoverable(context.Context, migration.PageQuery) (migration.Page[migration.JobView], error) {
	return migration.Page[migration.JobView]{Items: s.recovered}, nil
}
func (s *fakeStore) ClaimRecovery(_ context.Context, _, _ security.ID, owner string, _ time.Duration) (migration.JobLease, error) {
	job := s.recovered[0]
	s.recovered = nil
	return migration.JobLease{Job: job, Owner: owner, Token: 2}, nil
}
func (s *fakeStore) RecoveryPlan(context.Context, security.ID, security.ID) (*migration.ExecutionPlan, error) {
	return s.plan, nil
}

type fakeCoordinator struct{ err error }

func (c fakeCoordinator) WithDatabaseLock(ctx context.Context, _ security.ID, run func(context.Context, migration.LockedTarget) error) error {
	if c.err != nil {
		return c.err
	}
	return run(ctx, nil)
}

type fakeRevalidator struct{ err error }

func (r fakeRevalidator) Revalidate(context.Context, migration.JobView) (migration.ExecutionPlan, error) {
	return migration.ExecutionPlan{}, r.err
}

type fakeRunner struct {
	result    migration.RunResult
	err       error
	reconcile migration.RunResult
	block     chan struct{}
}

func (r fakeRunner) Run(ctx context.Context, _ migration.ExecutionPlan, _ migration.LockedTarget, _ migration.ProgressSink) (migration.RunResult, error) {
	if r.block != nil {
		select {
		case <-ctx.Done():
			return uncertain("canceled"), ctx.Err()
		case <-r.block:
		}
	}
	return r.result, r.err
}
func (r fakeRunner) Reconcile(context.Context, migration.ExecutionPlan, migration.LockedTarget) (migration.RunResult, error) {
	return r.reconcile, nil
}

func newTestWorker(t *testing.T, store *fakeStore, coordinator fakeCoordinator, revalidator fakeRevalidator, runner fakeRunner) *Worker {
	t.Helper()
	w, err := New(Config{Store: store, Coordinator: coordinator, Revalidator: revalidator, Runner: runner, Clock: security.SystemClock{},
		Owner: "test", LeaseDuration: 30 * time.Millisecond, Backoff: func(int) time.Duration { return time.Second }})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func leaseAt(attempt, maxAttempts int) *migration.JobLease {
	return &migration.JobLease{Owner: "test", Token: 1, Job: migration.JobView{
		JobSpec: migration.JobSpec{ID: "11111111-1111-4111-8111-111111111111", DatabaseID: "22222222-2222-4222-8222-222222222222", MaxAttempts: maxAttempts},
		Attempt: attempt, Status: migration.JobRunning}}
}

func calls(store *fakeStore) []string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]string(nil), store.calls...)
}

func expectCalls(t *testing.T, store *fakeStore, want ...string) {
	t.Helper()
	got := calls(store)
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
}

func TestProcessOneReportsIdleQueue(t *testing.T) {
	w := newTestWorker(t, &fakeStore{}, fakeCoordinator{}, fakeRevalidator{}, fakeRunner{})
	if worked, err := w.ProcessOne(context.Background()); worked || err != nil {
		t.Fatalf("idle = %v, %v", worked, err)
	}
}

func TestSuccessfulRunCompletesHistory(t *testing.T) {
	store := &fakeStore{lease: leaseAt(1, 3)}
	runner := fakeRunner{result: migration.RunResult{Status: migration.JobSucceeded, Execution: &migration.Execution{}}}
	w := newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{}, runner)
	if _, err := w.ProcessOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	expectCalls(t, store, "start", "complete")
}

func TestTransientFailureIsRetriedUntilAttemptsRunOut(t *testing.T) {
	transient := migration.RunResult{Status: migration.JobFailed, Error: &migration.JobFailure{Code: migration.JobErrorSQL, SQLState: "40001", Retryable: true}}
	store := &fakeStore{lease: leaseAt(1, 3)}
	w := newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{}, fakeRunner{result: transient})
	_, _ = w.ProcessOne(context.Background())
	expectCalls(t, store, "start", "retry")
	if store.failure.SQLState != "40001" {
		t.Fatalf("retry failure = %+v", store.failure)
	}

	store = &fakeStore{lease: leaseAt(3, 3)}
	w = newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{}, fakeRunner{result: transient})
	_, _ = w.ProcessOne(context.Background())
	expectCalls(t, store, "start", "outcome")
	if store.outcome.Status != migration.JobFailed || store.outcome.Error.Retryable {
		t.Fatalf("exhausted outcome = %+v", store.outcome)
	}
}

func TestPermanentFailureIsFinal(t *testing.T) {
	store := &fakeStore{lease: leaseAt(1, 3)}
	runner := fakeRunner{result: migration.RunResult{Status: migration.JobFailed, Error: &migration.JobFailure{Code: migration.JobErrorSQL, SQLState: "42P01"}}}
	w := newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{}, runner)
	_, _ = w.ProcessOne(context.Background())
	expectCalls(t, store, "start", "outcome")
}

func TestRunnerWithoutProvenOutcomeIsUncertain(t *testing.T) {
	store := &fakeStore{lease: leaseAt(1, 3)}
	w := newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{}, fakeRunner{err: errors.New("boom")})
	_, _ = w.ProcessOne(context.Background())
	expectCalls(t, store, "start", "outcome")
	if store.outcome.Status != migration.JobUncertain {
		t.Fatalf("outcome = %s, want uncertain", store.outcome.Status)
	}
}

func TestRevalidationFailureNeverStartsExecution(t *testing.T) {
	store := &fakeStore{lease: leaseAt(1, 3)}
	w := newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{err: security.ErrForbidden}, fakeRunner{})
	_, _ = w.ProcessOne(context.Background())
	expectCalls(t, store, "outcome")
	if store.outcome.Error.Code != migration.JobErrorRevalidation {
		t.Fatalf("outcome = %+v", store.outcome)
	}
}

func TestStateChangeBeforeStartFailsJob(t *testing.T) {
	store := &fakeStore{lease: leaseAt(1, 3), startErr: security.ErrConflict}
	w := newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{}, fakeRunner{})
	_, _ = w.ProcessOne(context.Background())
	expectCalls(t, store, "start", "outcome")
}

func TestUnreachableTargetIsRetried(t *testing.T) {
	store := &fakeStore{lease: leaseAt(1, 3)}
	w := newTestWorker(t, store, fakeCoordinator{err: security.ErrUnavailable}, fakeRevalidator{}, fakeRunner{})
	_, _ = w.ProcessOne(context.Background())
	expectCalls(t, store, "retry")
	if store.failure.Code != migration.JobErrorConnection {
		t.Fatalf("failure = %+v", store.failure)
	}
}

func TestLostLeaseCancelsRunningJob(t *testing.T) {
	store := &fakeStore{lease: leaseAt(1, 3), renewErr: security.ErrConflict}
	w := newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{}, fakeRunner{block: make(chan struct{})})
	done := make(chan struct{})
	go func() { _, _ = w.ProcessOne(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("job kept running after its lease was lost")
	}
}

func TestRecoveryUsesReceiptEvidence(t *testing.T) {
	job := leaseAt(1, 3).Job
	for _, tc := range []struct {
		name      string
		plan      *migration.ExecutionPlan
		reconcile migration.RunResult
		want      string
	}{
		{"never started", nil, migration.RunResult{}, "retry"},
		{"receipt found", &migration.ExecutionPlan{}, migration.RunResult{Status: migration.JobSucceeded, Execution: &migration.Execution{}}, "complete"},
		{"no receipt", &migration.ExecutionPlan{}, failed(migration.JobErrorInterrupted, "", "", true), "retry"},
		{"cannot prove", &migration.ExecutionPlan{}, uncertain("x"), "outcome"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{recovered: []migration.JobView{job}, plan: tc.plan}
			w := newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{}, fakeRunner{reconcile: tc.reconcile})
			if resolved := w.RecoverOnce(context.Background()); resolved != 1 {
				t.Fatalf("resolved = %d", resolved)
			}
			expectCalls(t, store, tc.want)
		})
	}
}

func TestClassifyTransientSQLStates(t *testing.T) {
	for state, retryable := range map[string]bool{"40001": true, "40P01": true, "55P03": true, "08006": true, "57P01": true, "42P01": false, "23505": false, "": false} {
		result := classify(stateError(state))
		if result.Error.Retryable != retryable || result.Error.SQLState != state {
			t.Fatalf("classify(%q) = %+v", state, result.Error)
		}
	}
}

type stateError string

func (e stateError) Error() string    { return "statement failed" }
func (e stateError) SQLState() string { return string(e) }

func TestDefaultBackoffGrowsAndIsBounded(t *testing.T) {
	for attempt := 1; attempt <= 10; attempt++ {
		delay := DefaultBackoff(attempt)
		ceiling := min(2*time.Second<<min(attempt-1, 5), time.Minute)
		if delay < ceiling/2 || delay > ceiling {
			t.Fatalf("attempt %d delay %v outside [%v, %v]", attempt, delay, ceiling/2, ceiling)
		}
	}
}

func TestMutatesKeyDetectsKeyAssignment(t *testing.T) {
	for fragment, want := range map[string]bool{
		"id = id + 1": true, `"id"=5`: true, "amount = 1, id = 2": true, " ID = 3": true,
		"amount = amount + 1": false, "valid = true": false, "paid_id = 2": false,
	} {
		if got := mutatesKey(fragment, "id"); got != want {
			t.Fatalf("mutatesKey(%q) = %v, want %v", fragment, got, want)
		}
	}
}

type recorder struct {
	events []string
}

func (r *recorder) QueueWait(time.Duration) { r.events = append(r.events, "queue_wait") }
func (r *recorder) LockWait(time.Duration)  { r.events = append(r.events, "lock_wait") }
func (r *recorder) RunFinished(migration.ChangeKind, migration.ExecutionMode, time.Duration) {
	r.events = append(r.events, "run")
}
func (r *recorder) JobFinished(status migration.JobStatus, _ migration.ChangeKind, _ migration.ExecutionMode) {
	r.events = append(r.events, "finished:"+string(status))
}
func (r *recorder) Retry(migration.ChangeKind) { r.events = append(r.events, "retry") }

func TestObserverSeesQueueLockRunAndOutcome(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result migration.RunResult
		lease  *migration.JobLease
		want   []string
	}{
		{"success", migration.RunResult{Status: migration.JobSucceeded, Execution: &migration.Execution{}}, leaseAt(1, 3),
			[]string{"queue_wait", "lock_wait", "run", "finished:succeeded"}},
		{"retry", failed(migration.JobErrorSQL, "", "40001", true), leaseAt(1, 3), []string{"queue_wait", "lock_wait", "run", "retry"}},
		{"final failure", failed(migration.JobErrorSQL, "", "42P01", false), leaseAt(1, 3),
			[]string{"queue_wait", "lock_wait", "run", "finished:failed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer := &recorder{}
			store := &fakeStore{lease: tc.lease}
			w, err := New(Config{Store: store, Coordinator: fakeCoordinator{}, Revalidator: fakeRevalidator{}, Runner: fakeRunner{result: tc.result},
				Clock: security.SystemClock{}, Owner: "test", Observer: observer, Backoff: func(int) time.Duration { return 0 }})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.ProcessOne(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(observer.events, ","); got != strings.Join(tc.want, ",") {
				t.Fatalf("events = %s, want %v", got, tc.want)
			}
		})
	}
}
