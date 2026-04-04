package httpx

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/MrEthical07/superapi/internal/core/tracing"
)

// Tracing wraps requests in OpenTelemetry server spans when tracing is enabled.
//
// Behavior:
// - Extracts incoming trace context from headers
// - Names spans with low-cardinality route patterns
// - Records status code and request metadata attributes
func Tracing(svc *tracing.Service) func(http.Handler) http.Handler {
	return TracingWithExcludes(svc, nil)
}

// TracingWithExcludes skips tracing spans for exact path matches.
func TracingWithExcludes(svc *tracing.Service, excludePaths []string) func(http.Handler) http.Handler {
	if svc == nil || !svc.Enabled() {
		return func(next http.Handler) http.Handler { return next }
	}

	tracer := svc.Tracer()
	prop := otel.GetTextMapPropagator()
	excludes := normalizeTracingPathSet(excludePaths)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, skip := excludes[strings.TrimSpace(r.URL.Path)]; skip {
				next.ServeHTTP(w, r)
				return
			}

			ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			spanName := r.Method + " unknown"
			ctx, span := tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))

			tw := &tracingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			r2 := r.WithContext(ctx)

			defer func() {
				if rec := recover(); rec != nil {
					span.RecordError(fmt.Errorf("panic recovered"))
					span.SetStatus(codes.Error, "panic")
					span.End()
					panic(rec)
				}

				statusCode := tw.statusCode
				route := RoutePattern(r2, statusCode, tw.routePattern)
				span.SetName(r2.Method + " " + route)

				attrs := []attribute.KeyValue{
					attribute.String("http.method", r2.Method),
					attribute.String("http.route", route),
					attribute.Int("http.status_code", statusCode),
				}

				if reqID := RequestIDFromContext(r2.Context()); reqID != "" {
					attrs = append(attrs, attribute.String("request.id", reqID))
				}

				if host, port := serverAddress(r2.Host); host != "" {
					attrs = append(attrs, attribute.String("server.address", host))
					if port > 0 {
						attrs = append(attrs, attribute.Int("server.port", port))
					}
				}

				span.SetAttributes(attrs...)

				if statusCode >= http.StatusInternalServerError {
					span.RecordError(fmt.Errorf("http status %d", statusCode))
					span.SetStatus(codes.Error, "server error")
				} else {
					span.SetStatus(codes.Ok, "")
				}
				span.End()
			}()

			next.ServeHTTP(tw, r2)
		})
	}
}

func normalizeTracingPathSet(paths []string) map[string]struct{} {
	if len(paths) == 0 {
		return nil
	}

	out := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		out[trimmed] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func serverAddress(hostport string) (string, int) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "", 0
	}

	host, portRaw, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport, 0
	}

	port, err := strconv.Atoi(portRaw)
	if err != nil {
		return host, 0
	}
	return host, port
}

type tracingResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	routePattern string
}

// WriteHeader captures status code and forwards header write.
func (w *tracingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// SetRoutePattern stores route pattern used to name tracing spans.
func (w *tracingResponseWriter) SetRoutePattern(pattern string) {
	w.routePattern = pattern
	if setter, ok := w.ResponseWriter.(interface{ SetRoutePattern(string) }); ok {
		setter.SetRoutePattern(pattern)
	}
}

// Flush forwards flush capability when supported by underlying writer.
func (w *tracingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack forwards connection hijack when supported.
func (w *tracingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

// Push forwards HTTP/2 server push when supported.
func (w *tracingResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}
