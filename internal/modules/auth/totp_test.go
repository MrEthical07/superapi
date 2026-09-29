package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/config"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

func enrollTOTP(t *testing.T, h *harness, token string) (secret string, backupCodes []string) {
	t.Helper()
	setup := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/totp/setup", token: token, body: map[string]string{"password": testPassword}})
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
	if again := h.do(call{method: http.MethodPost, path: "/api/v1/auth/mfa/totp/setup", token: token, body: map[string]string{"password": testPassword}}); again.status != http.StatusConflict {
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

// stubAccounts lets a test make the service's own pre-check pass so the
// request reaches goAuth's ErrTOTPAlreadyEnabled guard.
type stubAccounts struct{ totpEnabled bool }

func (s stubAccounts) FindRecipient(context.Context, string) (recipient, bool, error) {
	return recipient{}, false, nil
}

func (s stubAccounts) TOTPEnabled(context.Context, string) (bool, error) { return s.totpEnabled, nil }

func TestSetupTOTPAlreadyEnabledPreCheckAndEngineGuardMatch(t *testing.T) {
	h := newHarness(t, harnessOptions{auth: config.AuthConfig{TOTPEnabled: true}})
	userID := h.createUser("both@example.com")
	enrollTOTP(t, h, h.login("both@example.com", testPassword))

	// Pre-check path: the repository reports TOTP as enabled.
	_, preErr := newService(h.engine, stubAccounts{totpEnabled: true}, nil, features{TOTP: true}).setupTOTP(context.Background(), userID)
	// Engine path: the repository is stale, so only goAuth's guard fires.
	_, engineErr := newService(h.engine, stubAccounts{totpEnabled: false}, nil, features{TOTP: true}).setupTOTP(context.Background(), userID)

	pre, ok := apperr.AsAppError(preErr)
	if !ok {
		t.Fatalf("pre-check error %v is not an AppError", preErr)
	}
	engine, ok := apperr.AsAppError(engineErr)
	if !ok {
		t.Fatalf("engine-path error %v is not an AppError", engineErr)
	}
	if !errors.Is(engineErr, goauth.ErrTOTPAlreadyEnabled) {
		t.Fatalf("engine path did not reach goAuth's guard: %v", engineErr)
	}
	if pre.StatusCode != http.StatusConflict || engine.StatusCode != http.StatusConflict {
		t.Fatalf("status pre=%d engine=%d, want 409 both", pre.StatusCode, engine.StatusCode)
	}
	if pre.Code != engine.Code || pre.Message != engine.Message {
		t.Fatalf("responses differ: pre=%s/%q engine=%s/%q", pre.Code, pre.Message, engine.Code, engine.Message)
	}
}
