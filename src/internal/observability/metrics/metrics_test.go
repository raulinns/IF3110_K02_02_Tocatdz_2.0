package metrics

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/postgres"
)

type counter struct {
	counts map[migration.JobStatus]int64
	err    error
}

func (c counter) JobStatusCounts(context.Context) (map[migration.JobStatus]int64, error) {
	return c.counts, c.err
}

type sink struct{ committed, reverted int }

func (s *sink) BatchCommitted(context.Context, migration.BatchCheckpoint) error {
	s.committed++
	return nil
}
func (s *sink) BatchReverted(context.Context, migration.BatchCheckpoint) error {
	s.reverted++
	return nil
}

func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 {
		t.Fatalf("scrape status = %d", w.Code)
	}
	return w.Body.String()
}

func expect(t *testing.T, body string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(body, line) {
			t.Fatalf("metric %q not found in:\n%s", line, body)
		}
	}
}

func TestWorkerObservations(t *testing.T) {
	r := New()
	r.QueueWait(2 * time.Second)
	r.LockWait(50 * time.Millisecond)
	r.RunFinished(migration.ChangeApply, migration.ModeBatch, 3*time.Second)
	r.JobFinished(migration.JobSucceeded, migration.ChangeApply, migration.ModeBatch)
	r.JobFinished(migration.JobFailed, migration.ChangeRollback, migration.ModeNormal)
	r.Retry(migration.ChangeApply)
	body := scrape(t, r)
	expect(t, body,
		"tocatdz_queue_wait_seconds_count 1",
		"tocatdz_lock_wait_seconds_count 1",
		`tocatdz_migration_duration_seconds_count{kind="apply",mode="batch"} 1`,
		`tocatdz_jobs_finished_total{kind="apply",mode="batch",status="succeeded"} 1`,
		`tocatdz_jobs_finished_total{kind="rollback",mode="normal",status="failed"} 1`,
		`tocatdz_job_retries_total{kind="apply"} 1`,
		// Seri awal tersedia sebelum ada kejadian.
		`tocatdz_job_retries_total{kind="resume"} 0`,
	)
}

func TestProgressCountsOnlyNonEmptyBatches(t *testing.T) {
	r := New()
	next := &sink{}
	progress := r.Progress(next)
	ctx := context.Background()
	_ = progress.BatchCommitted(ctx, migration.BatchCheckpoint{Rows: 100})
	_ = progress.BatchCommitted(ctx, migration.BatchCheckpoint{Rows: 0})
	_ = progress.BatchReverted(ctx, migration.BatchCheckpoint{Rows: 40})
	if next.committed != 2 || next.reverted != 1 {
		t.Fatalf("wrapped sink calls = %d/%d, want 2/1", next.committed, next.reverted)
	}
	expect(t, scrape(t, r),
		`tocatdz_batches_processed_total{direction="up"} 1`, `tocatdz_rows_processed_total{direction="up"} 100`,
		`tocatdz_batches_processed_total{direction="down"} 1`, `tocatdz_rows_processed_total{direction="down"} 40`)
}

func TestJobGaugeListsEveryStatus(t *testing.T) {
	r := New()
	r.WatchJobs(counter{counts: map[migration.JobStatus]int64{migration.JobQueued: 3, migration.JobRunning: 1}})
	expect(t, scrape(t, r), `tocatdz_jobs{status="queued"} 3`, `tocatdz_jobs{status="running"} 1`,
		`tocatdz_jobs{status="failed"} 0`, `tocatdz_jobs{status="partially_reverted"} 0`)

	broken := New()
	broken.WatchJobs(counter{err: errors.New("db down")})
	if strings.Contains(scrape(t, broken), "tocatdz_jobs{") {
		t.Fatal("gauge reported values although the query failed")
	}
}

func TestPoolMetricsUseBoundedLabels(t *testing.T) {
	r := New()
	r.WatchPool("platform", func() postgres.PoolStats {
		return postgres.PoolStats{Active: 2, Idle: 3, Acquires: 10, AcquireWait: 1500 * time.Millisecond}
	})
	body := scrape(t, r)
	expect(t, body,
		`tocatdz_db_pool_connections{pool="platform",state="active"} 2`,
		`tocatdz_db_pool_connections{pool="platform",state="idle"} 3`,
		`tocatdz_db_pool_acquire_wait_seconds_total{pool="platform"} 1.5`,
		`tocatdz_db_pool_acquires_total{pool="platform"} 10`)
	// Beberapa pool (platform dan target) harus bisa didaftarkan bersama pada satu registry.
	r.WatchPool("target", func() postgres.PoolStats { return postgres.PoolStats{Active: 1} })
	expect(t, scrape(t, r), `tocatdz_db_pool_connections{pool="target",state="active"} 1`,
		`tocatdz_db_pool_connections{pool="platform",state="active"} 2`)
	for _, forbidden := range []string{"job_id", "user", "sql", "database_id"} {
		if strings.Contains(body, forbidden+"=") {
			t.Fatalf("label %s must not appear in metrics", forbidden)
		}
	}
}
