package metrics

import (
	"bufio"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/readiness"
)

const (
	namespace = "superapi"
)

// Service owns Prometheus collectors and HTTP instrumentation helpers.
type Service struct {
	enabled bool
	path    string

	excludePaths map[string]struct{}

	registry *prometheus.Registry
	handler  http.Handler

	httpRequestsTotal      *prometheus.CounterVec
	httpRequestDurationSec *prometheus.HistogramVec
	httpInFlight           prometheus.Gauge
	rateLimitRequests      *prometheus.CounterVec
	cacheOperations        *prometheus.CounterVec

	readyGauge     prometheus.Gauge
	dependencyRead *prometheus.GaugeVec
}

// New builds a metrics service and registers collectors when enabled.
func New(cfg config.MetricsConfig, pool *pgxpool.Pool) (*Service, error) {
	if !cfg.Enabled {
		return &Service{enabled: false, path: cfg.Path, excludePaths: normalizePathSet(cfg.ExcludePaths)}, nil
	}

	r := prometheus.NewRegistry()

	httpRequestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests processed.",
		},
		[]string{"method", "route", "status"},
	)

	httpRequestDurationSec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "route", "status"},
	)

	httpInFlight := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "http_in_flight_requests",
			Help:      "Current number of in-flight HTTP requests.",
		},
	)

	rateLimitRequests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "rate_limit_requests_total",
			Help:      "Rate limit outcomes by route.",
		},
		[]string{"route", "outcome"},
	)

	cacheOperations := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_operations_total",
			Help:      "Cache outcomes by route.",
		},
		[]string{"route", "outcome"},
	)

	readyGauge := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "ready",
			Help:      "Readiness status of the service (1=ready, 0=not_ready).",
		},
	)

	dependencyRead := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "dependency_ready",
			Help:      "Dependency readiness state as one-hot status labels (ok/disabled/error).",
		},
		[]string{"dependency", "status"},
	)

	if err := r.Register(httpRequestsTotal); err != nil {
		return nil, err
	}
	if err := r.Register(httpRequestDurationSec); err != nil {
		return nil, err
	}
	if err := r.Register(httpInFlight); err != nil {
		return nil, err
	}
	if err := r.Register(rateLimitRequests); err != nil {
		return nil, err
	}
	if err := r.Register(cacheOperations); err != nil {
		return nil, err
	}
	if err := r.Register(readyGauge); err != nil {
		return nil, err
	}
	if err := r.Register(dependencyRead); err != nil {
		return nil, err
	}

	if pool != nil {
		if err := r.Register(newPGXPoolCollector(pool)); err != nil {
			return nil, err
		}
	}

	return &Service{
		enabled:                true,
		path:                   cfg.Path,
		excludePaths:           normalizePathSet(cfg.ExcludePaths),
		registry:               r,
		handler:                promhttp.HandlerFor(r, promhttp.HandlerOpts{}),
		httpRequestsTotal:      httpRequestsTotal,
		httpRequestDurationSec: httpRequestDurationSec,
		httpInFlight:           httpInFlight,
		rateLimitRequests:      rateLimitRequests,
		cacheOperations:        cacheOperations,
		readyGauge:             readyGauge,
		dependencyRead:         dependencyRead,
	}, nil
}

// Enabled reports whether metrics collection is active.
func (s *Service) Enabled() bool {
	return s != nil && s.enabled
}

// Path returns the route path where metrics are exposed.
func (s *Service) Path() string {
	if s == nil || s.path == "" {
		return "/metrics"
	}
	return s.path
}

// Handler returns the metrics endpoint handler.
func (s *Service) Handler() http.Handler {
	if s == nil || !s.enabled || s.handler == nil {
		return http.NotFoundHandler()
	}
	return s.handler
}

// InstrumentHTTP records request counters/latency and in-flight gauge.
func (s *Service) InstrumentHTTP(next http.Handler) http.Handler {
	if s == nil || !s.enabled {
		return next
	}

	metricsPath := s.Path()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == metricsPath {
			next.ServeHTTP(w, r)
			return
		}
		if s.isExcludedPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		s.httpInFlight.Inc()
		defer s.httpInFlight.Dec()

		ww := &statusCapturingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(ww, r)

		status := strconv.Itoa(ww.statusCode)
		route := ww.routePattern
		if route == "" {
			route = routePattern(r, ww.statusCode)
		}
		method := r.Method

		s.httpRequestsTotal.WithLabelValues(method, route, status).Inc()
		s.httpRequestDurationSec.WithLabelValues(method, route, status).Observe(time.Since(start).Seconds())
	})
}

// CaptureRoutePattern stores resolved chi route patterns for later metrics labels.
func (s *Service) CaptureRoutePattern(next http.Handler) http.Handler {
	if s == nil || !s.enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		setter, ok := w.(interface{ SetRoutePattern(string) })
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

// ObserveReadiness updates readiness gauges from a readiness report.
func (s *Service) ObserveReadiness(report readiness.Report) {
	if s == nil || !s.enabled {
		return
	}

	if report.Status == readiness.StatusReady {
		s.readyGauge.Set(1)
	} else {
		s.readyGauge.Set(0)
	}

	s.dependencyRead.Reset()
	for dep, st := range report.Dependencies {
		s.dependencyRead.WithLabelValues(dep, readiness.DependencyOK).Set(boolToFloat64(st.Status == readiness.DependencyOK))
		s.dependencyRead.WithLabelValues(dep, readiness.DependencyDisabled).Set(boolToFloat64(st.Status == readiness.DependencyDisabled))
		s.dependencyRead.WithLabelValues(dep, readiness.DependencyError).Set(boolToFloat64(st.Status == readiness.DependencyError))
	}
}

// ObserveRateLimit increments route-level rate-limit outcome counters.
func (s *Service) ObserveRateLimit(route, outcome string) {
	if s == nil || !s.enabled || s.rateLimitRequests == nil {
		return
	}
	r := route
	if r == "" {
		r = "unknown"
	}
	o := outcome
	if o == "" {
		o = "unknown"
	}
	s.rateLimitRequests.WithLabelValues(r, o).Inc()
}

// ObserveCache increments route-level cache outcome counters.
func (s *Service) ObserveCache(route, outcome string) {
	if s == nil || !s.enabled || s.cacheOperations == nil {
		return
	}
	r := route
	if r == "" {
		r = "unknown"
	}
	o := outcome
	if o == "" {
		o = "unknown"
	}
	s.cacheOperations.WithLabelValues(r, o).Inc()
}

func (s *Service) isExcludedPath(path string) bool {
	if s == nil || len(s.excludePaths) == 0 {
		return false
	}
	_, ok := s.excludePaths[strings.TrimSpace(path)]
	return ok
}

func normalizePathSet(paths []string) map[string]struct{} {
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

func boolToFloat64(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func routePattern(r *http.Request, statusCode int) string {
	if statusCode == http.StatusNotFound {
		return "not_found"
	}

	rctx := chi.RouteContext(r.Context())
	if rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unknown"
}

type statusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	routePattern string
}

// WriteHeader captures status code before delegating to the wrapped writer.
func (w *statusCapturingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// SetRoutePattern stores resolved route pattern for metrics labeling.
func (w *statusCapturingResponseWriter) SetRoutePattern(pattern string) {
	w.routePattern = pattern
	if setter, ok := w.ResponseWriter.(interface{ SetRoutePattern(string) }); ok {
		setter.SetRoutePattern(pattern)
	}
}

// Flush forwards flush capability when supported by underlying writer.
func (w *statusCapturingResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack forwards connection hijack when supported.
func (w *statusCapturingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

// Push forwards HTTP/2 server push when supported.
func (w *statusCapturingResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}
