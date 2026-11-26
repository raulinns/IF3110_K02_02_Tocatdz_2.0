package tracing

import (
	"context"
	"strings"
	"testing"
)

func TestCarrierPreservesTraceParentAndCancellation(t *testing.T) {
	raw := `{"traceparent":"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01","tracestate":"vendor=test"}`
	ctx, cancel := context.WithCancel(context.Background())
	ctx = Restore(ctx, raw)
	cancel()
	if ctx.Err() != context.Canceled || !Current(ctx).Remote || Inject(ctx) != raw {
		t.Fatal("valid queue carrier or consumer cancellation was lost")
	}
	for _, invalid := range []string{
		"", strings.Repeat("x", 4097), raw + `{}`, `{"traceparent":null}`,
		strings.Replace(raw, `"traceparent":`, `"traceparent":"bad","traceparent":`, 1),
		strings.Replace(raw, "4bf92f3577b34da6a3ce929d0e0e4736", strings.Repeat("0", 32), 1),
		strings.Replace(raw, "vendor=test", "vendor=test,vendor=duplicate", 1),
		strings.Replace(raw, "tracestate", "baggage", 1),
	} {
		if Current(Extract(invalid)) != (SpanContext{}) {
			t.Fatal("invalid queue carrier accepted")
		}
	}
}
