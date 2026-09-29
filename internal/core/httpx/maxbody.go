package httpx

import (
	"context"
	"net/http"
)

// DefaultJSONBodyLimit caps a JSON request body when no MaxBodyBytes
// middleware limit applies (HTTP_MIDDLEWARE_MAX_BODY_BYTES=0 disables that
// middleware, and handlers can also be mounted without it).
const DefaultJSONBodyLimit int64 = 1 << 20 // 1 MiB

type bodyLimitKey struct{}

// MaxBodyBytes limits request body size for requests that can carry bodies.
// The limit is also recorded on the request context so DecodeAndValidateJSON
// applies the same cap instead of its default.
func MaxBodyBytes(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldLimitBody(r) {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
				r = r.WithContext(context.WithValue(r.Context(), bodyLimitKey{}, limit))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// jsonBodyLimit returns the cap DecodeAndValidateJSON must enforce, and whether
// the request body is already capped by the MaxBodyBytes middleware.
func jsonBodyLimit(r *http.Request) (limit int64, capped bool) {
	if v, ok := r.Context().Value(bodyLimitKey{}).(int64); ok && v > 0 {
		return v, true
	}
	return DefaultJSONBodyLimit, false
}

func shouldLimitBody(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return r.ContentLength > 0 || len(r.TransferEncoding) > 0
	}
}
