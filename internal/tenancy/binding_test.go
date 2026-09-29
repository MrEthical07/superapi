package tenancy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/tenancy"
	"github.com/MrEthical07/superapi/internal/tenancy/tenancytest"
)

// loginToken creates a user in tenantID and returns an access token for it.
func loginToken(t *testing.T, e *tenancytest.Engine, tenantID string) string {
	t.Helper()
	ctx := tenancy.WithRequestTenant(context.Background(), tenantID)
	if _, err := e.CreateAccount(ctx, goauth.CreateAccountRequest{Identifier: "binder@example.com", Password: password}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	access, _, err := e.Login(ctx, "binder@example.com", password)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	return access
}

// AuthRequired rejects a token whose tenant differs from the request tenant in
// every validation mode: jwt_only and hybrid routes never load the
// tenant-keyed session, so the token-to-tenant check is the only thing that
// binds them.
func TestAuthRequiredBindsTokenTenantToRequestTenant(t *testing.T) {
	e := tenancytest.NewEngine(t, auth.Features{})
	token := loginToken(t, e, "acme")

	cases := []struct {
		name          string
		mode          auth.Mode
		requestTenant string
		wantStatus    int
	}{
		{name: "hybrid, matching tenant", mode: auth.ModeHybrid, requestTenant: "acme", wantStatus: http.StatusOK},
		{name: "hybrid, no request tenant", mode: auth.ModeHybrid, requestTenant: "", wantStatus: http.StatusOK},
		{name: "hybrid, token from another tenant", mode: auth.ModeHybrid, requestTenant: "globex", wantStatus: http.StatusUnauthorized},
		{name: "jwt_only, token from another tenant", mode: auth.ModeJWTOnly, requestTenant: "globex", wantStatus: http.StatusUnauthorized},
		{name: "strict, token from another tenant", mode: auth.ModeStrict, requestTenant: "globex", wantStatus: http.StatusUnauthorized},
		{name: "jwt_only, matching tenant", mode: auth.ModeJWTOnly, requestTenant: "acme", wantStatus: http.StatusOK},
	}

	policy.UseAuthExtensions(e.Engine, *tenancy.AuthExtension(true))
	t.Cleanup(func() { policy.UseAuthExtensions(e.Engine) })

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var principalTenant string
			h := policy.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p, _ := auth.FromContext(r.Context())
				principalTenant = tenancy.PrincipalTenant(p)
				w.WriteHeader(http.StatusOK)
			}), policy.AuthRequired(e.Engine, tc.mode))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			if tc.requestTenant != "" {
				req = req.WithContext(tenancy.WithRequestTenant(req.Context(), tc.requestTenant))
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status=%d want %d body=%s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if tc.wantStatus == http.StatusOK && principalTenant != "acme" {
				t.Fatalf("principal tenant attribute = %q, want acme", principalTenant)
			}
		})
	}
}

// With tenancy off the binding check is not installed, but the principal still
// carries the tenant goAuth stamped (its default tenant "0"), so tenant
// policies keep working on a single-tenant deployment.
func TestAuthExtensionWithTenancyOff(t *testing.T) {
	e := tenancytest.NewEngine(t, auth.Features{})
	token := loginToken(t, e, "acme")

	policy.UseAuthExtensions(e.Engine, *tenancy.AuthExtension(false))
	t.Cleanup(func() { policy.UseAuthExtensions(e.Engine) })

	h := policy.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.FromContext(r.Context())
		if got := tenancy.PrincipalTenant(p); got != "acme" {
			t.Errorf("tenant attribute = %q, want acme", got)
		}
		w.WriteHeader(http.StatusOK)
	}), policy.AuthRequired(e.Engine, auth.ModeHybrid))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	// A mismatching request tenant is not checked when the check is not installed.
	req = req.WithContext(tenancy.WithRequestTenant(req.Context(), "globex"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
