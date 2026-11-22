// Package worker menjalankan job migration dari queue PostgreSQL.
package worker

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type Recoverer interface {
	Reconcile(ctx context.Context, plan migration.ExecutionPlan, target migration.LockedTarget) (migration.RunResult, error)
}

// NormalRunner menjalankan SQL dan receipt dalam satu transaksi target. Receipt mencegah double commit.
type NormalRunner struct{ clock security.Clock }

var (
	_ migration.Runner = (*NormalRunner)(nil)
	_ Recoverer        = (*NormalRunner)(nil)
)

func NewNormalRunner(clock security.Clock) (*NormalRunner, error) {
	if clock == nil {
		return nil, security.ErrInvalidInput
	}
	return &NormalRunner{clock: clock}, nil
}

func failed(code migration.JobErrorCode, message, sqlstate string, retryable bool) migration.RunResult {
	return migration.RunResult{Status: migration.JobFailed, Error: &migration.JobFailure{Code: code, Message: message, SQLState: sqlstate, Retryable: retryable}}
}

func uncertain(message string) migration.RunResult {
	return migration.RunResult{Status: migration.JobUncertain, Error: &migration.JobFailure{Code: migration.JobErrorUncertain, Message: message}}
}

func statementFor(plan migration.ExecutionPlan) (security.Secret, error) {
	switch plan.Direction {
	case migration.DirectionUp:
		return plan.Definition.UpSQL, nil
	case migration.DirectionDown:
		if plan.Definition.Irreversible || plan.Definition.DownSQL.Empty() {
			return security.Secret{}, security.ErrInvalidInput
		}
		return plan.Definition.DownSQL, nil
	}
	return security.Secret{}, security.ErrInvalidInput
}

func ReceiptKey(plan migration.ExecutionPlan, sql security.Secret) migration.ExecutionKey {
	return migration.ExecutionKey{DatabaseID: plan.DatabaseID, MigrationID: plan.MigrationID, ExecutionID: plan.ReceiptExecutionID,
		Version: int64(plan.ExpectedPosition) + 1, Checksum: sha256.Sum256([]byte(sql.Reveal()))}
}

func BuildExecution(plan migration.ExecutionPlan, sql security.Secret, started, committed time.Time) *migration.Execution {
	started, committed = started.UTC(), committed.UTC()
	if committed.Before(started) {
		committed = started
	}
	execution := &migration.Execution{
		ID: plan.ReceiptExecutionID, DatabaseID: plan.DatabaseID, MigrationID: plan.MigrationID, ChangeRequestID: plan.ChangeRequestID,
		JobID: plan.JobID, MigrationRevision: plan.Definition.Revision, Checksum: plan.Definition.Checksum, Operation: plan.Kind,
		Direction: plan.Direction, Definition: plan.Definition.DefinitionInput, Statements: []migration.ExecutedStatement{{Query: sql}},
		ActorID: plan.ActorID, StartedAt: started, CommittedAt: committed,
	}
	switch plan.Kind {
	case migration.ChangeRollback:
		execution.RevertsExecutionID = plan.TargetExecutionID
	case migration.ChangeForward:
		execution.ForwardOfExecutionID = plan.TargetExecutionID
	}
	return execution
}

// Error sementara (serialization, deadlock, lock timeout, koneksi) boleh di-retry.
func classify(err error) migration.RunResult {
	var stateful interface{ SQLState() string }
	state := ""
	if errors.As(err, &stateful) {
		state = stateful.SQLState()
	}
	switch {
	case state == "40001" || state == "40P01" || state == "55P03":
		return failed(migration.JobErrorSQL, "transient database conflict", state, true)
	case strings.HasPrefix(state, "08") || state == "57P01" || state == "57P02" || state == "57P03":
		return failed(migration.JobErrorConnection, "target connection lost", state, true)
	case state == "":
		return failed(migration.JobErrorSQL, "migration statement failed", "", false)
	default:
		return failed(migration.JobErrorSQL, "migration statement failed", state, false)
	}
}

func (r *NormalRunner) Run(ctx context.Context, plan migration.ExecutionPlan, target migration.LockedTarget, _ migration.ProgressSink) (migration.RunResult, error) {
	if plan.Definition.Mode != migration.ModeNormal {
		return failed(migration.JobErrorSQL, "batch mode needs the batch runner", "", false), security.ErrInvalidInput
	}
	if target == nil {
		return failed(migration.JobErrorConnection, "target unavailable", "", true), security.ErrUnavailable
	}
	// Workflow M2 hanya untuk PostgreSQL.
	if _, journaled := target.(migration.JournaledTarget); journaled {
		return failed(migration.JobErrorSQL, "migration workflow requires a PostgreSQL target", "", false), security.ErrInvalidInput
	}
	sql, err := statementFor(plan)
	if err != nil {
		return failed(migration.JobErrorUnsafeRevert, "migration is irreversible", "", false), err
	}
	started := r.clock.Now().UTC()
	// Receipt yang sudah ada berarti attempt sebelumnya sudah commit.
	if prior, err := r.reconcile(ctx, plan, target, sql, started); err != nil || prior.Status != migration.JobFailed {
		return prior, err
	}
	tx, err := target.Begin(ctx)
	if err != nil || tx == nil {
		return failed(migration.JobErrorConnection, "cannot begin target transaction", "", true), security.ErrUnavailable
	}
	rollback := func(result migration.RunResult) (migration.RunResult, error) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), migration.CleanupTimeout)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil {
			return uncertain("rollback outcome unknown"), migration.ErrExecutionUncertain
		}
		return result, nil
	}
	if err := tx.Execute(ctx, sql); err != nil {
		return rollback(classify(err))
	}
	key := ReceiptKey(plan, sql)
	if err := tx.WriteReceipt(ctx, migration.Receipt{Key: key}); err != nil {
		return rollback(failed(migration.JobErrorConnection, "cannot write execution receipt", "", true))
	}
	if err := tx.Commit(ctx); err != nil {
		// Hasil commit tidak pasti; hanya receipt yang bisa membuktikan.
		return uncertain("commit outcome unknown"), migration.ErrExecutionUncertain
	}
	return migration.RunResult{Status: migration.JobSucceeded, Execution: BuildExecution(plan, sql, started, r.clock.Now())}, nil
}

func (r *NormalRunner) Reconcile(ctx context.Context, plan migration.ExecutionPlan, target migration.LockedTarget) (migration.RunResult, error) {
	if target == nil {
		return uncertain("target unavailable"), migration.ErrExecutionUncertain
	}
	sql, err := statementFor(plan)
	if err != nil {
		return uncertain("plan has no executable statement"), err
	}
	return r.reconcile(ctx, plan, target, sql, r.clock.Now())
}

func (r *NormalRunner) reconcile(ctx context.Context, plan migration.ExecutionPlan, target migration.LockedTarget, sql security.Secret, started time.Time) (migration.RunResult, error) {
	key := ReceiptKey(plan, sql)
	lookup, err := target.LookupReceipt(ctx, key)
	if err != nil || !lookup.Quiescent {
		return uncertain("cannot prove execution outcome"), migration.ErrExecutionUncertain
	}
	if lookup.Receipt == nil {
		if lookup.Started {
			return uncertain("execution started without receipt"), migration.ErrExecutionUncertain
		}
		return failed(migration.JobErrorInterrupted, "no commit found", "", true), nil
	}
	if lookup.Receipt.Key != key {
		return uncertain("receipt does not match plan"), migration.ErrExecutionUncertain
	}
	return migration.RunResult{Status: migration.JobSucceeded, Execution: BuildExecution(plan, sql, started, r.clock.Now())}, nil
}
