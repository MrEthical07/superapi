package httpx

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
	coretracing "github.com/MrEthical07/superapi/internal/core/tracing"
)

type benchParseRequest struct {
	Duration string `json:"duration"`
}

func (r benchParseRequest) Validate() error {
	if strings.TrimSpace(r.Duration) == "" {
		return errors.New("duration is required")
	}
	return nil
}

type benchParseResponse struct {
	Nanoseconds int64 `json:"nanoseconds"`
}

func newBenchLogger(b *testing.B) *logx.Logger {
	b.Helper()
	logger, err := logx.NewWithWriter(logx.Config{Level: "error", Format: "json"}, io.Discard)
	if err != nil {
		b.Fatalf("new logger: %v", err)
	}
	return logger
}

func BenchmarkAssembleGlobalMiddleware_Healthz(b *testing.B) {
	logger := newBenchLogger(b)
	cfg := config.HTTPMiddlewareConfig{
		RequestIDEnabled:       true,
		RecovererEnabled:       true,
		MaxBodyBytes:           1 << 20,
		SecurityHeadersEnabled: true,
		RequestTimeout:         5 * time.Second,
		AccessLog: config.AccessLogConfig{
			Enabled: false,
		},
		ClientIP: config.ClientIPConfig{},
		CORS:     config.CORSConfig{Enabled: false},
	}

	mux := NewMux()
	mux.Handle(http.MethodGet, "/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	handler := AssembleGlobalMiddleware(mux, cfg, logger, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
		}
	}
}

func BenchmarkAssembleGlobalMiddleware_HealthzTracingEnabled(b *testing.B) {
	logger := newBenchLogger(b)
	cfg := config.HTTPMiddlewareConfig{
		RequestIDEnabled:       true,
		RecovererEnabled:       true,
		MaxBodyBytes:           1 << 20,
		SecurityHeadersEnabled: true,
		RequestTimeout:         5 * time.Second,
		AccessLog: config.AccessLogConfig{
			Enabled: false,
		},
		ClientIP: config.ClientIPConfig{},
		CORS:     config.CORSConfig{Enabled: false},
	}

	tracingSvc := coretracing.NewWithProvider(noop.NewTracerProvider(), nil)

	mux := NewMux()
	mux.Handle(http.MethodGet, "/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	handler := AssembleGlobalMiddleware(mux, cfg, logger, tracingSvc)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
		}
	}
}

func BenchmarkJSONWithRequest_ParseDuration(b *testing.B) {
	handler := Adapter(func(ctx *Context, req benchParseRequest) (benchParseResponse, error) {
		d, err := time.ParseDuration(req.Duration)
		if err != nil {
			return benchParseResponse{}, err
		}
		return benchParseResponse{Nanoseconds: d.Nanoseconds()}, nil
	})

	payload := []byte(`{"duration":"250ms"}`)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/system/parse-duration", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("status=%d want=%d body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}
	}
}

func BenchmarkShouldSampleRequest(b *testing.B) {
	requestID := "9f95bd04f36f4f77ba2f2f2e6988db7f"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = shouldSampleRequest(requestID, 0.10)
	}
}
