package worker

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/migration"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/logging"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/tracing"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

type capturedSpans struct {
	count          int
	consumerParent tracing.SpanContext
}

func (p *capturedSpans) Start(_ context.Context, parent tracing.SpanContext, _ string, kind tracing.Kind) (tracing.SpanContext, func(bool)) {
	if kind == tracing.Consumer {
		p.consumerParent = parent
	}
	p.count++
	return tracing.SpanContext{TraceID: parent.TraceID, SpanID: fmt.Sprintf("%016x", p.count), Flags: "01"}, func(bool) {}
}

func TestWorkerRestoresQueueTraceAndCorrelatesLogs(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const producer = "00f067aa0ba902b7"
	provider := &capturedSpans{}
	tracing.Install(provider)
	t.Cleanup(func() { tracing.Install(nil) })
	lease := leaseAt(1, 3)
	lease.Job.TraceContext = security.NewSecret(`{"traceparent":"00-` + traceID + "-" + producer + `-01"}`)
	lease.Job.RequestID = "original-request"
	store := &fakeStore{lease: lease}
	runner := fakeRunner{result: migration.RunResult{Status: migration.JobSucceeded, Execution: &migration.Execution{}}}
	w := newTestWorker(t, store, fakeCoordinator{}, fakeRevalidator{}, runner)
	var records bytes.Buffer
	w.cfg.Logger = logging.New(&records)
	if worked, err := w.ProcessOne(context.Background()); !worked || err != nil {
		t.Fatalf("job worked=%t error=%v", worked, err)
	}
	if provider.consumerParent.TraceID != traceID || provider.consumerParent.SpanID != producer || !provider.consumerParent.Remote {
		t.Fatal("worker did not start under the queue producer")
	}
	for _, field := range []string{`"trace_id":"` + traceID + `"`, `"request_id":"original-request"`,
		`"event":"job.claimed"`, `"event":"history.appended"`, `"actor":"worker:test"`} {
		if !strings.Contains(records.String(), field) {
			t.Errorf("worker log lost correlation field %s", field)
		}
	}
	if strings.Contains(records.String(), "traceparent") {
		t.Fatal("worker logs included the raw queue carrier")
	}
}
