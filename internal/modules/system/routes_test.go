package system

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/httpx"
)

func TestWhoamiRequiresAuth(t *testing.T) {
	m := &Module{}
	m.BindDependencies(nil)

	r := httpx.NewMux()
	if err := m.Register(r); err != nil {
		t.Fatalf("register: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/whoami", nil)
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnauthorized)
	}
}
