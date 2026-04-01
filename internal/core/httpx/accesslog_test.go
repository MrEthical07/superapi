package httpx

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

func newBufferLogger(t *testing.T) (*logx.Logger, *bytes.Buffer) {
	t.Helper()

	buf := &bytes.Buffer{}
	l, err := logx.NewWithWriter(logx.Config{Level: "debug", Format: "json"}, buf)
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	return l, buf
}

func parseSingleLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	content := strings.TrimSpace(buf.String())
	if content == "" {
		t.Fatalf("expected log entry, got empty buffer")
	}

	lines := strings.Split(content, "\n")
	line := strings.TrimSpace(lines[len(lines)-1])

	entry := map[string]any{}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("unmarshal log json: %v", err)
	}
	return entry
}

func TestAccessLogExclusionWorks(t *testing.T) {
	l, buf := newBufferLogger(t)

	cfg := config.AccessLogConfig{
		Enabled:      true,
		SampleRate:   1,
		ExcludePaths: []string{"/healthz", "/readyz", "/metrics"},
	}

	h := RequestID(AccessLog(cfg, l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if strings.TrimSpace(buf.String()) != "" {
		t.Fatalf("expected no access logs for excluded path, got: %s", buf.String())
	}
}

func TestAccessLogSamplingAndAlwaysLog5xx(t *testing.T) {
	l, buf := newBufferLogger(t)

	cfg := config.AccessLogConfig{Enabled: true, SampleRate: 0, SlowThreshold: 0}

	h := RequestID(AccessLog(cfg, l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil))
	if strings.TrimSpace(buf.String()) != "" {
		t.Fatalf("expected sampled-out request to produce no log, got: %s", buf.String())
	}

	h = RequestID(AccessLog(cfg, l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil))

	entry := parseSingleLog(t, buf)
	if got, _ := entry["status"].(float64); int(got) != http.StatusServiceUnavailable {
		t.Fatalf("status = %v, want %d", entry["status"], http.StatusServiceUnavailable)
	}
	if got, _ := entry["level"].(string); got != "error" {
		t.Fatalf("level = %q, want %q", got, "error")
	}
}

func TestAccessLogSlowRequestAlwaysLogs(t *testing.T) {
	l, buf := newBufferLogger(t)

	cfg := config.AccessLogConfig{Enabled: true, SampleRate: 0, SlowThreshold: 2 * time.Millisecond}

	h := RequestID(AccessLog(cfg, l)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/slow", nil))

	entry := parseSingleLog(t, buf)
	if got, _ := entry["level"].(string); got != "warn" {
		t.Fatalf("level = %q, want %q", got, "warn")
	}
}

func TestAccessLogRoutePatternAndSensitiveDefaults(t *testing.T) {
	l, buf := newBufferLogger(t)

	cfg := config.AccessLogConfig{Enabled: true, SampleRate: 1}

	r := chi.NewRouter()
	r.Use(CaptureRoutePattern)
	r.Get("/api/v1/tenants/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	h := RequestID(AccessLog(cfg, l)(r))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/abc123?token=secret", nil)
	req.Header.Set("Authorization", "Bearer top-secret")
	req.Header.Set("Cookie", "session=secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	entry := parseSingleLog(t, buf)
	if got, _ := entry["route"].(string); got != "/api/v1/tenants/{id}" {
		t.Fatalf("route = %q, want %q", got, "/api/v1/tenants/{id}")
	}
	if got, _ := entry["method"].(string); got != http.MethodGet {
		t.Fatalf("method = %q, want %q", got, http.MethodGet)
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Fatalf("expected duration_ms field")
	}
	if _, ok := entry["request_id"]; !ok {
		t.Fatalf("expected request_id field")
	}

	logLine := buf.String()
	if strings.Contains(logLine, "Bearer top-secret") || strings.Contains(logLine, "session=secret") {
		t.Fatalf("log line contains sensitive header values: %s", logLine)
	}
	if strings.Contains(logLine, "token=secret") {
		t.Fatalf("log line contains query string unexpectedly: %s", logLine)
	}
	if strings.Contains(logLine, "/api/v1/tenants/abc123") {
		t.Fatalf("log line contains raw path unexpectedly: %s", logLine)
	}
}

func TestAccessLogNotFoundRouteLabel(t *testing.T) {
	l, buf := newBufferLogger(t)

	cfg := config.AccessLogConfig{Enabled: true, SampleRate: 1}

	r := chi.NewRouter()
	r.Use(CaptureRoutePattern)

	h := RequestID(AccessLog(cfg, l)(r))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))

	entry := parseSingleLog(t, buf)
	if got, _ := entry["route"].(string); got != "not_found" {
		t.Fatalf("route = %q, want %q", got, "not_found")
	}
	if got, _ := entry["status"].(float64); int(got) != http.StatusNotFound {
		t.Fatalf("status = %v, want %d", entry["status"], http.StatusNotFound)
	}
}

func TestShouldSampleRequestDeterministic(t *testing.T) {
	rid := "req-123456789"

	v1 := shouldSampleRequest(rid, 0.05)
	v2 := shouldSampleRequest(rid, 0.05)
	if v1 != v2 {
		t.Fatalf("expected deterministic sampling for same request id")
	}

	if !shouldSampleRequest(rid, 1) {
		t.Fatalf("sample rate 1 must always sample")
	}
	if shouldSampleRequest(rid, 0) {
		t.Fatalf("sample rate 0 must never sample")
	}
}

func TestSampleKeyFromRequestIDHexPrefix(t *testing.T) {
	lower := "0123456789abcdef-tail"
	upper := "0123456789ABCDEF-tail"

	lowerKey := sampleKeyFromRequestID(lower)
	upperKey := sampleKeyFromRequestID(upper)

	const expected uint64 = 0x0123456789abcdef
	if lowerKey != expected {
		t.Fatalf("lower key=%x want=%x", lowerKey, expected)
	}
	if upperKey != expected {
		t.Fatalf("upper key=%x want=%x", upperKey, expected)
	}
}

func TestSampleKeyFromRequestIDFallbackDeterministic(t *testing.T) {
	rid := "req-1234"

	v1 := sampleKeyFromRequestID(rid)
	v2 := sampleKeyFromRequestID(rid)
	if v1 != v2 {
		t.Fatalf("expected deterministic fallback key for non-hex request id")
	}
}

func TestAccessLogTimeoutStatus(t *testing.T) {
	l, buf := newBufferLogger(t)

	cfg := config.AccessLogConfig{Enabled: true, SampleRate: 1}

	h := RequestID(RequestTimeout(15 * time.Millisecond)(AccessLog(cfg, l)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/slow-timeout", nil))

	entry := parseSingleLog(t, buf)
	if got, _ := entry["status"].(float64); int(got) != http.StatusGatewayTimeout {
		t.Fatalf("status = %v, want %d", entry["status"], http.StatusGatewayTimeout)
	}
}
