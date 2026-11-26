package tracing

import (
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"sync/atomic"
)

type Kind uint8

const (
	Internal Kind = iota
	Server
	Producer
	Consumer
	Client
)

type SpanContext struct {
	TraceID, SpanID, Flags, State string
	Remote                        bool
}

type Provider interface {
	Start(context.Context, SpanContext, string, Kind) (SpanContext, func(bool))
}

type providerRef struct{ provider Provider }

var active atomic.Pointer[providerRef]

// Install installs a process provider. Its owner shuts it down at process exit.
func Install(provider Provider) { active.Store(&providerRef{provider}) }

type spanKey struct{}

func Current(ctx context.Context) SpanContext {
	value, _ := ctx.Value(spanKey{}).(SpanContext)
	return value
}

func Start(ctx context.Context, name string, kind Kind) (context.Context, func(failed bool)) {
	config := active.Load()
	if config == nil || config.provider == nil {
		if Current(ctx).Remote {
			ctx = context.WithValue(ctx, spanKey{}, SpanContext{})
		}
		return ctx, func(bool) {}
	}
	span, end := config.provider.Start(ctx, Current(ctx), name, kind)
	return context.WithValue(ctx, spanKey{}, span), end
}

const carrierLimit = 4096

type carrier struct {
	TraceParent string `json:"traceparent"`
	TraceState  string `json:"tracestate,omitempty"`
}

var traceParent = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})$`)
var stateKey = regexp.MustCompile(`^(?:[a-z][a-z0-9_*/-]{0,255}|[a-z0-9][a-z0-9_*/-]{0,240}@[a-z][a-z0-9_*/-]{0,13})$`)

func Inject(ctx context.Context) string {
	span := Current(ctx)
	value := carrier{TraceParent: "00-" + span.TraceID + "-" + span.SpanID + "-" + span.Flags, TraceState: span.State}
	if parseParent(value) == (SpanContext{}) {
		return ""
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func Extract(raw string) context.Context { return Restore(context.Background(), raw) }

// Restore preserves the consumer's cancellation and replaces its trace parent.
func Restore(ctx context.Context, raw string) context.Context {
	return context.WithValue(ctx, spanKey{}, decodeCarrier(raw))
}

func decodeCarrier(raw string) SpanContext {
	if len(raw) == 0 || len(raw) > carrierLimit {
		return SpanContext{}
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return SpanContext{}
	}
	seen := map[string]bool{}
	var value carrier
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return SpanContext{}
		}
		seen[key] = true
		var field *string
		if decoder.Decode(&field) != nil || field == nil {
			return SpanContext{}
		}
		switch key {
		case "traceparent":
			value.TraceParent = *field
		case "tracestate":
			value.TraceState = *field
		default:
			return SpanContext{}
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return SpanContext{}
	}
	if _, err := decoder.Token(); err != io.EOF {
		return SpanContext{}
	}
	return parseParent(value)
}

func parseParent(value carrier) SpanContext {
	fields := traceParent.FindStringSubmatch(value.TraceParent)
	if fields == nil || strings.Trim(fields[1], "0") == "" || strings.Trim(fields[2], "0") == "" || !validState(value.TraceState) {
		return SpanContext{}
	}
	return SpanContext{TraceID: fields[1], SpanID: fields[2], Flags: fields[3], State: value.TraceState, Remote: true}
}

func validState(value string) bool {
	if value == "" {
		return true
	}
	members := strings.Split(value, ",")
	if len(value) > 512 || len(members) > 32 {
		return false
	}
	keys := map[string]bool{}
	for _, member := range members {
		key, field, ok := strings.Cut(strings.Trim(member, " \t"), "=")
		if !ok || keys[key] || !stateKey.MatchString(key) || len(field) == 0 || len(field) > 256 || field[len(field)-1] == ' ' {
			return false
		}
		for _, ch := range field {
			if ch < ' ' || ch > '~' || ch == '=' {
				return false
			}
		}
		keys[key] = true
	}
	return true
}
