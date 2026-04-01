package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type routePatternSetter interface {
	SetRoutePattern(pattern string)
}

// CaptureRoutePattern records the resolved chi route pattern on response writer wrappers.
func CaptureRoutePattern(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		setter, ok := w.(routePatternSetter)
		if !ok {
			return
		}

		rctx := chi.RouteContext(r.Context())
		if rctx == nil {
			return
		}

		if pattern := rctx.RoutePattern(); pattern != "" {
			setter.SetRoutePattern(pattern)
		}
	})
}

// RoutePattern resolves a stable route label for instrumentation.
func RoutePattern(r *http.Request, statusCode int, capturedPattern string) string {
	if statusCode == http.StatusNotFound {
		return "not_found"
	}
	if capturedPattern != "" {
		return capturedPattern
	}

	rctx := chi.RouteContext(r.Context())
	if rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}

	return "unknown"
}
