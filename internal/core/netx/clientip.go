package netx

import (
	"context"
	"strings"
)

type clientIPKey struct{}

// WithClientIP stores the client IP in the context.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

// ClientIPFromContext returns the client IP from the context, if present.
func ClientIPFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(clientIPKey{}).(string)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}
