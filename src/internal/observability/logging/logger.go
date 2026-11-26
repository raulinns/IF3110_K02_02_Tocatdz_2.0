package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/observability/tracing"
	"github.com/raulinns/IF3110_K02_02_Tocatdz_2.0/internal/security"
)

func New(output io.Writer) *slog.Logger {
	return slog.New(handler{next: slog.NewJSONHandler(output, &slog.HandlerOptions{ReplaceAttr: redact})})
}

type loggerKey struct{}

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	logger, _ := ctx.Value(loggerKey{}).(*slog.Logger)
	return logger
}

type handler struct {
	next   slog.Handler
	bound  []slog.Attr
	groups []string
}

func (h handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h handler) Handle(ctx context.Context, r slog.Record) error {
	// Add correlation at the root regardless of application logger grouping.
	attrs := make(map[string]slog.Attr, len(h.bound)+r.NumAttrs())
	for _, a := range h.bound {
		attrs[a.Key] = merge(attrs[a.Key], a)
	}
	r.Attrs(func(a slog.Attr) bool {
		for _, grouped := range groupAttrs(h.groups, []slog.Attr{a}) {
			attrs[grouped.Key] = merge(attrs[grouped.Key], grouped)
		}
		return true
	})
	actor := any("system")
	if p, ok := security.PrincipalFromContext(ctx); ok {
		actor = string(p.UserID)
	}
	span := tracing.Current(ctx)
	defaults := []slog.Attr{
		slog.String("event", "application"), slog.Any("actor", actor), slog.Any("database_id", nil),
		slog.String("status", statusFor(r.Level)), slog.Any("request_id", optional(security.RequestIDFromContext(ctx))),
	}
	record := slog.NewRecord(r.Time.UTC(), r.Level, r.Message, r.PC)
	for _, field := range defaults {
		if supplied, ok := attrs[field.Key]; ok {
			if supplied.Value.Kind() != slog.KindString || supplied.Value.String() != "" {
				field = supplied
			}
		}
		delete(attrs, field.Key)
		record.AddAttrs(field)
	}
	record.AddAttrs(slog.Any("trace_id", optional(span.TraceID)), slog.Any("span_id", optional(span.SpanID)))
	for key, a := range attrs {
		if key != "trace_id" && key != "span_id" {
			record.AddAttrs(a)
		}
	}
	return h.next.Handle(ctx, record)
}

func merge(existing, incoming slog.Attr) slog.Attr {
	if existing.Value.Kind() == slog.KindGroup && incoming.Value.Kind() == slog.KindGroup {
		attrs := append([]slog.Attr(nil), existing.Value.Group()...)
		for _, attr := range incoming.Value.Group() {
			found := false
			for i, old := range attrs {
				if old.Key == attr.Key {
					attrs[i] = merge(old, attr)
					found = true
					break
				}
			}
			if !found {
				attrs = append(attrs, attr)
			}
		}
		incoming.Value = slog.GroupValue(attrs...)
	}
	return incoming
}

func (h handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	bound := append([]slog.Attr(nil), h.bound...)
	h.bound = append(bound, groupAttrs(h.groups, attrs)...)
	return h
}

func (h handler) WithGroup(name string) slog.Handler {
	if name != "" {
		h.groups = append(append([]string(nil), h.groups...), name)
	}
	return h
}

func groupAttrs(groups []string, attrs []slog.Attr) []slog.Attr {
	for i := len(groups) - 1; i >= 0; i-- {
		attrs = []slog.Attr{{Key: groups[i], Value: slog.GroupValue(attrs...)}}
	}
	return attrs
}

func statusFor(level slog.Level) string {
	if level >= slog.LevelWarn {
		return "failed"
	}
	return "succeeded"
}

func optional(value string) any {
	if value == "" {
		return nil
	}
	return value
}

var attrKeyNormalizer = strings.NewReplacer("-", "_", ".", "_")

func protectedKey(value string) bool {
	key := strings.ToLower(attrKeyNormalizer.Replace(value))
	switch key {
	case "password", "password_hash", "dsn", "database_url", "target_dsn", "cookie", "set_cookie", "authorization",
		"session", "session_id", "session_token", "csrf_token", "x_csrf_token", "trace_context",
		"token", "secret", "credential", "credentials", "request_body", "response_body":
		return true
	}
	return false
}

func redact(groups []string, attr slog.Attr) slog.Attr {
	if protectedKey(attr.Key) {
		return slog.String(attr.Key, "[REDACTED]")
	}
	for _, group := range groups {
		if protectedKey(group) {
			return slog.String(attr.Key, "[REDACTED]")
		}
	}
	key := strings.ToLower(attrKeyNormalizer.Replace(attr.Key))
	if key == "params" {
		if values, ok := attr.Value.Any().([]string); ok {
			masked := make([]string, len(values))
			for i := range masked {
				masked[i] = "[REDACTED]"
			}
			return slog.Any(attr.Key, masked)
		}
		return slog.String(attr.Key, "[REDACTED]")
	}
	if attr.Value.Kind() == slog.KindAny {
		switch attr.Value.Any().(type) {
		case nil, security.ID, slog.Level:
		default:
			return slog.String(attr.Key, "[REDACTED]")
		}
	}
	return attr
}
