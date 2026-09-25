package policy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

// The policy test engine logs in without a request tenant, so its token
// carries goAuth's default tenant "0".
func TestAuthRequiredBindsTokenTenantToRequestTenant(t *testing.T) {
	engine, token := newPolicyTestAuthEngine(t)

	cases := []struct {
		name          string
		requestTenant string
		wantStatus    int
	}{
		{name: "no request tenant (tenancy off)", requestTenant: "", wantStatus: http.StatusOK},
		{name: "matching request tenant", requestTenant: auth.DefaultTenantID, wantStatus: http.StatusOK},
		{name: "token from another tenant", requestTenant: "tenant-b", wantStatus: http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}), AuthRequired(engine, auth.ModeHybrid))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			if tc.requestTenant != "" {
				req = req.WithContext(auth.WithRequestTenant(req.Context(), tc.requestTenant))
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status=%d want %d body=%s", rr.Code, tc.wantStatus, rr.Body.String())
			}
		})
	}
}

func TestTenantRequiredRejectsRequestTenantMismatch(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := TenantRequired()(next)

	cases := []struct {
		name          string
		principal     string
		requestTenant string
		wantStatus    int
	}{
		{name: "match", principal: "acme", requestTenant: "acme", wantStatus: http.StatusOK},
		{name: "no request tenant", principal: "acme", requestTenant: "", wantStatus: http.StatusOK},
		{name: "mismatch", principal: "acme", requestTenant: "other", wantStatus: http.StatusNotFound},
		{name: "principal without tenant", principal: "", requestTenant: "acme", wantStatus: http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			ctx := auth.WithContext(req.Context(), auth.AuthContext{UserID: "u1", TenantID: tc.principal})
			if tc.requestTenant != "" {
				ctx = auth.WithRequestTenant(ctx, tc.requestTenant)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req.WithContext(ctx))
			if rr.Code != tc.wantStatus {
				t.Fatalf("status=%d want %d", rr.Code, tc.wantStatus)
			}
		})
	}
}
