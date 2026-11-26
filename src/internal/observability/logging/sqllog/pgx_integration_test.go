package sqllog

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/tracing"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

// Only a connection-local temporary table is modified. CI supplies the test DB.
func TestPostgresTelemetryEvidence(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is required for real PostgreSQL evidence")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	var logs, spans bytes.Buffer
	shutdown, err := tracing.StartProcess(context.Background(), "tocatdz-postgres-test", &spans)
	if err != nil {
		t.Fatal("cannot initialize SDK")
	}
	t.Cleanup(func() { _ = shutdown() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ctx = logging.WithLogger(security.WithRequestID(ctx, "postgres-verification"), logging.New(&logs))
	ctx = logging.WithOperation(ctx, logging.Operation{Actor: "system", Mutation: true})
	ctx, end := tracing.Start(ctx, "postgres.verification", tracing.Internal)
	config, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	config.Tracer = Tracer{Kind: Target}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal("cannot connect to test PostgreSQL")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Close(cleanup)
	}()
	if _, err := conn.Exec(ctx, "CREATE TEMP TABLE telemetry_items (id integer PRIMARY KEY, value text)"); err != nil {
		t.Fatal("cannot create temporary test table")
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal("cannot begin test transaction")
	}
	if _, err := tx.Exec(ctx, "INSERT INTO telemetry_items VALUES ($1, $2)", 1, "parameter-canary"); err != nil {
		t.Fatal("cannot insert test row")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("cannot commit test transaction")
	}
	var value string
	if err := conn.QueryRow(ctx, "SELECT value FROM telemetry_items WHERE id = 1").Scan(&value); err != nil || value != "parameter-canary" {
		t.Fatal("cannot read committed test row")
	}
	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal("cannot begin failure transaction")
	}
	if _, err := tx.Exec(ctx, "INSERT INTO telemetry_items VALUES (1, 'literal-canary')"); err == nil {
		t.Fatal("expected real PostgreSQL unique violation")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("cannot roll back failed transaction")
	}
	end(false)
	if err := shutdown(); err != nil {
		t.Fatal("cannot flush SDK")
	}
	seen := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal(err)
		}
		seen[record["event"].(string)] = true
		if record["status"] == "failed" && record["sqlstate"] == "23505" {
			seen["unique_violation"] = true
		}
		if record["trace_id"] != tracing.Current(ctx).TraceID {
			t.Fatal("driver SQL lost trace context")
		}
		t.Logf("K5_LOG %s", line)
	}
	for _, event := range []string{"sql.query", "sql.exec.done", "tx.commit", "tx.rollback", "unique_violation"} {
		if !seen[event] {
			t.Fatalf("missing real driver event %s", event)
		}
	}
	if strings.Contains(logs.String()+spans.String(), "canary") {
		t.Fatal("real SQL telemetry exposed sensitive data")
	}
	for _, line := range bytes.Split(bytes.TrimSpace(spans.Bytes()), []byte("\n")) {
		t.Logf("K5_SPAN %s", line)
	}
}
