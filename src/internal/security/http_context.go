package security

import "context"

type requestIDKey struct{}

// WithRequestID is for trusted server middleware only. Never copy an inbound
// X-Request-ID into this context; generate it at the outermost HTTP boundary.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
