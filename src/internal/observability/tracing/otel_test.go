package tracing

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func TestOpenTelemetryExportsQueueSpanChain(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	var output bytes.Buffer
	provider, shutdown, err := NewOpenTelemetry(context.Background(), "tocatdz-test", &output)
	if err != nil {
		t.Fatal(err)
	}
	Install(provider)
	t.Cleanup(func() { Install(nil) })
	httpCtx, endHTTP := Start(context.Background(), "HTTP POST /api/v1/jobs", Server)
	enqueue, endEnqueue := Start(httpCtx, "job.enqueue", Producer)
	carrier := Inject(enqueue)
	endEnqueue(false)
	endHTTP(false)
	consumer, endConsumer := Start(Extract(carrier), "job.process", Consumer)
	_, endQuery := Start(consumer, "db.query", Client)
	endQuery(true)
	endConsumer(true)
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(cleanup); err != nil {
		t.Fatal(err)
	}
	exported := make(map[string]map[string]any)
	decoder := json.NewDecoder(&output)
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		exported[record["name"].(string)] = record
	}
	trace := Current(httpCtx).TraceID
	if len(exported) != 4 || trace == "" {
		t.Fatal("unexpected exported span count")
	}
	for _, record := range exported {
		if record["trace_id"] != trace {
			t.Fatal("trace ID changed between HTTP and worker")
		}
	}
	if exported["job.process"]["parent_span_id"] != exported["job.enqueue"]["span_id"] ||
		exported["db.query"]["parent_span_id"] != exported["job.process"]["span_id"] ||
		exported["db.query"]["status"] != "Error" {
		t.Fatal("exporter lost parent IDs or error status")
	}
}
