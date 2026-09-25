package auth

import (
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
)

// TestWhoamiSingleTenantOutputUnchanged pins the v0.8.0 whoami response with
// tenancy off: goAuth stamps its default tenant "0" into the token and the
// provider leaves UserRecord.TenantID empty.
func TestWhoamiSingleTenantOutputUnchanged(t *testing.T) {
	engine, _ := authtest.NewEngine(t, false, authtest.NewUserRepository())
	ctx := context.Background()
	if _, err := engine.CreateAccount(ctx, goauth.CreateAccountRequest{Identifier: "dana@example.com", Password: "correct-horse-battery-staple"}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	access, _, err := engine.Login(ctx, "dana@example.com", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	m := New()
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
		OK   bool                       `json:"ok"`
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.OK {
		t.Fatalf("ok=false body=%s", rr.Body.String())
	}
	wantKeys := map[string]string{
		"tenant_id":   `"0"`,
		"role":        `"user"`,
		"permissions": `["system.whoami"]`,
	}
	if len(env.Data) != 4 {
		t.Fatalf("data keys=%v want user_id, tenant_id, role, permissions", env.Data)
	}
	if _, ok := env.Data["user_id"]; !ok {
		t.Fatal("missing user_id")
	}
	for k, want := range wantKeys {
		if got := string(env.Data[k]); got != want {
			t.Fatalf("%s=%s want %s", k, got, want)
		}
	}
}
