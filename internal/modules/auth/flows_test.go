package auth

import (
	"net/http"
	"strings"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/httpx"
)

func TestRoutesAbsentWhenAuthDisabled(t *testing.T) {
	m := New()
	m.BindDependencies(nil)
	mux := httpx.NewMux()
	if err := m.Register(mux); err != nil {
		t.Fatalf("register: %v", err)
	}
	h := &harness{t: t, handler: mux}
	if res := h.post("/api/v1/auth/login", map[string]string{"identifier": "a", "password": "b"}); res.status != http.StatusNotFound {
		t.Fatalf("login with auth disabled: status=%d want 404", res.status)
	}

	m = New()
	m.BindDependencies(&app.Dependencies{})
	mux = httpx.NewMux()
	if err := m.Register(mux); err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestFeatureFlagsGateRoutes(t *testing.T) {
	gated := []struct {
		path string
		flag func(*config.AuthConfig)
	}{
		{"/api/v1/auth/register", func(c *config.AuthConfig) { c.RegistrationEnabled = true }},
		{"/api/v1/auth/password/reset/request", func(c *config.AuthConfig) { c.PasswordResetEnabled = true }},
		{"/api/v1/auth/password/reset/confirm", func(c *config.AuthConfig) { c.PasswordResetEnabled = true }},
		{"/api/v1/auth/email/verify/request", func(c *config.AuthConfig) { c.EmailVerificationEnabled = true }},
		{"/api/v1/auth/email/verify/confirm", func(c *config.AuthConfig) { c.EmailVerificationEnabled = true }},
		{"/api/v1/auth/mfa/totp/setup", func(c *config.AuthConfig) { c.TOTPEnabled = true }},
		{"/api/v1/auth/mfa/totp/confirm", func(c *config.AuthConfig) { c.TOTPEnabled = true }},
		{"/api/v1/auth/mfa/totp/disable", func(c *config.AuthConfig) { c.TOTPEnabled = true }},
		{"/api/v1/auth/mfa/backup-codes/regenerate", func(c *config.AuthConfig) { c.TOTPEnabled = true }},
	}

	off := newHarness(t, harnessOptions{})
	for _, g := range gated {
		if res := off.post(g.path, map[string]string{}); res.status != http.StatusNotFound {
			t.Errorf("%s with flag off: status=%d want 404", g.path, res.status)
		}
	}
	for _, g := range gated {
		var cfg config.AuthConfig
		g.flag(&cfg)
		on := newHarness(t, harnessOptions{auth: cfg})
		if res := on.post(g.path, map[string]string{}); res.status == http.StatusNotFound {
			t.Errorf("%s with flag on: got 404", g.path)
		}
	}

	// Always-on account routes exist and require authentication.
	for _, path := range []string{"/api/v1/auth/logout/all", "/api/v1/auth/password/change"} {
		if res := off.post(path, map[string]string{}); res.status != http.StatusUnauthorized {
			t.Errorf("%s: status=%d want 401", path, res.status)
		}
	}
	if res := off.do(call{method: http.MethodGet, path: "/api/v1/auth/sessions"}); res.status != http.StatusUnauthorized {
		t.Errorf("sessions: status=%d want 401", res.status)
	}
}

func TestRegisterIsEnumerationSafe(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{RegistrationEnabled: true}})
	h.createUser("taken@example.com")

	fresh := h.post("/api/v1/auth/register", map[string]string{"identifier": "new@example.com", "password": testPassword})
	dup := h.post("/api/v1/auth/register", map[string]string{"identifier": "taken@example.com", "password": testPassword})
	if fresh.status != http.StatusAccepted || dup.status != http.StatusAccepted {
		t.Fatalf("fresh=%d dup=%d want 202/202 (%s / %s)", fresh.status, dup.status, fresh.body, dup.body)
	}
	if fresh.comparable(t) != dup.comparable(t) {
		t.Fatalf("bodies differ: %s vs %s", fresh.comparable(t), dup.comparable(t))
	}

	// The new account works; a client cannot choose its role.
	h.login("new@example.com", testPassword)
	withRole := h.post("/api/v1/auth/register", map[string]any{"identifier": "admin@example.com", "password": testPassword, "role": "admin"})
	if withRole.status != http.StatusBadRequest {
		t.Fatalf("role field must be rejected: status=%d", withRole.status)
	}
}

func TestRegisterAutoLogin(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{RegistrationEnabled: true, RegistrationAutoLogin: true}})
	res := h.post("/api/v1/auth/register", map[string]string{"identifier": "auto@example.com", "password": testPassword})
	if res.status != http.StatusCreated {
		t.Fatalf("status=%d body=%s", res.status, res.body)
	}
	var tok tokenResponse
	res.data(t, &tok)
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		t.Fatalf("expected tokens: %s", res.body)
	}
	// Existing identifier: no tokens, generic accept (documented trade-off).
	if again := h.post("/api/v1/auth/register", map[string]string{"identifier": "auto@example.com", "password": testPassword}); again.status != http.StatusAccepted {
		t.Fatalf("duplicate with auto-login: status=%d", again.status)
	}
}

func TestPasswordResetFlow(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{PasswordResetEnabled: true}})
	h.createUser("reset@example.com")

	known := h.post("/api/v1/auth/password/reset/request", map[string]string{"identifier": "reset@example.com"})
	unknown := h.post("/api/v1/auth/password/reset/request", map[string]string{"identifier": "ghost@example.com"})
	if known.status != http.StatusAccepted || unknown.status != http.StatusAccepted {
		t.Fatalf("known=%d unknown=%d want 202/202", known.status, unknown.status)
	}
	if known.comparable(t) != unknown.comparable(t) {
		t.Fatalf("bodies differ: %s vs %s", known.comparable(t), unknown.comparable(t))
	}
	if strings.Contains(string(known.body), "challenge") {
		t.Fatalf("response must not carry the challenge: %s", known.body)
	}

	h.flush()
	challenge, ok := h.notifier.reset("reset@example.com")
	if !ok || challenge == "" {
		t.Fatal("expected a reset message for the existing account")
	}
	if _, ok := h.notifier.reset("ghost@example.com"); ok {
		t.Fatal("no message may be sent for an unknown account")
	}

	if bad := h.post("/api/v1/auth/password/reset/confirm", map[string]string{"challenge": "not-a-challenge", "new_password": "another-long-password-1"}); bad.status != http.StatusBadRequest {
		t.Fatalf("bad challenge: status=%d body=%s", bad.status, bad.body)
	}
	const newPassword = "a-brand-new-password-42"
	if ok := h.post("/api/v1/auth/password/reset/confirm", map[string]string{"challenge": challenge, "new_password": newPassword}); ok.status != http.StatusOK {
		t.Fatalf("confirm: status=%d body=%s", ok.status, ok.body)
	}
	h.login("reset@example.com", newPassword)
	if old := h.post("/api/v1/auth/login", map[string]string{"identifier": "reset@example.com", "password": testPassword}); old.status != http.StatusUnauthorized {
		t.Fatalf("old password still works: status=%d", old.status)
	}
	if reuse := h.post("/api/v1/auth/password/reset/confirm", map[string]string{"challenge": challenge, "new_password": "yet-another-password-7"}); reuse.status != http.StatusBadRequest {
		t.Fatalf("challenge must be single-use: status=%d", reuse.status)
	}
}

func TestEmailVerificationFlow(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{
		RegistrationEnabled:       true,
		EmailVerificationEnabled:  true,
		EmailVerificationRequired: true,
	}})

	if res := h.post("/api/v1/auth/register", map[string]string{"identifier": "verify@example.com", "password": testPassword}); res.status != http.StatusAccepted {
		t.Fatalf("register: %d %s", res.status, res.body)
	}
	h.flush()
	challenge, ok := h.notifier.verification("verify@example.com")
	if !ok {
		t.Fatal("registration must send a verification message")
	}

	if blocked := h.post("/api/v1/auth/login", map[string]string{"identifier": "verify@example.com", "password": testPassword}); blocked.status != http.StatusForbidden {
		t.Fatalf("unverified login: status=%d want 403", blocked.status)
	}

	// Request endpoint is enumeration-safe and sends nothing for unknown ids.
	known := h.post("/api/v1/auth/email/verify/request", map[string]string{"identifier": "verify@example.com"})
	unknown := h.post("/api/v1/auth/email/verify/request", map[string]string{"identifier": "nobody@example.com"})
	if known.status != http.StatusAccepted || known.comparable(t) != unknown.comparable(t) {
		t.Fatalf("known=%d %s unknown=%d %s", known.status, known.body, unknown.status, unknown.body)
	}
	h.flush()
	if _, ok := h.notifier.verification("nobody@example.com"); ok {
		t.Fatal("no message may be sent for an unknown account")
	}
	// The re-request issued a fresh challenge; use it via the id+code variant.
	latest, _ := h.notifier.verification("verify@example.com")
	parts := strings.SplitN(latest, ":", 3)
	if len(parts) != 3 {
		t.Fatalf("unexpected challenge format %q", latest)
	}
	if res := h.post("/api/v1/auth/email/verify/confirm", map[string]string{"verification_id": parts[1], "code": parts[2]}); res.status != http.StatusOK {
		t.Fatalf("confirm code: status=%d body=%s", res.status, res.body)
	}
	h.login("verify@example.com", testPassword)

	// Already verified: the first challenge (if still stored) or any other is rejected.
	if res := h.post("/api/v1/auth/email/verify/confirm", map[string]string{"challenge": challenge + "x"}); res.status != http.StatusBadRequest {
		t.Fatalf("tampered challenge: status=%d", res.status)
	}
	if res := h.post("/api/v1/auth/email/verify/confirm", map[string]string{"challenge": "c", "code": "x"}); res.status != http.StatusBadRequest {
		t.Fatalf("mixed input must be rejected: status=%d", res.status)
	}
}

func TestPasswordChangeSessionsAndLogoutAll(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	h.createUser("change@example.com")
	token := h.login("change@example.com", testPassword)
	h.login("change@example.com", testPassword)

	sessions := h.do(call{method: http.MethodGet, path: "/api/v1/auth/sessions", token: token})
	var list sessionsResponse
	sessions.data(t, &list)
	if sessions.status != http.StatusOK || len(list.Sessions) != 2 {
		t.Fatalf("sessions: status=%d body=%s", sessions.status, sessions.body)
	}

	wrong := h.do(call{method: http.MethodPost, path: "/api/v1/auth/password/change", token: token, body: map[string]string{"current_password": "nope-nope-nope", "new_password": "a-new-password-123"}})
	if wrong.status != http.StatusBadRequest {
		t.Fatalf("wrong current password: status=%d body=%s", wrong.status, wrong.body)
	}
	const next = "a-new-password-123"
	ok := h.do(call{method: http.MethodPost, path: "/api/v1/auth/password/change", token: token, body: map[string]string{"current_password": testPassword, "new_password": next}})
	if ok.status != http.StatusOK {
		t.Fatalf("change: status=%d body=%s", ok.status, ok.body)
	}
	fresh := h.login("change@example.com", next)

	if res := h.do(call{method: http.MethodPost, path: "/api/v1/auth/logout/all", token: fresh}); res.status != http.StatusOK {
		t.Fatalf("logout all: status=%d body=%s", res.status, res.body)
	}
	// Strict mode: every session is gone, so the token no longer works.
	if res := h.do(call{method: http.MethodGet, path: "/api/v1/auth/whoami", token: fresh}); res.status != http.StatusUnauthorized {
		t.Fatalf("token after logout-all: status=%d want 401", res.status)
	}
}
