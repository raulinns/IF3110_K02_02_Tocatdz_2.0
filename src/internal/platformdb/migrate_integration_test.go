package platformdb

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestMigrateAppliesIdempotentlyAndDetectsDrift(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := OpenMigrated(ctx, Config{URL: databaseURL})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second Migrate() call returned error: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM platform_schema_migrations").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration ledger count = %d, error = %v", count, err)
	}
	var checksum string
	if err := pool.QueryRow(ctx, "SELECT checksum FROM platform_schema_migrations WHERE version = 1").Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE platform_schema_migrations SET checksum = 'drift' WHERE version = 1"); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "UPDATE platform_schema_migrations SET checksum = $1 WHERE version = 1", checksum)

	err = Migrate(ctx, pool)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Migrate() drift error = %v", err)
	}
}
