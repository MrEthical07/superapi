package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireBearerTokenUsesErrorEnvelope(t *testing.T) {
	h := requireBearerToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "secret-token")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnauthorized)
	}
	if got := rr.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatalf("expected WWW-Authenticate header to be set")
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type=%q expected application/json", got)
	}
	if !strings.Contains(rr.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("expected unauthorized envelope, got %s", rr.Body.String())
	}
}
