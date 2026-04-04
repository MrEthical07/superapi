package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/MrEthical07/superapi/internal/core/config"
)

func BenchmarkInstrumentHTTP_RequestPath(b *testing.B) {
	svc, err := New(config.MetricsConfig{Enabled: true, Path: "/metrics"}, nil)
	if err != nil {
		b.Fatalf("new metrics service: %v", err)
	}

	r := chi.NewRouter()
	r.Use(svc.CaptureRoutePattern)
	r.Get("/api/v1/system/whoami", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := svc.InstrumentHTTP(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/whoami", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
		}
	}
}

func BenchmarkInstrumentHTTP_ExcludedPath(b *testing.B) {
	svc, err := New(config.MetricsConfig{Enabled: true, Path: "/metrics", ExcludePaths: []string{"/healthz"}}, nil)
	if err != nil {
		b.Fatalf("new metrics service: %v", err)
	}

	r := chi.NewRouter()
	r.Use(svc.CaptureRoutePattern)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := svc.InstrumentHTTP(r)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
		}
	}
}
