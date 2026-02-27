package tenants

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/httpx"
)

type staticAuthProvider struct {
	principal auth.AuthContext
	err       error
}

func (p staticAuthProvider) Authenticate(ctx context.Context, token string, mode auth.Mode) (auth.AuthContext, error) {
	if p.err != nil {
		return auth.AuthContext{}, p.err
	}
	if token == "valid-token" {
		return p.principal, nil
	}
	return auth.AuthContext{}, auth.ErrUnauthenticated
}

func TestTenantsSelfRouteUnauthorizedWithoutAuth(t *testing.T) {
	m := New()
	m.BindDependencies(&app.Dependencies{Auth: auth.NewDisabledProvider(), AuthMode: auth.ModeHybrid})

	r := httpx.NewMux()
	if err := m.Register(r); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/self", nil)
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnauthorized)
	}
}

func TestTenantsSelfRouteTenantWithDBDisabled(t *testing.T) {
	m := New()
	m.BindDependencies(&app.Dependencies{
		Auth:     staticAuthProvider{principal: auth.AuthContext{UserID: "u1", TenantID: "tenant_1"}},
		AuthMode: auth.ModeHybrid,
	})

	r := httpx.NewMux()
	if err := m.Register(r); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/self", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusServiceUnavailable)
	}
}

func TestTenantsSelfRouteRejectsAuthProviderFailure(t *testing.T) {
	m := New()
	m.BindDependencies(&app.Dependencies{
		Auth:     staticAuthProvider{err: errors.New("invalid token")},
		AuthMode: auth.ModeHybrid,
	})

	r := httpx.NewMux()
	if err := m.Register(r); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/self", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnauthorized)
	}
}
