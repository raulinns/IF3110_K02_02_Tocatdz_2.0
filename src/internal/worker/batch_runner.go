package worker

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/targetbatch"
)

const checkpointPage = 500

// BatchRunner menjalankan migration K4: satu transaksi target per batch berisi UPDATE dan checkpoint.
// Batch yang gagal hanya membatalkan dirinya; hasilnya partial dan bisa di-resume atau di-revert.
type BatchRunner struct{ clock security.Clock }

var (
	_ migration.Runner = (*BatchRunner)(nil)
	_ Recoverer        = (*BatchRunner)(nil)
)

func NewBatchRunner(clock security.Clock) (*BatchRunner, error) {
	if clock == nil {
		return nil, security.ErrInvalidInput
	}
	return &BatchRunner{clock: clock}, nil
}

type batchJob struct {
	plan      migration.ExecutionPlan
	target    targetbatch.Target
	config    migration.BatchConfig
	table     string
	key       string
	namespace security.ID
	direction targetbatch.Direction
}

func (r *BatchRunner) prepare(ctx context.Context, plan migration.ExecutionPlan, target migration.LockedTarget) (batchJob, *migration.RunResult) {
	reject := func(code migration.JobErrorCode, message string, retryable bool) (batchJob, *migration.RunResult) {
		result := failed(code, message, "", retryable)
		return batchJob{}, &result
	}
	if plan.Definition.Mode != migration.ModeBatch || plan.Definition.Batch == nil {
		return reject(migration.JobErrorSQL, "migration is not a batch migration", false)
	}
	bt, ok := target.(targetbatch.Target)
	if !ok {
		return reject(migration.JobErrorSQL, "target has no batch capability", false)
	}
	if err := bt.BatchSupported(ctx); err != nil {
		return reject(migration.JobErrorSQL, "target is not prepared for batch migration", false)
	}
	config := *plan.Definition.Batch
	if config.BatchSize < migration.MinBatchSize || config.BatchSize > migration.MaxBatchSize ||
		config.Schema == "" || config.Table == "" || config.KeyColumn == "" {
		return reject(migration.JobErrorSQL, "invalid batch configuration", false)
	}
	// scope_where belum didukung karena butuh allowlist.
	if !config.ScopeWhere.Empty() {
		return reject(migration.JobErrorSQL, "scope_where is not supported", false)
	}
	info, err := bt.KeyColumn(ctx, config.Schema, config.Table, config.KeyColumn)
	if err != nil {
		return reject(migration.JobErrorConnection, "cannot inspect batch key", true)
	}
	if !info.Exists || !info.Integer || !info.NotNull || !info.Unique {
		return reject(migration.JobErrorSQL, "batch key must be a unique, non-null integer column", false)
	}
	job := batchJob{plan: plan, target: bt, config: config, table: pgx.Identifier{config.Schema, config.Table}.Sanitize(),
		key: pgx.Identifier{config.KeyColumn}.Sanitize()}
	switch plan.Direction {
	case migration.DirectionUp:
		job.direction, job.namespace = targetbatch.Up, plan.ReceiptExecutionID
	case migration.DirectionDown:
		// Rollback memakai execution yang dibatalkan sebagai namespace; revert_batches memakai receipt asal.
		job.direction, job.namespace = targetbatch.Down, plan.TargetExecutionID
		if job.namespace == "" {
			job.namespace = plan.SourceExecutionID
		}
		if plan.Definition.Irreversible || plan.Definition.DownSQL.Empty() {
			return reject(migration.JobErrorUnsafeRevert, "migration is irreversible", false)
		}
	default:
		return reject(migration.JobErrorSQL, "invalid direction", false)
	}
	if !security.ValidID(job.namespace) {
		return reject(migration.JobErrorSQL, "missing checkpoint namespace", false)
	}
	fragment := plan.Definition.UpSQL
	if job.direction == targetbatch.Down {
		fragment = plan.Definition.DownSQL
	}
	if mutatesKey(fragment.Reveal(), config.KeyColumn) {
		return reject(migration.JobErrorSQL, "batch SET must not change the key column", false)
	}
	return job, nil
}

func mutatesKey(fragment, key string) bool {
	pattern := regexp.MustCompile(`(?i)(^|,)\s*("?` + regexp.QuoteMeta(key) + `"?)\s*=`)
	return pattern.MatchString(strings.TrimSpace(fragment))
}

func (j batchJob) upStatement() string {
	return fmt.Sprintf(`UPDATE %s SET %s WHERE %s IN (SELECT %s FROM %s WHERE ($1::bigint IS NULL OR %s > $1) AND %s <= $2 ORDER BY %s LIMIT $3) RETURNING %s`,
		j.table, j.plan.Definition.UpSQL.Reveal(), j.key, j.key, j.table, j.key, j.key, j.key, j.key)
}

func (j batchJob) downStatement() string {
	return fmt.Sprintf(`UPDATE %s SET %s WHERE %s = ANY($1::bigint[]) RETURNING %s`, j.table, j.plan.Definition.DownSQL.Reveal(), j.key, j.key)
}

type progress struct {
	lastBatch int64
	cursor    *int64
	scopeMax  *int64
	final     bool
	batches   int64
	rows      int64
	done      map[int64]bool
	nonEmpty  map[int64]bool
}

func scan(ctx context.Context, target targetbatch.Target, namespace security.ID, direction targetbatch.Direction) (progress, error) {
	state := progress{done: map[int64]bool{}, nonEmpty: map[int64]bool{}}
	var after int64
	for {
		page, err := target.Checkpoints(ctx, namespace, direction, targetbatch.Query{AfterBatch: after, Limit: checkpointPage})
		if err != nil {
			return state, err
		}
		for _, checkpoint := range page {
			state.lastBatch, state.final = checkpoint.Batch, checkpoint.Final
			to, scope := checkpoint.To, checkpoint.ScopeMax
			state.cursor, state.scopeMax = &to, &scope
			state.done[checkpoint.Batch] = true
			if checkpoint.Rows > 0 {
				state.nonEmpty[checkpoint.Batch] = true
				state.batches++
				state.rows += checkpoint.Rows
			}
			after = checkpoint.Batch
		}
		if len(page) < checkpointPage {
			return state, nil
		}
	}
}

func decimal(value *int64) *string {
	if value == nil {
		return nil
	}
	text := strconv.FormatInt(*value, 10)
	return &text
}

func mirror(checkpoint targetbatch.Checkpoint, statement string, params []security.Secret, at time.Time) migration.BatchCheckpoint {
	batch := checkpoint.Batch
	return migration.BatchCheckpoint{ExecutionID: checkpoint.ExecutionID, Batch: checkpoint.Batch, From: decimal(checkpoint.From),
		To: strconv.FormatInt(checkpoint.To, 10), ScopeMax: strconv.FormatInt(checkpoint.ScopeMax, 10), Rows: checkpoint.Rows,
		CommittedAt: at, Statement: migration.ExecutedStatement{Query: security.NewSecret(statement), Params: params, Batch: &batch}}
}

// Jika sudah ada batch yang commit, status menjadi partial, bukan failed.
func stopped(direction targetbatch.Direction, done migration.BatchProgress, base migration.RunResult) migration.RunResult {
	base.Progress = done
	committed := done.BatchesCommitted
	status := migration.JobPartial
	if direction == targetbatch.Down {
		committed, status = done.BatchesReverted, migration.JobPartiallyReverted
	}
	if committed > 0 {
		base.Status = status
		if base.Error != nil {
			base.Error.Retryable = false
		}
	}
	return base
}

func (r *BatchRunner) Run(ctx context.Context, plan migration.ExecutionPlan, target migration.LockedTarget, sink migration.ProgressSink) (migration.RunResult, error) {
	job, rejected := r.prepare(ctx, plan, target)
	if rejected != nil {
		return *rejected, security.ErrInvalidInput
	}
	started := r.clock.Now().UTC()
	if job.direction == targetbatch.Up {
		return r.runUp(ctx, job, sink, started)
	}
	return r.runDown(ctx, job, sink, started)
}

func (r *BatchRunner) commitBatch(ctx context.Context, job batchJob, statement string, args []any, build func(keys []int64) targetbatch.Checkpoint) (targetbatch.Checkpoint, *migration.RunResult, error) {
	tx, err := job.target.BeginBatch(ctx)
	if err != nil {
		result := failed(migration.JobErrorConnection, "cannot begin batch transaction", "", true)
		return targetbatch.Checkpoint{}, &result, nil
	}
	rollback := func(result migration.RunResult) (targetbatch.Checkpoint, *migration.RunResult, error) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), migration.CleanupTimeout)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil {
			unknown := uncertain("batch rollback outcome unknown")
			return targetbatch.Checkpoint{}, &unknown, migration.ErrExecutionUncertain
		}
		return targetbatch.Checkpoint{}, &result, nil
	}
	keys, err := tx.UpdateKeys(ctx, statement, args...)
	if err != nil {
		return rollback(classify(err))
	}
	checkpoint := build(keys)
	if err := tx.WriteCheckpoint(ctx, checkpoint); err != nil {
		return rollback(failed(migration.JobErrorConnection, "cannot write batch checkpoint", "", true))
	}
	if err := tx.Commit(ctx); err != nil {
		unknown := uncertain("batch commit outcome unknown")
		return targetbatch.Checkpoint{}, &unknown, migration.ErrExecutionUncertain
	}
	return checkpoint, nil, nil
}

func (r *BatchRunner) runUp(ctx context.Context, job batchJob, sink migration.ProgressSink, started time.Time) (migration.RunResult, error) {
	state, err := scan(ctx, job.target, job.namespace, targetbatch.Up)
	if err != nil {
		return uncertain("cannot read batch checkpoints"), migration.ErrExecutionUncertain
	}
	done := migration.BatchProgress{BatchesCommitted: state.batches, RowsCommitted: state.rows}
	statement := job.upStatement()
	if state.final {
		return r.succeeded(job, statement, started, done), nil
	}
	scope := state.scopeMax
	if scope == nil && job.plan.ScopeMax != nil {
		if value, err := strconv.ParseInt(*job.plan.ScopeMax, 10, 64); err == nil {
			scope = &value
		}
	}
	if scope == nil {
		if scope, err = job.target.ScopeMax(ctx, job.config.Schema, job.config.Table, job.config.KeyColumn); err != nil {
			return stopped(targetbatch.Up, done, failed(migration.JobErrorConnection, "cannot read batch scope", "", true)), nil
		}
	}
	if scope == nil {
		empty := int64(0)
		scope = &empty // tabel kosong: cukup satu checkpoint final
	}
	cursor, batch := state.cursor, state.lastBatch
	for {
		if ctx.Err() != nil {
			return stopped(targetbatch.Up, done, failed(migration.JobErrorInterrupted, "batch processing interrupted", "", true)), nil
		}
		batch++
		from := cursor
		checkpoint, stop, err := r.commitBatch(ctx, job, statement, []any{from, *scope, job.config.BatchSize}, func(keys []int64) targetbatch.Checkpoint {
			to := *scope
			if len(keys) > 0 {
				to = slices.Max(keys)
			}
			return targetbatch.Checkpoint{ExecutionID: job.namespace, Direction: targetbatch.Up, Batch: batch, From: from, To: to,
				ScopeMax: *scope, Rows: int64(len(keys)), Keys: keys, Final: len(keys) < job.config.BatchSize}
		})
		if stop != nil {
			return stopped(targetbatch.Up, done, *stop), err
		}
		if checkpoint.Rows > 0 {
			done.BatchesCommitted++
			done.RowsCommitted += checkpoint.Rows
		}
		if sink != nil {
			_ = sink.BatchCommitted(ctx, mirror(checkpoint, statement, []security.Secret{secretInt(from), security.NewSecret(strconv.FormatInt(*scope, 10)),
				security.NewSecret(strconv.Itoa(job.config.BatchSize))}, r.clock.Now().UTC()))
		}
		if checkpoint.Final {
			return r.succeeded(job, statement, started, done), nil
		}
		to := checkpoint.To
		cursor = &to
	}
}

func secretInt(value *int64) security.Secret {
	if value == nil {
		return security.NewSecret("NULL")
	}
	return security.NewSecret(strconv.FormatInt(*value, 10))
}

func (r *BatchRunner) runDown(ctx context.Context, job batchJob, sink migration.ProgressSink, started time.Time) (migration.RunResult, error) {
	reverted, err := scan(ctx, job.target, job.namespace, targetbatch.Down)
	if err != nil {
		return uncertain("cannot read batch checkpoints"), migration.ErrExecutionUncertain
	}
	original, err := scan(ctx, job.target, job.namespace, targetbatch.Up)
	if err != nil {
		return uncertain("cannot read batch checkpoints"), migration.ErrExecutionUncertain
	}
	if len(original.nonEmpty) == 0 {
		// Tanpa key tersimpan, revert harus menebak row yang terdampak.
		return failed(migration.JobErrorUnsafeRevert, "no committed batch checkpoints to revert", "", false), security.ErrInvalidInput
	}
	done := migration.BatchProgress{BatchesReverted: reverted.batches, RowsReverted: reverted.rows}
	statement := job.downStatement()
	var before int64
	for {
		page, err := job.target.Checkpoints(ctx, job.namespace, targetbatch.Up, targetbatch.Query{BeforeBatch: before, Limit: checkpointPage, Descending: true})
		if err != nil {
			return stopped(targetbatch.Down, done, failed(migration.JobErrorConnection, "cannot read batch checkpoints", "", true)), nil
		}
		for _, source := range page {
			before = source.Batch
			if reverted.done[source.Batch] || source.Rows == 0 {
				continue
			}
			if ctx.Err() != nil {
				return stopped(targetbatch.Down, done, failed(migration.JobErrorInterrupted, "batch revert interrupted", "", true)), nil
			}
			checkpoint, stop, err := r.commitBatch(ctx, job, statement, []any{source.Keys}, func(keys []int64) targetbatch.Checkpoint {
				return targetbatch.Checkpoint{ExecutionID: job.namespace, Direction: targetbatch.Down, Batch: source.Batch, From: source.From,
					To: source.To, ScopeMax: source.ScopeMax, Rows: int64(len(keys)), Keys: keys}
			})
			if stop != nil {
				return stopped(targetbatch.Down, done, *stop), err
			}
			done.BatchesReverted++
			done.RowsReverted += checkpoint.Rows
			if sink != nil {
				_ = sink.BatchReverted(ctx, mirror(checkpoint, statement, []security.Secret{security.NewSecret(fmt.Sprint(len(source.Keys)) + " keys")}, r.clock.Now().UTC()))
			}
		}
		if len(page) < checkpointPage {
			break
		}
	}
	return r.succeeded(job, statement, started, done), nil
}

func (r *BatchRunner) succeeded(job batchJob, statement string, started time.Time, done migration.BatchProgress) migration.RunResult {
	execution := BuildExecution(job.plan, security.NewSecret(statement), started, r.clock.Now())
	if job.plan.Kind == migration.ChangeResume && job.direction == targetbatch.Down && job.plan.TargetExecutionID != "" {
		execution.RevertsExecutionID = job.plan.TargetExecutionID
	}
	return migration.RunResult{Status: migration.JobSucceeded, Progress: done, Execution: execution}
}

// Reconcile hanya memakai checkpoint sebagai bukti.
func (r *BatchRunner) Reconcile(ctx context.Context, plan migration.ExecutionPlan, target migration.LockedTarget) (migration.RunResult, error) {
	job, rejected := r.prepare(ctx, plan, target)
	if rejected != nil {
		return uncertain("cannot inspect batch target"), migration.ErrExecutionUncertain
	}
	started := r.clock.Now().UTC()
	up, err := scan(ctx, job.target, job.namespace, targetbatch.Up)
	if err != nil {
		return uncertain("cannot read batch checkpoints"), migration.ErrExecutionUncertain
	}
	if job.direction == targetbatch.Up {
		done := migration.BatchProgress{BatchesCommitted: up.batches, RowsCommitted: up.rows}
		switch {
		case up.final:
			return r.succeeded(job, job.upStatement(), started, done), nil
		case len(up.done) > 0:
			return migration.RunResult{Status: migration.JobPartial, Progress: done,
				Error: &migration.JobFailure{Code: migration.JobErrorInterrupted, Message: "batch processing interrupted"}}, nil
		}
		return failed(migration.JobErrorInterrupted, "no batch committed", "", true), nil
	}
	down, err := scan(ctx, job.target, job.namespace, targetbatch.Down)
	if err != nil {
		return uncertain("cannot read batch checkpoints"), migration.ErrExecutionUncertain
	}
	done := migration.BatchProgress{BatchesReverted: down.batches, RowsReverted: down.rows}
	remaining := 0
	for batch := range up.nonEmpty {
		if !down.done[batch] {
			remaining++
		}
	}
	switch {
	case len(down.done) == 0:
		return failed(migration.JobErrorInterrupted, "no batch reverted", "", true), nil
	case remaining == 0:
		return r.succeeded(job, job.downStatement(), started, done), nil
	}
	return migration.RunResult{Status: migration.JobPartiallyReverted, Progress: done,
		Error: &migration.JobFailure{Code: migration.JobErrorInterrupted, Message: "batch revert interrupted"}}, nil
}

type ModeRunner struct {
	Normal *NormalRunner
	Batch  *BatchRunner
}

var (
	_ migration.Runner = ModeRunner{}
	_ Recoverer        = ModeRunner{}
)

func NewModeRunner(clock security.Clock) (ModeRunner, error) {
	normal, err := NewNormalRunner(clock)
	if err != nil {
		return ModeRunner{}, err
	}
	batch, err := NewBatchRunner(clock)
	if err != nil {
		return ModeRunner{}, err
	}
	return ModeRunner{Normal: normal, Batch: batch}, nil
}

func (m ModeRunner) Run(ctx context.Context, plan migration.ExecutionPlan, target migration.LockedTarget, sink migration.ProgressSink) (migration.RunResult, error) {
	if plan.Definition.Mode == migration.ModeBatch {
		return m.Batch.Run(ctx, plan, target, sink)
	}
	return m.Normal.Run(ctx, plan, target, sink)
}

func (m ModeRunner) Reconcile(ctx context.Context, plan migration.ExecutionPlan, target migration.LockedTarget) (migration.RunResult, error) {
	if plan.Definition.Mode == migration.ModeBatch {
		return m.Batch.Reconcile(ctx, plan, target)
	}
	return m.Normal.Reconcile(ctx, plan, target)
}
