package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging/sqllog"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/tracing"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/targetbatch"
)

// These are component integration tests: the SDK, logging, worker and batch
// transaction orchestration are real; queue storage and database IO are doubles.
func TestTelemetryEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name, state, event string
		batch, commitFail  bool
	}{
		{name: "execute", event: "job.succeeded"},
		{name: "retry", state: "40001", event: "job.retry_scheduled"},
		{name: "failure", state: "23505", event: "job.failed"},
		{name: "batch", batch: true, event: "batch.commit"},
		{name: "batch_rollback", batch: true, state: "23505", event: "batch.rollback"},
		{name: "batch_uncertain", batch: true, commitFail: true, event: "job.uncertain"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
			var spans, logs bytes.Buffer
			shutdown, err := tracing.StartProcess(context.Background(), "tocatdz-test", &spans)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = shutdown() })
			// Model an already completed API producer. Actual API middleware is
			// exercised separately by TestAPITelemetryEvidence.
			httpCtx, endHTTP := tracing.Start(context.Background(), "HTTP POST /api/v1/databases/{database_id}/change-requests/{change_request_id}/execute", tracing.Server)
			producer, endProducer := tracing.Start(httpCtx, "job.enqueue", tracing.Producer)
			lease := leaseAt(1, 3)
			lease.Job.RequestID = "test-request-" + scenario.name
			lease.Job.TraceContext = security.NewSecret(tracing.Inject(producer))
			lease.Job.ExecutionID = "33333333-3333-4333-8333-333333333333"
			lease.Job.MigrationID = "44444444-4444-4444-8444-444444444444"
			endProducer(false)
			endHTTP(false)
			store := &fakeStore{lease: lease}
			w := newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{}, fakeRunner{})
			w.cfg.Logger = logging.New(&logs)
			w.cfg.Revalidator = telemetryRevalidator{batch: scenario.batch}
			w.cfg.Runner = telemetryRunner{batch: scenario.batch, state: scenario.state, commitFail: scenario.commitFail}
			if worked, err := w.ProcessOne(context.Background()); !worked || err != nil {
				t.Fatalf("worked=%v error=%v", worked, err)
			}
			if err := shutdown(); err != nil {
				t.Fatal(err)
			}
			traceID := tracing.Current(httpCtx).TraceID
			spanByID := map[string]map[string]any{}
			spanByName := map[string]map[string]any{}
			for _, span := range telemetryRecords(t, spans.Bytes()) {
				if span["trace_id"] != traceID {
					t.Fatal("span lost the producer trace")
				}
				spanByID[span["span_id"].(string)] = span
				spanByName[span["name"].(string)] = span
			}
			if spanByName["job.process"]["parent_span_id"] != tracing.Current(producer).SpanID {
				t.Fatal("consumer lost its queue parent")
			}
			if spanByName["lock.acquire"] == nil || spanByName["db.query"] == nil {
				t.Fatal("missing lock or query span")
			}
			failed := scenario.state != "" || scenario.commitFail
			if (spanByName["job.process"]["status"] == "Error") != failed {
				t.Fatal("incorrect consumer error status")
			}
			seen := map[string]bool{}
			for _, record := range telemetryRecords(t, logs.Bytes()) {
				seen[record["event"].(string)] = true
				span := spanByID[record["span_id"].(string)]
				if record["trace_id"] != traceID || span == nil || record["request_id"] != lease.Job.RequestID {
					t.Fatal("log cannot be joined to its exported span and original request")
				}
				if record["event"] == scenario.event && (record["migration_revision"] != float64(7) || record["direction"] != "up") {
					t.Fatal("outcome lost the reviewed revision/direction")
				}
			}
			if !seen[scenario.event] {
				t.Fatalf("missing event %s", scenario.event)
			}
			if failed && (seen["history.appended"] || seen["job.succeeded"] || seen["batch.commit"]) {
				t.Fatal("failure emitted an unproven commit/success")
			}
			if strings.Contains(logs.String()+spans.String(), "canary") {
				t.Fatal("telemetry exposed secret input")
			}
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				t.Logf("K5_LOG %s", line)
			}
			for _, line := range strings.Split(strings.TrimSpace(spans.String()), "\n") {
				t.Logf("K5_SPAN %s", line)
			}
		})
	}
}

func telemetryRecords(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

type telemetryRevalidator struct{ batch bool }

func (r telemetryRevalidator) Revalidate(context.Context, migration.JobView) (migration.ExecutionPlan, error) {
	plan := migration.ExecutionPlan{Direction: migration.DirectionUp, Definition: migration.MigrationDefinition{Revision: 7}}
	if r.batch {
		plan.Definition.Mode = migration.ModeBatch
	}
	return plan, nil
}

type telemetryRunner struct {
	migration.Runner
	batch      bool
	state      string
	commitFail bool
}

func (r telemetryRunner) Run(ctx context.Context, plan migration.ExecutionPlan, _ migration.LockedTarget, _ migration.ProgressSink) (migration.RunResult, error) {
	tx := telemetryTx{state: r.state, commitFail: r.commitFail}
	if r.batch {
		runner, _ := NewBatchRunner(security.SystemClock{})
		_, result, err := runner.commitBatch(ctx, batchJob{target: telemetryTarget{tx: tx}, direction: targetbatch.Up}, 1,
			"UPDATE items SET text = 'literal-canary' WHERE id = $1", []any{"parameter-canary"},
			func(keys []int64) targetbatch.Checkpoint {
				return targetbatch.Checkpoint{Batch: 1, Rows: int64(len(keys))}
			})
		if result != nil {
			return *result, err
		}
	} else {
		_, err := tx.UpdateKeys(ctx, "UPDATE items SET text = 'literal-canary' WHERE id = $1", "parameter-canary")
		if err != nil {
			_ = tx.Rollback(ctx)
			return classify(err), nil
		}
		if err := tx.Commit(ctx); err != nil {
			return uncertain("commit outcome unknown"), err
		}
	}
	return migration.RunResult{Status: migration.JobSucceeded, Execution: &migration.Execution{
		MigrationID: logging.OperationFromContext(ctx).MigrationID, MigrationRevision: plan.Definition.Revision}}, nil
}

type telemetryTarget struct {
	targetbatch.Target
	tx telemetryTx
}

func (t telemetryTarget) BeginBatch(context.Context) (targetbatch.Tx, error) { return t.tx, nil }

type telemetryTx struct {
	state      string
	commitFail bool
}

func (tx telemetryTx) UpdateKeys(ctx context.Context, sql string, args ...any) ([]int64, error) {
	var err error
	if tx.state != "" {
		err = &pgconn.PgError{Code: tx.state, Message: "driver-canary"}
	}
	ctx = sqllog.Start(ctx, sqllog.Target, security.NewSecret(sql), len(args))
	sqllog.Done(ctx, int64(1), err)
	return []int64{1}, err
}
func (telemetryTx) WriteCheckpoint(context.Context, targetbatch.Checkpoint) error { return nil }
func (tx telemetryTx) Commit(ctx context.Context) error {
	var err error
	if tx.commitFail {
		err = errors.New("commit-canary")
	}
	ctx = sqllog.Start(ctx, sqllog.Target, security.NewSecret("commit"), 0)
	sqllog.Done(ctx, nil, err)
	return err
}
func (telemetryTx) Rollback(ctx context.Context) error {
	ctx = sqllog.Start(ctx, sqllog.Target, security.NewSecret("rollback"), 0)
	sqllog.Done(ctx, nil, nil)
	return nil
}
