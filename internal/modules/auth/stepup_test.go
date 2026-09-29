package auth

import (
	"net/http"
	"strings"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/config"
)

// stepUpEndpoint describes one endpoint that needs the account password in
// addition to an access token.
type stepUpEndpoint struct {
	name string
	path string
	// body builds a request body carrying an optional password.
	body func(password *string) map[string]any
	// ok is the status that proves the request got past the password check
	// and reached the action itself.
	ok func(status int) bool
}

// extraStepUpEndpoints are step-up endpoints of optional features (WebAuthn);
// their test files add to it, so pruning a feature removes its cases.
var extraStepUpEndpoints []stepUpEndpoint

func stepUpEndpoints() []stepUpEndpoint {
	eps := []stepUpEndpoint{
		{
			name: "totp setup",
			path: "/api/v1/auth/mfa/totp/setup",
			body: func(p *string) map[string]any { return withPassword(map[string]any{}, p) },
			ok:   func(status int) bool { return status == http.StatusOK },
		},
	}
	return append(eps, extraStepUpEndpoints...)
}

func withPassword(m map[string]any, password *string) map[string]any {
	if password != nil {
		m["password"] = *password
	}
	return m
}

func newStepUpHarness(t *testing.T, identifier string) (*harness, string) {
	t.Helper()
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{TOTPEnabled: true}})
	h.createUser(identifier)
	return h, h.login(identifier, testPassword)
}

func TestStepUpRequiresPassword(t *testing.T) {
	for _, ep := range stepUpEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			h, token := newStepUpHarness(t, "stepup@example.com")

			t.Run("no password", func(t *testing.T) {
				res := h.do(call{method: http.MethodPost, path: ep.path, token: token, body: ep.body(nil)})
				if res.status != http.StatusBadRequest || !strings.Contains(string(res.body), "password is required") {
					t.Fatalf("status=%d body=%s, want 400 password is required", res.status, res.body)
				}
			})
			t.Run("empty password", func(t *testing.T) {
				empty := ""
				res := h.do(call{method: http.MethodPost, path: ep.path, token: token, body: ep.body(&empty)})
				if res.status != http.StatusBadRequest {
					t.Fatalf("status=%d body=%s, want 400", res.status, res.body)
				}
			})
			t.Run("oversized password", func(t *testing.T) {
				long := strings.Repeat("a", maxPasswordLength+1)
				res := h.do(call{method: http.MethodPost, path: ep.path, token: token, body: ep.body(&long)})
				if res.status != http.StatusBadRequest {
					t.Fatalf("status=%d body=%s, want 400", res.status, res.body)
				}
			})
			t.Run("wrong password", func(t *testing.T) {
				wrong := "not-the-account-password"
				res := h.do(call{method: http.MethodPost, path: ep.path, token: token, body: ep.body(&wrong)})
				if res.status != http.StatusUnauthorized {
					t.Fatalf("status=%d body=%s, want 401", res.status, res.body)
				}
			})
			t.Run("correct password", func(t *testing.T) {
				right := testPassword
				res := h.do(call{method: http.MethodPost, path: ep.path, token: token, body: ep.body(&right)})
				if !ep.ok(res.status) {
					t.Fatalf("status=%d body=%s, did not get past the password check", res.status, res.body)
				}
			})
			t.Run("no token", func(t *testing.T) {
				right := testPassword
				res := h.do(call{method: http.MethodPost, path: ep.path, body: ep.body(&right)})
				if res.status != http.StatusUnauthorized {
					t.Fatalf("status=%d, want 401 without an access token", res.status)
				}
			})
		})
	}
}

// A wrong step-up password answers exactly like a failed login, so the two
// are indistinguishable to a client.
func TestStepUpWrongPasswordMatchesLoginFailure(t *testing.T) {
	h, token := newStepUpHarness(t, "same@example.com")
	wrong := "not-the-account-password"

	login := h.post("/api/v1/auth/login", map[string]string{"identifier": "same@example.com", "password": wrong})
	for _, ep := range stepUpEndpoints() {
		res := h.do(call{method: http.MethodPost, path: ep.path, token: token, body: ep.body(&wrong)})
		if res.status != login.status {
			t.Fatalf("%s: status=%d, login failure status=%d", ep.name, res.status, login.status)
		}
		if res.comparable(t) != login.comparable(t) {
			t.Fatalf("%s: body %s differs from the login failure %s", ep.name, res.comparable(t), login.comparable(t))
		}
	}
}

// goAuth's password-verify limiter caps guesses made with a stolen token.
func TestStepUpIsRateLimited(t *testing.T) {
	for _, ep := range stepUpEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			h, token := newStepUpHarness(t, "limited@example.com")
			wrong := "still-not-the-password"

			limitedAt := 0
			for i := 1; i <= 30; i++ {
				res := h.do(call{method: http.MethodPost, path: ep.path, token: token, body: ep.body(&wrong)})
				if res.status == http.StatusTooManyRequests {
					limitedAt = i
					break
				}
				if res.status != http.StatusUnauthorized {
					t.Fatalf("attempt %d: status=%d body=%s", i, res.status, res.body)
				}
			}
			if limitedAt == 0 {
				t.Fatal("no 429 after 30 wrong passwords")
			}

			// Once limited, even the correct password is refused: guessing
			// cannot be finished off with a lucky last attempt.
			right := testPassword
			if res := h.do(call{method: http.MethodPost, path: ep.path, token: token, body: ep.body(&right)}); res.status != http.StatusTooManyRequests {
				t.Fatalf("correct password while limited: status=%d body=%s, want 429", res.status, res.body)
			}
		})
	}
}

// The already-enabled 409 must not be reachable without the password: a valid
// token with a wrong password gets 401, and only the right password reaches
// the 409.
func TestStepUpPrecedesAlreadyEnabledCheck(t *testing.T) {
	h, token := newStepUpHarness(t, "enabled@example.com")
	secret, _ := enrollTOTP(t, h, token)

	// Enrolling revokes existing sessions; log in again through the MFA step.
	done := h.post("/api/v1/auth/mfa/confirm", map[string]string{"challenge": mfaChallenge(t, h, "enabled@example.com"), "code": totpCode(t, secret, 0), "type": "totp"})
	var tok tokenResponse
	done.data(t, &tok)
	if done.status != http.StatusOK || tok.AccessToken == "" {
		t.Fatalf("mfa login: status=%d body=%s", done.status, done.body)
	}

	setup := func(password string) int {
		return h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/totp/setup", token: tok.AccessToken, body: map[string]any{"password": password}}).status
	}
	if got := setup("wrong-password-for-enabled"); got != http.StatusUnauthorized {
		t.Fatalf("wrong password: status=%d, want 401", got)
	}
	if got := setup(testPassword); got != http.StatusConflict {
		t.Fatalf("correct password: status=%d, want 409", got)
	}
}
