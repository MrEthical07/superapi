package tenancy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/auth/authtest"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
	authmodule "github.com/MrEthical07/superapi/internal/modules/auth"
	"github.com/MrEthical07/superapi/internal/tenancy"
	"github.com/MrEthical07/superapi/internal/tenancy/tenancytest"
)

// TestEndToEnd drives the real tenant middleware, login route and whoami route
// with goAuth multi-tenancy enabled.
func TestEndToEnd(t *testing.T) {
	e := tenancytest.NewEngine(t, auth.Features{})
	if _, err := e.CreateAccount(tenancy.WithRequestTenant(context.Background(), "tenant-a"),
		goauth.CreateAccountRequest{Identifier: "erin@example.com", Password: password}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	policy.UseAuthExtensions(e.Engine, *tenancy.AuthExtension(true))
	t.Cleanup(func() { policy.UseAuthExtensions(e.Engine) })

	m := authmodule.New()
	m.BindDependencies(&app.Dependencies{AuthEngine: e.Engine, AuthMode: auth.ModeStrict})
	mux := httpx.NewMux()
	if err := m.Register(mux); err != nil {
		t.Fatalf("register: %v", err)
	}
	handler := tenancy.Middleware(tenancy.ResolverConfig{Header: "X-Tenant-ID"})(mux)

	do := func(method, path, tenantID, bearer string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Content-Type", "application/json")
		if tenantID != "" {
			req.Header.Set("X-Tenant-ID", tenantID)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}
	login := func(tenantID, pw string) *httptest.ResponseRecorder {
		return do(http.MethodPost, "/api/v1/auth/login", tenantID, "", map[string]string{"identifier": "erin@example.com", "password": pw})
	}
	errBody := func(rr *httptest.ResponseRecorder) string {
		var env struct {
			Error json.RawMessage `json:"error"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &env)
		return string(env.Error)
	}

	ok := login("tenant-a", password)
	if ok.Code != http.StatusOK {
		t.Fatalf("same-tenant login status=%d body=%s", ok.Code, ok.Body.String())
	}
	var tokens struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(ok.Body.Bytes(), &tokens); err != nil || tokens.Data.AccessToken == "" {
		t.Fatalf("decode tokens: %v body=%s", err, ok.Body.String())
	}

	cross := login("tenant-b", password)
	wrong := login("tenant-a", "definitely-wrong-password")
	if cross.Code != http.StatusUnauthorized || wrong.Code != http.StatusUnauthorized {
		t.Fatalf("cross=%d wrong=%d want 401/401", cross.Code, wrong.Code)
	}
	if errBody(cross) != errBody(wrong) {
		t.Fatalf("cross-tenant error %s differs from wrong-password error %s", errBody(cross), errBody(wrong))
	}

	if missing := login("", password); missing.Code != http.StatusBadRequest {
		t.Fatalf("missing tenant status=%d want 400", missing.Code)
	}

	if rr := do(http.MethodGet, "/api/v1/auth/whoami", "tenant-a", tokens.Data.AccessToken, nil); rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"tenant_id":"tenant-a"`)) {
		t.Fatalf("whoami same tenant status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr := do(http.MethodGet, "/api/v1/auth/whoami", "tenant-b", tokens.Data.AccessToken, nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("whoami with tenant-a token under tenant-b status=%d want 401", rr.Code)
	}
}

// With tenancy registered but switched off, whoami reports goAuth's default
// tenant "0", exactly as before tenancy became a removable feature.
func TestWhoamiWithTenancyRegisteredButOff(t *testing.T) {
	engine, _ := authtest.NewEngine(t, authtest.NewUserRepository())
	ctx := context.Background()
	if _, err := engine.CreateAccount(ctx, goauth.CreateAccountRequest{Identifier: "dana@example.com", Password: password}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	access, _, err := engine.Login(ctx, "dana@example.com", password)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	policy.UseAuthExtensions(engine, *tenancy.AuthExtension(false))
	t.Cleanup(func() { policy.UseAuthExtensions(engine) })

	m := authmodule.New()
	m.BindDependencies(&app.Dependencies{AuthEngine: engine, AuthMode: auth.ModeHybrid})
	r := httpx.NewMux()
	if err := m.Register(r); err != nil {
		t.Fatalf("register: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var env struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data) != 4 {
		t.Fatalf("data keys=%v want user_id, tenant_id, role, permissions", env.Data)
	}
	for k, want := range map[string]string{"tenant_id": `"0"`, "role": `"user"`, "permissions": `["system.whoami"]`} {
		if got := string(env.Data[k]); got != want {
			t.Fatalf("%s=%s want %s", k, got, want)
		}
	}
}
