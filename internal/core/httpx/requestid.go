package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/requestid"
)

const maxRequestIDLen = 64

// RequestIDFromContext returns request id stored by RequestID middleware.
func RequestIDFromContext(ctx context.Context) string {
	return requestid.FromContext(ctx)
}

// RequestID injects X-Request-Id header and request context value.
//
// Behavior:
// - Accepts incoming X-Request-Id when format is valid
// - Generates cryptographically random ID when missing/invalid
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := normalizeRequestID(r.Header.Get("X-Request-Id"))
		if rid == "" {
			rid = newRequestID()
		}

		w.Header().Set("X-Request-Id", rid)
		ctx := requestid.WithContext(r.Context(), rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// extremely unlikely; fallback to deterministic-looking zero string is still valid format
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b[:])
}

func normalizeRequestID(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > maxRequestIDLen {
		return ""
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if c >= 'a' && c <= 'z' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			continue
		}
		if c >= '0' && c <= '9' {
			continue
		}
		switch c {
		case '-', '_', '.':
			continue
		default:
			return ""
		}
	}
	return trimmed
}
