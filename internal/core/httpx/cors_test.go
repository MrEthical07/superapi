package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/config"
)

func TestCORSDeniedPreflightUsesErrorEnvelope(t *testing.T) {
	h := RequestID(CORS(config.CORSConfig{
		Enabled:      true,
		AllowOrigins: []string{"https://allowed.example.com"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/system/whoami", nil)
	req.Header.Set("Origin", "https://denied.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusForbidden)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type=%q expected application/json", got)
	}
	if !strings.Contains(rr.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("expected forbidden envelope, got %s", rr.Body.String())
	}
}
