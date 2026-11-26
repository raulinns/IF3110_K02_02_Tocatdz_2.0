package sqllog

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func TestPgxTracerRedactsSQLParametersAndErrors(t *testing.T) {
	var output bytes.Buffer
	ctx := logging.WithLogger(security.WithRequestID(context.Background(), "server-request"), logging.New(&output))
	ctx = logging.WithOperation(ctx, logging.Operation{Actor: "worker:test", Mutation: true, Batch: 2})
	tracer := Tracer{Kind: Target}
	query := tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{
		SQL:  "UPDATE rows SET text = 'literal-canary' WHERE id = $1",
		Args: []any{"parameter-canary"},
	})
	tracer.TraceQueryEnd(query, nil, pgx.TraceQueryEndData{Err: &pgconn.PgError{Code: "23505", Message: "driver-canary"}})
	if strings.Contains(output.String(), "canary") {
		t.Fatal("SQL logger exposed a literal, parameter or driver error")
	}
	decoder := json.NewDecoder(&output)
	for _, event := range []string{"sql.exec.start", "sql.exec.done"} {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["event"] != event || record["request_id"] != "server-request" || record["batch"] != float64(2) ||
			record["query"] != "UPDATE rows SET text = $2 WHERE id = $1" {
			t.Fatalf("unexpected event: %v", record)
		}
		if event == "sql.exec.done" && (record["sqlstate"] != "23505" || record["status"] != "failed" || record["rows"] != nil) {
			t.Fatal("failed query must expose only safe error metadata")
		}
	}
}
