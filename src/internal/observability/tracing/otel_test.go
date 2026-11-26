package tracing

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestOpenTelemetryExportsQueueSpanChain(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
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

func TestOpenTelemetryExportsToTraceSpecificEndpoint(t *testing.T) {
	requests := make(chan *collector.ExportTraceServiceRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/traces" || r.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Error("unexpected OTLP HTTP request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(r.Body)
		var request collector.ExportTraceServiceRequest
		if err != nil || proto.Unmarshal(body, &request) != nil {
			t.Error("invalid OTLP protobuf payload")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- &request
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer server.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", server.URL+"/v1/traces")
	var console bytes.Buffer
	shutdown, err := StartProcess(context.Background(), "tocatdz-test", &console)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown() })
	_, end := Start(context.Background(), "otlp-proof", Internal)
	end(true)
	if err := shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-requests:
		if len(request.ResourceSpans) != 1 || len(request.ResourceSpans[0].ScopeSpans) != 1 || len(request.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
			t.Fatal("unexpected OTLP span count")
		}
		span := request.ResourceSpans[0].ScopeSpans[0].Spans[0]
		var record map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(console.Bytes()), &record); err != nil {
			t.Fatal(err)
		}
		if span.Name != "otlp-proof" || span.Status.GetCode().String() != "STATUS_CODE_ERROR" || record["status"] != "Error" {
			t.Fatal("console and OTLP lost error status")
		}
	default:
		t.Fatal("trace-specific endpoint did not receive spans")
	}
}

func TestProcessShutdownFlushesAfterSignalCancellation(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	var output bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shutdown, err := StartProcess(ctx, "tocatdz-test", &output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown() })
	spanCtx, end := Start(ctx, "shutdown-proof", Internal)
	end(false)
	cancel()
	if err := shutdown(); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatal(err)
	}
	if record["trace_id"] != Current(spanCtx).TraceID || record["name"] != "shutdown-proof" {
		t.Fatal("shutdown lost a finished span")
	}
	ctx, end = Start(context.Background(), "after-shutdown", Internal)
	end(false)
	if Current(ctx).TraceID != "" {
		t.Fatal("shutdown left an active process provider")
	}
}
