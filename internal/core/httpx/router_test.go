package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewMuxNotFoundUsesAPIEnvelope(t *testing.T) {
	m := NewMux()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	m.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusNotFound)
	}
	if !strings.Contains(rr.Body.String(), `"code":"not_found"`) {
		t.Fatalf("expected not_found code, got body=%s", rr.Body.String())
	}
}

func TestNewMuxMethodNotAllowedUsesAPIEnvelope(t *testing.T) {
	m := NewMux()
	m.Handle(http.MethodGet, "/api/v1/system/ping", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/ping", nil)
	m.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusMethodNotAllowed)
	}
	if !strings.Contains(rr.Body.String(), `"code":"method_not_allowed"`) {
		t.Fatalf("expected method_not_allowed code, got body=%s", rr.Body.String())
	}
}
