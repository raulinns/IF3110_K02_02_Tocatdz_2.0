package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestConcurrentMigrationsDoNotExhaustTargetPool(t *testing.T) {
	f := newMigrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	const count = targetMaxConns + 2
	ids := make([]security.ID, count)
	for i := range ids {
		ids[i] = f.draft(t, fmt.Sprintf("Paralel %d", i), fmt.Sprintf("CREATE TABLE public.parallel_%d(id int PRIMARY KEY);", i))
	}
	blocker, err := f.target.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Release()
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_lock($1)", int64(targetLockKey)); err != nil {
		t.Fatal(err)
	}
	defer blocker.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", int64(targetLockKey))
	pool, err := f.targets.pool(ctx, f.database)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result migration.ExecutionResult
		err    error
	}
	results := make(chan outcome, count)
	for _, id := range ids {
		go func() { result, err := f.service.Execute(ctx, f.request, id, 1); results <- outcome{result, err} }()
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for pool.Stat().AcquiredConns() < targetMaxConns {
		select {
		case <-deadline.C:
			t.Fatal("concurrent calls did not fill target lock pool")
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_unlock($1)", int64(targetLockKey)); err != nil {
		t.Fatal(err)
	}
	versions := map[int64]bool{}
	for range ids {
		select {
		case result := <-results:
			if result.err != nil || result.result.Status != migration.Applied {
				t.Fatalf("concurrent execution failed: %+v %v", result.result, result.err)
			}
			if versions[result.result.Version] {
				t.Fatal("duplicate version under concurrency")
			}
			versions[result.result.Version] = true
		case <-ctx.Done():
			t.Fatal("concurrent migration pool stalled")
		}
	}
	for i := 1; i <= count; i++ {
		if !versions[int64(i)] {
			t.Fatal("history versions are not linear")
		}
	}
}
