package httpx

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/response"
)

// RequestTimeout enforces per-request context deadline for downstream handlers.
//
// Behavior:
// - Disabled when timeout <= 0
// - Returns 504 timeout envelope when deadline expires before any response header is written
func RequestTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	if timeout <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldBypassRequestTimeout(r) {
				next.ServeHTTP(w, r)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			tw := &timeoutResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(tw, r.WithContext(ctx))

			if errors.Is(ctx.Err(), context.DeadlineExceeded) && !tw.wroteHeader {
				rid := RequestIDFromContext(ctx)
				response.Error(w, apperr.New(apperr.CodeTimeout, http.StatusGatewayTimeout, "request timed out"), rid)
			}
		})
	}
}

func shouldBypassRequestTimeout(r *http.Request) bool {
	if r == nil {
		return false
	}

	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return true
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return true
	}

	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, "text/event-stream")
}

type timeoutResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

// WriteHeader records status and forwards header write.
func (w *timeoutResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write marks header as written and forwards body bytes.
func (w *timeoutResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// SetRoutePattern forwards route-pattern propagation when supported.
func (w *timeoutResponseWriter) SetRoutePattern(pattern string) {
	if setter, ok := w.ResponseWriter.(interface{ SetRoutePattern(string) }); ok {
		setter.SetRoutePattern(pattern)
	}
}

// Flush forwards flush capability when supported by underlying writer.
func (w *timeoutResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack forwards connection hijack when supported.
func (w *timeoutResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

// Push forwards HTTP/2 server push when supported.
func (w *timeoutResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}
