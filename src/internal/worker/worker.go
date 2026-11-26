package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/tracing"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Store interface {
	migration.WorkerStore
	migration.ReconciliationStore
	RecoveryPlan(ctx context.Context, databaseID, jobID security.ID) (*migration.ExecutionPlan, error)
}

const (
	DefaultLeaseDuration    = 30 * time.Second
	DefaultPollInterval     = time.Second
	DefaultRecoveryInterval = 15 * time.Second
	DefaultJobTimeout       = 30 * time.Minute
	recoveryPageSize        = 50
)

type Config struct {
	Store            Store
	Coordinator      migration.Coordinator
	Revalidator      migration.Revalidator
	Runner           migration.Runner
	Progress         migration.ProgressSink
	Observer         Observer
	Clock            security.Clock
	Logger           *slog.Logger
	Owner            string
	Concurrency      int
	LeaseDuration    time.Duration
	PollInterval     time.Duration
	RecoveryInterval time.Duration
	JobTimeout       time.Duration
	Backoff          func(attempt int) time.Duration
}

// Observer menerima kejadian untuk metrik. Implementasinya tidak boleh memblokir.
type Observer interface {
	QueueWait(d time.Duration)
	LockWait(d time.Duration)
	RunFinished(kind migration.ChangeKind, mode migration.ExecutionMode, d time.Duration)
	JobFinished(status migration.JobStatus, kind migration.ChangeKind, mode migration.ExecutionMode)
	Retry(kind migration.ChangeKind)
}

type noObserver struct{}

func (noObserver) QueueWait(time.Duration)                                                        {}
func (noObserver) LockWait(time.Duration)                                                         {}
func (noObserver) RunFinished(migration.ChangeKind, migration.ExecutionMode, time.Duration)       {}
func (noObserver) JobFinished(migration.JobStatus, migration.ChangeKind, migration.ExecutionMode) {}
func (noObserver) Retry(migration.ChangeKind)                                                     {}

type Worker struct{ cfg Config }

func New(cfg Config) (*Worker, error) {
	if cfg.Store == nil || cfg.Coordinator == nil || cfg.Revalidator == nil || cfg.Runner == nil || cfg.Clock == nil ||
		cfg.Owner == "" || len(cfg.Owner) > 200 || cfg.Concurrency < 0 {
		return nil, security.ErrInvalidInput
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 1
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = DefaultLeaseDuration
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.RecoveryInterval <= 0 {
		cfg.RecoveryInterval = DefaultRecoveryInterval
	}
	if cfg.JobTimeout <= 0 {
		cfg.JobTimeout = DefaultJobTimeout
	}
	if cfg.Observer == nil {
		cfg.Observer = noObserver{}
	}
	if cfg.Backoff == nil {
		cfg.Backoff = DefaultBackoff
	}
	return &Worker{cfg: cfg}, nil
}

// DefaultBackoff: 2s, 4s, 8s, ... maksimum 1 menit, dengan jitter.
func DefaultBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := 2 * time.Second << min(attempt-1, 5)
	if delay > time.Minute {
		delay = time.Minute
	}
	return delay/2 + rand.N(delay/2+1)
}

// Run berhenti mengambil job saat ctx dibatalkan; job yang sedang berjalan tetap diselesaikan.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range w.cfg.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				worked, err := w.ProcessOne(ctx)
				if err != nil {
					w.cfg.Logger.Warn("job claim failed", "event", "job.claim_failed", "error", safeText(err))
				}
				if !worked || err != nil {
					sleep(ctx, w.cfg.PollInterval)
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			w.RecoverOnce(ctx)
			sleep(ctx, w.cfg.RecoveryInterval)
		}
	}()
	wg.Wait()
}

func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// safeText mencegah teks error driver masuk ke log.
func safeText(err error) string {
	for _, known := range []error{security.ErrConflict, security.ErrNotFound, security.ErrInvalidInput, security.ErrForbidden,
		security.ErrUnauthenticated, security.ErrUnavailable, security.ErrTargetNotReady, migration.ErrExecutionUncertain,
		context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "unexpected error"
}

func (w *Worker) now() time.Time { return w.cfg.Clock.Now().UTC() }

func (w *Worker) log(ctx context.Context, level slog.Level, event string, lease migration.JobLease, attrs ...any) {
	base := []any{"event", event, "job_id", string(lease.Job.ID), "database_id", string(lease.Job.DatabaseID),
		"change_request_id", string(lease.Job.ChangeRequestID), "execution_id", string(lease.Job.ExecutionID),
		"attempt", lease.Job.Attempt, "request_id", lease.Job.RequestID, "worker", w.cfg.Owner, "actor", "worker:" + w.cfg.Owner}
	w.cfg.Logger.Log(ctx, level, event, append(base, attrs...)...)
}

func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	lease, err := w.cfg.Store.ClaimJob(ctx, w.cfg.Owner, w.now(), w.cfg.LeaseDuration)
	if errors.Is(err, security.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	w.handle(ctx, lease)
	return true, nil
}

// heartbeat membatalkan job jika lease diambil alih worker lain.
func (w *Worker) heartbeat(ctx context.Context, cancelJob context.CancelFunc, lease migration.JobLease) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(max(w.cfg.LeaseDuration/3, 10*time.Millisecond))
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := w.cfg.Store.RenewLease(ctx, lease, w.now().Add(w.cfg.LeaseDuration)); errors.Is(err, security.ErrConflict) {
					w.log(ctx, slog.LevelWarn, "job.lease_lost", lease)
					cancelJob()
					return
				}
			}
		}
	}()
	return func() { close(done); <-stopped }
}

type outcome func(ctx context.Context) error

func (w *Worker) handle(parent context.Context, lease migration.JobLease) {
	jobCtx, cancel := context.WithTimeout(w.jobContext(parent, lease), w.cfg.JobTimeout)
	defer cancel()
	jobCtx, endJob := tracing.Start(jobCtx, "job.process", tracing.Consumer)
	failed := true
	defer func() { endJob(failed) }()
	w.log(jobCtx, slog.LevelInfo, "job.claimed", lease, "status", "running")
	w.cfg.Observer.QueueWait(max(w.now().Sub(lease.Job.RunAfter), 0))
	stop := w.heartbeat(jobCtx, cancel, lease)
	var finish outcome
	waitStart := w.now()
	lockCtx, endLock := tracing.Start(jobCtx, "lock.acquire", tracing.Internal)
	acquired := false
	w.log(lockCtx, slog.LevelInfo, "lock.acquire.start", lease, "status", "start")
	lockErr := w.cfg.Coordinator.WithDatabaseLock(lockCtx, lease.Job.DatabaseID, func(locked context.Context, target migration.LockedTarget) error {
		acquired = true
		endLock(false)
		lockWait := w.now().Sub(waitStart)
		w.cfg.Observer.LockWait(lockWait)
		w.log(jobCtx, slog.LevelInfo, "lock.acquired", lease, "lock_wait_ms", lockWait.Milliseconds())
		revalidateCtx, endRevalidation := tracing.Start(locked, "revalidate", tracing.Internal)
		plan, err := w.cfg.Revalidator.Revalidate(revalidateCtx, lease.Job)
		endRevalidation(err != nil)
		if err != nil {
			w.log(jobCtx, slog.LevelWarn, "job.revalidation_failed", lease, "error", safeText(err))
			finish = w.final(lease, migration.RunResult{Status: migration.JobFailed, Error: &migration.JobFailure{
				Code: migration.JobErrorRevalidation, Message: "revalidation failed"}})
			return nil
		}
		w.log(jobCtx, slog.LevelInfo, "job.revalidated", lease)
		if err := w.cfg.Store.StartRunning(locked, lease, plan); err != nil {
			if errors.Is(err, security.ErrConflict) {
				finish = w.final(lease, migration.RunResult{Status: migration.JobFailed, Error: &migration.JobFailure{
					Code: migration.JobErrorRevalidation, Message: "workflow state changed before execution"}})
			} else {
				finish = w.retryOrFail(lease, migration.JobFailure{Code: migration.JobErrorConnection, Message: "platform database unavailable", Retryable: true})
			}
			return nil
		}
		name := "sql.exec"
		if plan.Definition.Mode == migration.ModeBatch {
			name = "batch.process"
		}
		runCtx, endRun := tracing.Start(locked, name, tracing.Internal)
		meta := logging.OperationFromContext(runCtx)
		meta.Revision, meta.Direction, meta.Mutation = plan.Definition.Revision, string(plan.Direction), true
		runCtx = logging.WithOperation(runCtx, meta)
		started := w.now()
		result, runErr := w.cfg.Runner.Run(runCtx, plan, target, w.cfg.Progress)
		w.cfg.Observer.RunFinished(lease.Job.Kind, lease.Job.Mode, w.now().Sub(started))
		failed = runErr != nil || result.Status != migration.JobSucceeded
		endRun(failed)
		finish = w.afterRun(lease, result, runErr)
		return nil
	})
	if !acquired {
		endLock(lockErr != nil)
	}
	if lockErr != nil && finish == nil {
		// Target tidak terjangkau sebelum SQL dikirim.
		w.log(jobCtx, slog.LevelWarn, "lock.failed", lease, "error", safeText(lockErr))
		finish = w.retryOrFail(lease, migration.JobFailure{Code: migration.JobErrorConnection, Message: "target unavailable", Retryable: true})
	}
	if acquired {
		w.log(jobCtx, slog.LevelInfo, "lock.released", lease, "status", "released")
	}
	stop()
	cleanup, done := context.WithTimeout(context.WithoutCancel(jobCtx), 10*time.Second)
	defer done()
	if finish != nil {
		if err := finish(cleanup); err != nil {
			failed = true
			// Lease akan kedaluwarsa dan job diselesaikan oleh loop rekonsiliasi.
			w.log(jobCtx, slog.LevelError, "job.outcome_not_recorded", lease, "error", safeText(err))
		}
	}
}

func (w *Worker) jobContext(parent context.Context, lease migration.JobLease) context.Context {
	ctx := tracing.Restore(context.WithoutCancel(parent), lease.Job.TraceContext.Reveal())
	ctx = security.WithRequestID(ctx, lease.Job.RequestID)
	ctx = logging.WithLogger(ctx, w.cfg.Logger)
	return logging.WithOperation(ctx, logging.Operation{
		Actor: "worker:" + w.cfg.Owner, DatabaseID: lease.Job.DatabaseID, JobID: lease.Job.ID,
		MigrationID: lease.Job.MigrationID, ChangeRequestID: lease.Job.ChangeRequestID, ExecutionID: lease.Job.ExecutionID,
		Attempt: lease.Job.Attempt,
	})
}

func errorCode(failure *migration.JobFailure) string {
	if failure == nil {
		return ""
	}
	return string(failure.Code)
}

// Error tanpa hasil dari runner tidak pernah dianggap rollback.
func (w *Worker) afterRun(lease migration.JobLease, result migration.RunResult, runErr error) outcome {
	switch {
	case result.Status == migration.JobSucceeded && result.Execution != nil:
		execution := *result.Execution
		return func(ctx context.Context) error {
			historyCtx, end := tracing.Start(ctx, "history.append", tracing.Internal)
			err := w.cfg.Store.CompleteCommitted(historyCtx, lease, execution)
			end(err != nil)
			if err != nil {
				return err
			}
			w.cfg.Observer.JobFinished(migration.JobSucceeded, lease.Job.Kind, lease.Job.Mode)
			w.log(ctx, slog.LevelInfo, "job.succeeded", lease, "migration_id", string(execution.MigrationID))
			w.log(historyCtx, slog.LevelInfo, "history.appended", lease, "status", "committed",
				"migration_id", string(execution.MigrationID), "migration_revision", execution.MigrationRevision)
			return nil
		}
	case result.Status == migration.JobFailed && result.Error != nil && result.Error.Retryable:
		return w.retryOrFail(lease, *result.Error)
	case result.Status == "":
		return func(ctx context.Context) error {
			w.log(ctx, slog.LevelError, "job.no_outcome", lease, "error", safeText(runErr))
			return w.final(lease, migration.RunResult{Status: migration.JobUncertain, Error: &migration.JobFailure{
				Code: migration.JobErrorUncertain, Message: "runner returned no proven outcome"}})(ctx)
		}
	default:
		return w.final(lease, result)
	}
}

func (w *Worker) final(lease migration.JobLease, result migration.RunResult) outcome {
	return func(ctx context.Context) error {
		if err := w.cfg.Store.RecordOutcome(ctx, lease, result); err != nil {
			return err
		}
		w.cfg.Observer.JobFinished(result.Status, lease.Job.Kind, lease.Job.Mode)
		w.log(ctx, slog.LevelWarn, "job."+string(result.Status), lease, "status", string(result.Status),
			"error", errorCode(result.Error), "sqlstate", sqlState(result.Error))
		return nil
	}
}

func sqlState(failure *migration.JobFailure) string {
	if failure == nil {
		return ""
	}
	return failure.SQLState
}

func (w *Worker) retryOrFail(lease migration.JobLease, failure migration.JobFailure) outcome {
	if lease.Job.Attempt >= lease.Job.MaxAttempts {
		failure.Retryable = false
		return w.final(lease, migration.RunResult{Status: migration.JobFailed, Error: &failure})
	}
	return func(ctx context.Context) error {
		delay := w.cfg.Backoff(lease.Job.Attempt)
		if err := w.cfg.Store.ScheduleRetry(ctx, lease, failure, w.now().Add(delay)); err != nil {
			return err
		}
		w.cfg.Observer.Retry(lease.Job.Kind)
		w.log(ctx, slog.LevelWarn, "job.retry_scheduled", lease, "status", "scheduled",
			"error", string(failure.Code), "sqlstate", failure.SQLState, "retry_in_ms", delay.Milliseconds())
		return nil
	}
}

// RecoverOnce hanya membaca bukti di target; SQL migration tidak dijalankan ulang.
func (w *Worker) RecoverOnce(ctx context.Context) int {
	page, err := w.cfg.Store.ListRecoverable(ctx, migration.PageQuery{Page: 1, Limit: recoveryPageSize})
	if err != nil {
		w.cfg.Logger.Warn("recovery listing failed", "event", "job.recovery_failed", "error", safeText(err))
		return 0
	}
	resolved := 0
	for _, job := range page.Items {
		if ctx.Err() != nil {
			break
		}
		lease, err := w.cfg.Store.ClaimRecovery(ctx, job.DatabaseID, job.ID, w.cfg.Owner, w.cfg.LeaseDuration)
		if err != nil {
			continue
		}
		if w.recover(ctx, lease) {
			resolved++
		}
	}
	return resolved
}

func (w *Worker) recover(parent context.Context, lease migration.JobLease) (resolved bool) {
	ctx, cancel := context.WithTimeout(w.jobContext(parent, lease), migration.ExecutionTimeout)
	defer cancel()
	ctx, end := tracing.Start(ctx, "job.reconcile", tracing.Consumer)
	defer func() { end(!resolved) }()
	w.log(ctx, slog.LevelInfo, "job.recovery_claimed", lease, "status", string(lease.Job.Status))
	plan, err := w.cfg.Store.RecoveryPlan(ctx, lease.Job.DatabaseID, lease.Job.ID)
	if err != nil {
		return false
	}
	var finish outcome
	if plan == nil {
		// Belum ada SQL yang dikirim.
		finish = w.retryOrFail(lease, migration.JobFailure{Code: migration.JobErrorInterrupted, Message: "worker stopped before execution", Retryable: true})
	} else {
		recoverer, ok := w.cfg.Runner.(Recoverer)
		if !ok {
			return false
		}
		_ = w.cfg.Coordinator.WithDatabaseLock(ctx, lease.Job.DatabaseID, func(locked context.Context, target migration.LockedTarget) error {
			if repairer, ok := w.cfg.Runner.(interface {
				Repair(context.Context, migration.ExecutionPlan, migration.LockedTarget, migration.ProgressSink) error
			}); ok {
				if err := repairer.Repair(locked, *plan, target, w.cfg.Progress); err != nil {
					return err
				}
			}
			result, _ := recoverer.Reconcile(locked, *plan, target)
			switch result.Status {
			case migration.JobSucceeded:
				finish = w.afterRun(lease, result, nil)
			case migration.JobFailed:
				finish = w.retryOrFail(lease, migration.JobFailure{Code: migration.JobErrorInterrupted, Message: "worker stopped before commit", Retryable: true})
			case migration.JobPartial, migration.JobPartiallyReverted:
				finish = w.final(lease, result)
			default:
				if lease.Job.Status == migration.JobRunning {
					finish = w.final(lease, uncertain("commit outcome unknown"))
				}
			}
			return nil
		})
	}
	if finish == nil {
		return false
	}
	if err := finish(ctx); err != nil {
		w.log(ctx, slog.LevelError, "job.outcome_not_recorded", lease, "error", safeText(err))
		return false
	}
	w.log(ctx, slog.LevelInfo, "job.reconciled", lease, "status", "succeeded")
	return true
}
