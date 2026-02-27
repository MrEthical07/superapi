package httpx

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

func testLogger(t *testing.T) *logx.Logger {
	t.Helper()

	l, err := logx.New(logx.Config{Level: "error", Format: "json"})
	if err != nil {
		t.Fatalf("logger init failed: %v", err)
	}
	return l
}

func TestAssembleGlobalMiddleware_RecovererUsesRequestID(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	h := AssembleGlobalMiddleware(base, config.HTTPMiddlewareConfig{
		RequestIDEnabled: true,
		RecovererEnabled: true,
	}, testLogger(t), nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
	if got := rr.Header().Get("X-Request-Id"); got == "" {
		t.Fatalf("missing X-Request-Id header")
	}
	if !strings.Contains(rr.Body.String(), "request_id") {
		t.Fatalf("expected response body to include request_id")
	}
}

func TestAssembleGlobalMiddleware_SecurityHeaders(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := AssembleGlobalMiddleware(base, config.HTTPMiddlewareConfig{
		SecurityHeadersEnabled: true,
	}, testLogger(t), nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rr.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
}

func TestAssembleGlobalMiddleware_MaxBodyBytes(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	h := AssembleGlobalMiddleware(base, config.HTTPMiddlewareConfig{
		MaxBodyBytes: 8,
	}, testLogger(t), nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789"))
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestAssembleGlobalMiddleware_MaxBodyBytes_DoesNotBreakGet(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := AssembleGlobalMiddleware(base, config.HTTPMiddlewareConfig{
		MaxBodyBytes: 1,
	}, testLogger(t), nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestAssembleGlobalMiddleware_RequestTimeout(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	h := AssembleGlobalMiddleware(base, config.HTTPMiddlewareConfig{
		RequestIDEnabled: true,
		RequestTimeout:   20 * time.Millisecond,
	}, testLogger(t), nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusGatewayTimeout)
	}
	if got := rr.Header().Get("X-Request-Id"); got == "" {
		t.Fatalf("missing X-Request-Id header")
	}
}
