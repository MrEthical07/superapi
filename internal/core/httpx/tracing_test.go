package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	tracetest "go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	coretracing "github.com/MrEthical07/superapi/internal/core/tracing"
)

func TestTracingMiddlewareDisabledNoSpans(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer tp.Shutdown(t.Context())

	oldProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(oldProvider)

	h := Tracing(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if got := len(rec.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
}

func TestTracingMiddlewareRoutePatternAndAttributes(t *testing.T) {
	res := resource.NewWithAttributes(semconv.SchemaURL)
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(rec),
	)
	defer tp.Shutdown(t.Context())

	oldProvider := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		otel.SetTracerProvider(oldProvider)
		otel.SetTextMapPropagator(oldProp)
	}()

	svc := coretracing.NewWithProvider(tp, tp.Shutdown)

	r := chi.NewRouter()
	r.Use(CaptureRoutePattern)
	r.Get("/api/v1/tenants/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	h := RequestID(Tracing(svc)(r))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tenants/abc", nil))

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	span := spans[0]

	if got := span.Name(); got != "GET /api/v1/tenants/{id}" {
		t.Fatalf("span name = %q, want %q", got, "GET /api/v1/tenants/{id}")
	}

	attr := span.Attributes()
	assertHasAttr(t, attr, "http.method", "GET")
	assertHasAttr(t, attr, "http.route", "/api/v1/tenants/{id}")
	assertHasAttrInt(t, attr, "http.status_code", http.StatusServiceUnavailable)
	assertAttrPresent(t, attr, "request.id")
}

func TestTracingMiddlewareHonorsTraceparent(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer tp.Shutdown(t.Context())

	oldProvider := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		otel.SetTracerProvider(oldProvider)
		otel.SetTextMapPropagator(oldProp)
	}()

	svc := coretracing.NewWithProvider(tp, tp.Shutdown)

	r := chi.NewRouter()
	r.Use(CaptureRoutePattern)
	r.Get("/system/parse-duration", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := Tracing(svc)(r)
	req := httptest.NewRequest(http.MethodGet, "/system/parse-duration", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	if got := spans[0].Parent().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("parent trace id = %s, want %s", got, "4bf92f3577b34da6a3ce929d0e0e4736")
	}
}

func TestTracingWithExcludesSkipsConfiguredPath(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer tp.Shutdown(t.Context())

	oldProvider := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		otel.SetTracerProvider(oldProvider)
		otel.SetTextMapPropagator(oldProp)
	}()

	svc := coretracing.NewWithProvider(tp, tp.Shutdown)

	h := TracingWithExcludes(svc, []string{"/healthz"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if got := len(rec.Ended()); got != 0 {
		t.Fatalf("ended spans = %d, want 0", got)
	}
}

func assertHasAttr(t *testing.T, attrs []attribute.KeyValue, key string, want string) {
	t.Helper()
	for _, a := range attrs {
		if string(a.Key) == key && a.Value.AsString() == want {
			return
		}
	}
	t.Fatalf("missing attribute %s=%s", key, want)
}

func assertHasAttrInt(t *testing.T, attrs []attribute.KeyValue, key string, want int) {
	t.Helper()
	for _, a := range attrs {
		if string(a.Key) == key && int(a.Value.AsInt64()) == want {
			return
		}
	}
	t.Fatalf("missing attribute %s=%d", key, want)
}

func assertAttrPresent(t *testing.T, attrs []attribute.KeyValue, key string) {
	t.Helper()
	for _, a := range attrs {
		if string(a.Key) == key {
			return
		}
	}
	t.Fatalf("missing attribute %s", key)
}
