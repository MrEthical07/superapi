package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/httpx"
)

// loginFrom posts a login as a request that arrived from remoteAddr and, when
// forwardedFor is set, carried that X-Forwarded-For header. The handler chain
// includes the ClientIP middleware the app installs, configured with
// trustedProxies.
func loginFrom(t *testing.T, h *harness, trustedProxies []string, remoteAddr, forwardedFor, identifier, password string) int {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]string{"identifier": identifier, "password": password}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", &buf)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	rr := httptest.NewRecorder()
	httpx.ClientIP(config.ClientIPConfig{TrustedProxies: trustedProxies})(h.handler).ServeHTTP(rr, req)
	return rr.Code
}

func setIPLimiterEnv(t *testing.T, enabled string) {
	t.Helper()
	t.Setenv("AUTH_LOGIN_IP_LIMITER_ENABLED", enabled)
	t.Setenv("AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS", "3")
	t.Setenv("AUTH_LOGIN_IP_LIMITER_COOLDOWN", "")
	t.Setenv("AUTH_REFRESH_RECHECK_USER", "")
}

// The login handler hands goAuth the client address the trusted-proxy logic
// resolved, so the per-IP budget works end to end: through a trusted proxy the
// forwarded address is what is counted, per address and across accounts.
func TestLoginIPLimiterThroughTrustedProxy(t *testing.T) {
	setIPLimiterEnv(t, "true")
	h := newHarness(t, harnessOptions{})
	for _, who := range []string{"a", "b", "c", "d"} {
		h.createUser(who + "@example.com")
	}
	trusted := []string{"10.0.0.0/8"}
	const proxy = "10.0.0.1:4000"

	for _, who := range []string{"a", "b", "c"} {
		if got := loginFrom(t, h, trusted, proxy, "203.0.113.9", who+"@example.com", "wrong-password"); got != http.StatusUnauthorized {
			t.Fatalf("wrong password for %s: status %d, want 401", who, got)
		}
	}
	if got := loginFrom(t, h, trusted, proxy, "203.0.113.9", "d@example.com", testPassword); got != http.StatusTooManyRequests {
		t.Fatalf("correct password from the exhausted address: status %d, want 429", got)
	}
	if got := loginFrom(t, h, trusted, proxy, "198.51.100.7", "d@example.com", testPassword); got != http.StatusOK {
		t.Fatalf("correct password from another forwarded address: status %d, want 200", got)
	}
}

// The address is only as trustworthy as the trusted-proxy configuration. A
// caller that is not a trusted proxy cannot choose its address with
// X-Forwarded-For: rotating the header does not escape the budget of the real
// peer address.
func TestLoginIPLimiterIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {
	setIPLimiterEnv(t, "true")
	h := newHarness(t, harnessOptions{})
	for _, who := range []string{"a", "b", "c", "d"} {
		h.createUser(who + "@example.com")
	}
	trusted := []string{"10.0.0.0/8"}
	const attacker = "192.0.2.50:4000"

	for i, who := range []string{"a", "b", "c"} {
		spoof := []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"}[i]
		if got := loginFrom(t, h, trusted, attacker, spoof, who+"@example.com", "wrong-password"); got != http.StatusUnauthorized {
			t.Fatalf("wrong password for %s: status %d, want 401", who, got)
		}
	}
	if got := loginFrom(t, h, trusted, attacker, "198.51.100.99", "d@example.com", testPassword); got != http.StatusTooManyRequests {
		t.Fatalf("spoofed fresh address from an untrusted peer: status %d, want 429", got)
	}
}

// Off by default: the same traffic is never throttled by address.
func TestLoginIPLimiterOffByDefaultOverHTTP(t *testing.T) {
	setIPLimiterEnv(t, "")
	h := newHarness(t, harnessOptions{})
	for _, who := range []string{"a", "b", "c", "d"} {
		h.createUser(who + "@example.com")
	}
	for _, who := range []string{"a", "b", "c"} {
		_ = loginFrom(t, h, nil, "192.0.2.50:4000", "", who+"@example.com", "wrong-password")
	}
	if got := loginFrom(t, h, nil, "192.0.2.50:4000", "", "d@example.com", testPassword); got != http.StatusOK {
		t.Fatalf("with the limiter off: status %d, want 200", got)
	}
}
