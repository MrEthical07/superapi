package auth

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
)

// TestWhoamiOutputWithoutFeatures pins the whoami response when no optional
// feature contributes a principal attribute: user_id, role and permissions and
// nothing else. (A feature that attaches an attribute, such as an organization id,
// adds one field under the attribute's key; that is tested with the feature.)
func TestWhoamiOutputWithoutFeatures(t *testing.T) {
	engine, _ := authtest.NewEngine(t, authtest.NewUserRepository())
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
		"role":        `"user"`,
		"permissions": `["system.whoami"]`,
	}
	if len(env.Data) != 3 {
		t.Fatalf("data keys=%v want user_id, role, permissions", env.Data)
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

// A feature that attaches a principal attribute (policy.AuthExtension) shows up
// as one extra top-level field under the attribute's key, after user_id; empty
// values and keys that collide with core fields are skipped.
func TestWhoamiReportsFeatureAttributes(t *testing.T) {
	engine, _ := authtest.NewEngine(t, authtest.NewUserRepository())
	ctx := context.Background()
	if _, err := engine.CreateAccount(ctx, goauth.CreateAccountRequest{Identifier: "erin@example.com", Password: "correct-horse-battery-staple"}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	access, _, err := engine.Login(ctx, "erin@example.com", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	calls := 0
	policy.UseAuthExtensions(engine,
		policy.AuthExtension{Attribute: func(*goauth.AuthResult) (string, string) { calls++; return "org_id", "acme" }},
		policy.AuthExtension{Attribute: func(*goauth.AuthResult) (string, string) { return "region", "" }},
		policy.AuthExtension{Attribute: func(*goauth.AuthResult) (string, string) { return "role", "spoofed" }},
	)
	t.Cleanup(func() { policy.UseAuthExtensions(engine) })

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
	if rr.Code != http.StatusOK || calls == 0 {
		t.Fatalf("status=%d calls=%d body=%s", rr.Code, calls, rr.Body.String())
	}

	var env struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(env.Data["org_id"]) != `"acme"` {
		t.Fatalf("org_id=%s in %s", env.Data["org_id"], rr.Body.String())
	}
	if _, present := env.Data["region"]; present {
		t.Fatal("an attribute with an empty value must be omitted")
	}
	if string(env.Data["role"]) != `"user"` {
		t.Fatalf("a colliding attribute must not override a core field: role=%s", env.Data["role"])
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte(`"user_id"`)) || bytes.Index(rr.Body.Bytes(), []byte(`"org_id"`)) < bytes.Index(rr.Body.Bytes(), []byte(`"user_id"`)) {
		t.Fatalf("attributes come after user_id: %s", rr.Body.String())
	}
}
