package tracing

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// NewOpenTelemetry configures console export and adds OTLP/HTTP when an
// endpoint is set. The caller installs the adapter and owns SDK shutdown.
func NewOpenTelemetry(ctx context.Context, service string, output io.Writer) (Provider, func(context.Context) error, error) {
	if output == nil {
		output = os.Stdout
	}
	options := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service))),
		sdktrace.WithBatcher(&consoleExporter{output: json.NewEncoder(output), service: service}),
	}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, nil, err
		}
		options = append(options, sdktrace.WithBatcher(exporter))
	}
	provider := sdktrace.NewTracerProvider(options...)
	return otelProvider{tracer: provider.Tracer("tocatdz")}, provider.Shutdown, nil
}

type otelProvider struct{ tracer trace.Tracer }

func (p otelProvider) Start(ctx context.Context, parent SpanContext, name string, kind Kind) (SpanContext, func(bool)) {
	if parent.TraceID != "" {
		tid, traceErr := trace.TraceIDFromHex(parent.TraceID)
		sid, spanErr := trace.SpanIDFromHex(parent.SpanID)
		flags, flagsErr := strconv.ParseUint(parent.Flags, 16, 8)
		state, stateErr := trace.ParseTraceState(parent.State)
		if traceErr == nil && spanErr == nil && flagsErr == nil && stateErr == nil {
			parentContext := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID: tid, SpanID: sid, TraceFlags: trace.TraceFlags(flags), TraceState: state, Remote: parent.Remote,
			})
			ctx = trace.ContextWithSpanContext(ctx, parentContext)
		}
	}
	kinds := [...]trace.SpanKind{trace.SpanKindInternal, trace.SpanKindServer, trace.SpanKindProducer, trace.SpanKindConsumer, trace.SpanKindClient}
	_, span := p.tracer.Start(ctx, name, trace.WithSpanKind(kinds[kind]))
	current := span.SpanContext()
	return SpanContext{TraceID: current.TraceID().String(), SpanID: current.SpanID().String(), Flags: current.TraceFlags().String(),
		State: current.TraceState().String()}, func(failed bool) {
		if failed {
			span.SetStatus(codes.Error, "operation failed")
		}
		span.End()
	}
}

type consoleExporter struct {
	mu      sync.Mutex
	output  *json.Encoder
	service string
}

func (e *consoleExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, span := range spans {
		if err := ctx.Err(); err != nil {
			return err
		}
		record := map[string]any{
			"service": e.service, "name": span.Name(), "kind": span.SpanKind().String(), "status": span.Status().Code.String(),
			"trace_id": span.SpanContext().TraceID().String(), "span_id": span.SpanContext().SpanID().String(),
			"started_at": span.StartTime().UTC(), "duration_ms": float64(span.EndTime().Sub(span.StartTime())) / 1e6,
		}
		if parent := span.Parent(); parent.IsValid() {
			record["parent_span_id"] = parent.SpanID().String()
		}
		if err := e.output.Encode(record); err != nil {
			return err
		}
	}
	return nil
}

func (*consoleExporter) Shutdown(context.Context) error { return nil }
