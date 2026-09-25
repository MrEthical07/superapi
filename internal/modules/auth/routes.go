package auth

import (
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

// Register mounts the auth routes under /api/v1/auth.
//
// Nothing is registered when auth is disabled (AUTH_ENABLED=false). Optional
// endpoint groups are registered only when their feature flag is on, so a
// disabled feature answers 404 like any unknown route.
//
// Public credential endpoints rely on goAuth's built-in abuse limiters (login,
// MFA, reset, verification, and account creation are each rate limited per
// tenant and identifier). Policies are passed directly to r.Handle (no
// variadic spread) so `superapi-verify` can check them statically. See
// docs/auth-flows.md.
func (m *Module) Register(r httpx.Router) error {
	engine := m.runtime.AuthEngine()
	if engine == nil {
		return nil
	}
	mode := m.runtime.AuthMode()

	// --- credentials (always on) ---
	r.Handle(http.MethodPost, "/api/v1/auth/login", httpx.Adapter(m.login))
	r.Handle(http.MethodPost, "/api/v1/auth/mfa/confirm", httpx.Adapter(m.confirmMFA))
	r.Handle(http.MethodPost, "/api/v1/auth/refresh", httpx.Adapter(m.refresh))
	r.Handle(http.MethodPost, "/api/v1/auth/logout", httpx.Adapter(m.logout))

	// --- account (always on, authenticated) ---
	r.Handle(http.MethodPost, "/api/v1/auth/logout/all", httpx.Adapter(m.logoutAll),
		policy.AuthRequired(engine, mode),
	)
	r.Handle(http.MethodGet, "/api/v1/auth/sessions", httpx.Adapter(m.sessions),
		policy.AuthRequired(engine, mode),
	)
	if limiter := m.runtime.Limiter(); limiter != nil {
		r.Handle(http.MethodGet, "/api/v1/auth/whoami", httpx.Adapter(m.whoami),
			policy.AuthRequired(engine, mode),
			policy.RateLimitWithKeyer(limiter, "auth.whoami", m.rateRule, ratelimit.KeyByUserOrTenantOrTokenHash(16)),
		)
		r.Handle(http.MethodPost, "/api/v1/auth/password/change", httpx.Adapter(m.changePassword),
			policy.AuthRequired(engine, mode),
			policy.RateLimitWithKeyer(limiter, "auth.password.change", m.rateRule, ratelimit.KeyByUserOrTenantOrTokenHash(16)),
		)
	} else {
		r.Handle(http.MethodGet, "/api/v1/auth/whoami", httpx.Adapter(m.whoami),
			policy.AuthRequired(engine, mode),
		)
		r.Handle(http.MethodPost, "/api/v1/auth/password/change", httpx.Adapter(m.changePassword),
			policy.AuthRequired(engine, mode),
		)
	}

	// --- registration (AUTH_REGISTRATION_ENABLED) ---
	if m.features.Registration {
		r.Handle(http.MethodPost, "/api/v1/auth/register", httpx.Adapter(m.registerAccount))
	}

	// --- password reset (AUTH_PASSWORD_RESET_ENABLED) ---
	if m.features.PasswordReset {
		r.Handle(http.MethodPost, "/api/v1/auth/password/reset/request", httpx.Adapter(m.passwordResetRequest))
		r.Handle(http.MethodPost, "/api/v1/auth/password/reset/confirm", httpx.Adapter(m.passwordResetConfirm))
	}

	// --- email verification (AUTH_EMAIL_VERIFICATION_ENABLED) ---
	if m.features.EmailVerification {
		r.Handle(http.MethodPost, "/api/v1/auth/email/verify/request", httpx.Adapter(m.emailVerifyRequest))
		r.Handle(http.MethodPost, "/api/v1/auth/email/verify/confirm", httpx.Adapter(m.emailVerifyConfirm))
	}

	// --- TOTP + backup codes (AUTH_TOTP_ENABLED) ---
	if m.features.TOTP {
		r.Handle(http.MethodPost, "/api/v1/auth/mfa/totp/setup", httpx.Adapter(m.totpSetup),
			policy.AuthRequired(engine, mode),
		)
		r.Handle(http.MethodPost, "/api/v1/auth/mfa/totp/confirm", httpx.Adapter(m.totpConfirm),
			policy.AuthRequired(engine, mode),
		)
		r.Handle(http.MethodPost, "/api/v1/auth/mfa/totp/disable", httpx.Adapter(m.totpDisable),
			policy.AuthRequired(engine, mode),
		)
		r.Handle(http.MethodPost, "/api/v1/auth/mfa/backup-codes/regenerate", httpx.Adapter(m.backupCodesRegenerate),
			policy.AuthRequired(engine, mode),
		)
	}

	// --- WebAuthn (WEBAUTHN_ENABLED) ---
	//
	// Registered whenever auth is on, as in v0.8.0: while WebAuthn is disabled
	// goAuth rejects every ceremony (403). See docs/enabling-webauthn.md.
	r.Handle(http.MethodPost, "/api/v1/auth/webauthn/register/begin", httpx.Adapter(m.webAuthnRegisterBegin),
		policy.AuthRequired(engine, mode),
	)
	r.Handle(http.MethodPost, "/api/v1/auth/webauthn/register/finish", httpx.Adapter(m.webAuthnRegisterFinish),
		policy.AuthRequired(engine, mode),
	)
	r.Handle(http.MethodGet, "/api/v1/auth/webauthn/credentials", httpx.Adapter(m.webAuthnListCredentials),
		policy.AuthRequired(engine, mode),
	)
	r.Handle(http.MethodPost, "/api/v1/auth/webauthn/credentials/remove", httpx.Adapter(m.webAuthnRemoveCredential),
		policy.AuthRequired(engine, mode),
	)

	return nil
}
