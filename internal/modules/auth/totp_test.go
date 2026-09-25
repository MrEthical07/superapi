package auth

import (
	"net/http"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/config"
)

func enrollTOTP(t *testing.T, h *harness, token string) (secret string, backupCodes []string) {
	t.Helper()
	setup := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/totp/setup", token: token})
	if setup.status != http.StatusOK {
		t.Fatalf("setup: status=%d body=%s", setup.status, setup.body)
	}
	var s totpSetupResponse
	setup.data(t, &s)
	if s.SecretBase32 == "" || s.OTPAuthURI == "" {
		t.Fatalf("setup response incomplete: %s", setup.body)
	}

	if bad := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/totp/confirm", token: token, body: map[string]string{"code": "000000"}}); bad.status != http.StatusBadRequest {
		t.Fatalf("wrong confirm code: status=%d body=%s", bad.status, bad.body)
	}

	// Previous time step (within goAuth's skew of 1), so later steps can use
	// strictly newer counters without waiting for the clock.
	confirm := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/totp/confirm", token: token, body: map[string]string{"code": totpCode(t, s.SecretBase32, -1)}})
	if confirm.status != http.StatusOK {
		t.Fatalf("confirm: status=%d body=%s", confirm.status, confirm.body)
	}
	var c totpConfirmResponse
	confirm.data(t, &c)
	if !c.Enabled || len(c.BackupCodes) == 0 {
		t.Fatalf("confirm response: %s", confirm.body)
	}
	return s.SecretBase32, c.BackupCodes
}

func mfaChallenge(t *testing.T, h *harness, identifier string) string {
	t.Helper()
	res := h.post("/api/v1/auth/login", map[string]string{"identifier": identifier, "password": testPassword})
	var tok tokenResponse
	res.data(t, &tok)
	if res.status != http.StatusOK || !tok.MFARequired || tok.MFAChallenge == "" || tok.AccessToken != "" {
		t.Fatalf("expected MFA challenge: status=%d body=%s", res.status, res.body)
	}
	return tok.MFAChallenge
}

func TestTOTPLifecycle(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{TOTPEnabled: true}})
	h.createUser("totp@example.com")
	token := h.login("totp@example.com", testPassword)

	secret, backupCodes := enrollTOTP(t, h, token)

	// Confirming TOTP revokes existing sessions (goAuth), so log in again —
	// now through the MFA challenge.
	challenge := mfaChallenge(t, h, "totp@example.com")
	current := totpCode(t, secret, 0)
	done := h.post("/api/v1/auth/mfa/confirm", map[string]string{"challenge": challenge, "code": current, "type": "totp"})
	var tok tokenResponse
	done.data(t, &tok)
	if done.status != http.StatusOK || tok.AccessToken == "" {
		t.Fatalf("mfa confirm: status=%d body=%s", done.status, done.body)
	}
	token = tok.AccessToken

	// Replay protection: the same code cannot complete a second login.
	replay := h.post("/api/v1/auth/mfa/confirm", map[string]string{"challenge": mfaChallenge(t, h, "totp@example.com"), "code": current, "type": "totp"})
	if replay.status == http.StatusOK {
		t.Fatalf("replayed TOTP code accepted: %s", replay.body)
	}

	// Enrolling again while enabled is refused (would replace the secret).
	if again := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/totp/setup", token: token}); again.status != http.StatusConflict {
		t.Fatalf("re-setup while enabled: status=%d want 409", again.status)
	}

	// Backup codes: single use.
	code := backupCodes[0]
	first := h.post("/api/v1/auth/mfa/confirm", map[string]string{"challenge": mfaChallenge(t, h, "totp@example.com"), "code": code, "type": "backup"})
	if first.status != http.StatusOK {
		t.Fatalf("backup code login: status=%d body=%s", first.status, first.body)
	}
	second := h.post("/api/v1/auth/mfa/confirm", map[string]string{"challenge": mfaChallenge(t, h, "totp@example.com"), "code": code, "type": "backup"})
	if second.status == http.StatusOK {
		t.Fatal("backup code accepted twice")
	}

	// Regenerate with the next time step (newer than every code used so far).
	var fresh tokenResponse
	first.data(t, &fresh)
	if bad := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/backup-codes/regenerate", token: fresh.AccessToken, body: map[string]string{"code": "123456"}}); bad.status != http.StatusBadRequest {
		t.Fatalf("regenerate with wrong code: status=%d body=%s", bad.status, bad.body)
	}
	regen := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/backup-codes/regenerate", token: fresh.AccessToken, body: map[string]string{"code": totpCode(t, secret, 1)}})
	var codes backupCodesResponse
	regen.data(t, &codes)
	if regen.status != http.StatusOK || len(codes.BackupCodes) == 0 {
		t.Fatalf("regenerate: status=%d body=%s", regen.status, regen.body)
	}
	if codes.BackupCodes[0] == backupCodes[0] {
		t.Fatal("regenerated codes must differ")
	}
}

func TestTOTPDisable(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{TOTPEnabled: true}})
	h.createUser("off@example.com")
	token := h.login("off@example.com", testPassword)

	// Disable before enrolling: nothing to disable.
	if res := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/totp/disable", token: token, body: map[string]string{"code": "123456"}}); res.status != http.StatusConflict {
		t.Fatalf("disable without totp: status=%d body=%s", res.status, res.body)
	}

	secret, _ := enrollTOTP(t, h, token)
	done := h.post("/api/v1/auth/mfa/confirm", map[string]string{"challenge": mfaChallenge(t, h, "off@example.com"), "code": totpCode(t, secret, 0), "type": "totp"})
	var tok tokenResponse
	done.data(t, &tok)

	if bad := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/totp/disable", token: tok.AccessToken, body: map[string]string{"code": "000000"}}); bad.status != http.StatusBadRequest {
		t.Fatalf("disable with wrong code: status=%d body=%s", bad.status, bad.body)
	}
	res := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/totp/disable", token: tok.AccessToken, body: map[string]string{"code": totpCode(t, secret, 1)}})
	if res.status != http.StatusOK {
		t.Fatalf("disable: status=%d body=%s", res.status, res.body)
	}
	// Login no longer requires a second factor.
	h.login("off@example.com", testPassword)
}
