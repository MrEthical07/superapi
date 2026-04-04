package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	io_prometheus_client "github.com/prometheus/client_model/go"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/readiness"
)

func TestInstrumentHTTPRecordsRouteMetrics(t *testing.T) {
	svc, err := New(config.MetricsConfig{Enabled: true, Path: "/metrics"}, nil)
	if err != nil {
		t.Fatalf("new metrics service: %v", err)
	}

	r := chi.NewRouter()
	r.Use(svc.CaptureRoutePattern)
	r.Get("/api/v1/tenants/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	h := svc.InstrumentHTTP(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/123", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, rr.Code)
	}

	assertMetricValue(t, svc, "superapi_http_requests_total",
		map[string]string{"method": http.MethodGet, "route": "/api/v1/tenants/{id}", "status": "201"},
		1,
	)
}

func TestInstrumentHTTPSkipsMetricsEndpoint(t *testing.T) {
	svc, err := New(config.MetricsConfig{Enabled: true, Path: "/metrics"}, nil)
	if err != nil {
		t.Fatalf("new metrics service: %v", err)
	}

	r := chi.NewRouter()
	r.Use(svc.CaptureRoutePattern)
	r.Get("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := svc.InstrumentHTTP(r)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	assertMetricValue(t, svc, "superapi_http_requests_total",
		map[string]string{"method": http.MethodGet, "route": "/metrics", "status": "200"},
		0,
	)
}

func TestInstrumentHTTPLabelsNotFound(t *testing.T) {
	svc, err := New(config.MetricsConfig{Enabled: true, Path: "/metrics"}, nil)
	if err != nil {
		t.Fatalf("new metrics service: %v", err)
	}

	r := chi.NewRouter()
	r.Use(svc.CaptureRoutePattern)

	h := svc.InstrumentHTTP(r)

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}

	assertMetricValue(t, svc, "superapi_http_requests_total",
		map[string]string{"method": http.MethodGet, "route": "not_found", "status": "404"},
		1,
	)
}

func TestInstrumentHTTPSkipsExcludedPath(t *testing.T) {
	svc, err := New(config.MetricsConfig{Enabled: true, Path: "/metrics", ExcludePaths: []string{"/healthz"}}, nil)
	if err != nil {
		t.Fatalf("new metrics service: %v", err)
	}

	r := chi.NewRouter()
	r.Use(svc.CaptureRoutePattern)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := svc.InstrumentHTTP(r)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	assertMetricValue(t, svc, "superapi_http_requests_total",
		map[string]string{"method": http.MethodGet, "route": "/healthz", "status": "200"},
		0,
	)
}

func TestObserveReadinessUpdatesGauges(t *testing.T) {
	svc, err := New(config.MetricsConfig{Enabled: true, Path: "/metrics"}, nil)
	if err != nil {
		t.Fatalf("new metrics service: %v", err)
	}

	svc.ObserveReadiness(readiness.Report{
		Status: readiness.StatusNotReady,
		Dependencies: map[string]readiness.DependencyStatus{
			"postgres": {Status: readiness.DependencyError, Message: "timeout"},
			"redis":    {Status: readiness.DependencyDisabled},
		},
	})

	assertMetricValue(t, svc, "superapi_ready", nil, 0)
	assertMetricValue(t, svc, "superapi_dependency_ready",
		map[string]string{"dependency": "postgres", "status": readiness.DependencyError},
		1,
	)
	assertMetricValue(t, svc, "superapi_dependency_ready",
		map[string]string{"dependency": "redis", "status": readiness.DependencyDisabled},
		1,
	)
}

func TestObserveRateLimitIncrementsCounter(t *testing.T) {
	svc, err := New(config.MetricsConfig{Enabled: true, Path: "/metrics"}, nil)
	if err != nil {
		t.Fatalf("new metrics service: %v", err)
	}

	svc.ObserveRateLimit("/api/v1/system/whoami", "allowed")
	svc.ObserveRateLimit("/api/v1/system/whoami", "blocked")

	assertMetricValue(t, svc, "superapi_rate_limit_requests_total",
		map[string]string{"route": "/api/v1/system/whoami", "outcome": "allowed"},
		1,
	)
	assertMetricValue(t, svc, "superapi_rate_limit_requests_total",
		map[string]string{"route": "/api/v1/system/whoami", "outcome": "blocked"},
		1,
	)
}

func TestObserveCacheIncrementsCounter(t *testing.T) {
	svc, err := New(config.MetricsConfig{Enabled: true, Path: "/metrics"}, nil)
	if err != nil {
		t.Fatalf("new metrics service: %v", err)
	}

	svc.ObserveCache("/api/v1/tenants/{id}", "hit")
	svc.ObserveCache("/api/v1/tenants/{id}", "miss")

	assertMetricValue(t, svc, "superapi_cache_operations_total",
		map[string]string{"route": "/api/v1/tenants/{id}", "outcome": "hit"},
		1,
	)
	assertMetricValue(t, svc, "superapi_cache_operations_total",
		map[string]string{"route": "/api/v1/tenants/{id}", "outcome": "miss"},
		1,
	)
}

func assertMetricValue(t *testing.T, svc *Service, metricName string, labels map[string]string, expected float64) {
	t.Helper()

	mfs, err := svc.registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}

		for _, m := range mf.GetMetric() {
			if !labelsMatch(m.GetLabel(), labels) {
				continue
			}

			actual := metricValue(m)
			if actual != expected {
				t.Fatalf("metric %s labels=%v expected %v got %v", metricName, labels, expected, actual)
			}
			return
		}
	}

	if expected == 0 {
		return
	}
	t.Fatalf("metric %s with labels %v not found", metricName, labels)
}

func labelsMatch(actual []*io_prometheus_client.LabelPair, expected map[string]string) bool {
	if len(expected) == 0 {
		return true
	}
	if len(actual) != len(expected) {
		return false
	}

	for _, label := range actual {
		name := label.GetName()
		value, ok := expected[name]
		if !ok || value != label.GetValue() {
			return false
		}
	}

	return true
}

func metricValue(m *io_prometheus_client.Metric) float64 {
	if m.GetCounter() != nil {
		return m.GetCounter().GetValue()
	}
	if m.GetGauge() != nil {
		return m.GetGauge().GetValue()
	}
	if m.GetHistogram() != nil {
		return float64(m.GetHistogram().GetSampleCount())
	}
	return 0
}
