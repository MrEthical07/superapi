// START HERE:
//
// This file is the primary customization point for goAuth.
//
// Projects should configure authentication behavior here instead of modifying
// goauth_provider.go or dependency wiring.
//
// Common customizations:
//
//   - JWT issuer/audience
//   - token lifetimes
//   - signing keys
//   - password reset behavior
//   - email verification behavior
//   - MFA/TOTP settings
//   - session hardening
//   - audit configuration
//   - production security requirements
//
// Recommended workflow:
//
//   1. Start from goauth.DefaultConfig()
//   2. Apply project-specific overrides
//   3. Run goAuth lint checks
//   4. Let Builder.Build() perform final validation
//
// See:
//   docs/auth-goauth.md
//   https://pkg.go.dev/github.com/MrEthical07/goAuth

package auth

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	goauth "github.com/MrEthical07/goAuth"
)

// Features carries the opt-in auth feature flags (AUTH_*_ENABLED). Each flag
// enables one goAuth config section below and the matching endpoint group in
// internal/modules/auth. The zero value keeps every optional feature off.
type Features struct {
	// RegistrationAutoLogin issues tokens from CreateAccount
	// (AUTH_REGISTRATION_AUTO_LOGIN).
	RegistrationAutoLogin bool
	// PasswordReset enables goAuth password reset (AUTH_PASSWORD_RESET_ENABLED).
	PasswordReset bool
	// EmailVerification enables goAuth email verification
	// (AUTH_EMAIL_VERIFICATION_ENABLED); new accounts start pending.
	EmailVerification bool
	// EmailVerificationRequired blocks login until verified
	// (AUTH_EMAIL_VERIFICATION_REQUIRED).
	EmailVerificationRequired bool
	// TOTP enables TOTP MFA and backup codes (AUTH_TOTP_ENABLED).
	TOTP bool
	// TOTPIssuer labels the account in authenticator apps (AUTH_TOTP_ISSUER).
	TOTPIssuer string
	// AllowTestOverrides permits the AUTH_TEST_* perf overrides. The app sets
	// it only for APP_ENV=dev/test; any AUTH_TEST_* value is rejected otherwise.
	AllowTestOverrides bool
}

// ConfigMutator adjusts the goAuth configuration after the project defaults are
// applied and before goAuth's lint runs. Optional features use it to switch on
// the goAuth settings they own.
type ConfigMutator func(*goauth.Config)

// ProjectGoAuthConfig builds the goAuth configuration for this project.
func ProjectGoAuthConfig(mode Mode, features Features, mutators ...ConfigMutator) (goauth.Config, error) {
	cfg := goauth.DefaultConfig()

	// ------------------------------------------------------------
	// Validation Mode
	// ------------------------------------------------------------

	cfg.ValidationMode = toGoAuthValidationMode(mode)

	// ------------------------------------------------------------
	// JWT Behavior
	// ------------------------------------------------------------

	// Access/refresh token lifetimes.
	// Override for project-specific requirements.
	cfg.JWT.AccessTTL = 5 * time.Minute
	cfg.JWT.RefreshTTL = 7 * 24 * time.Hour

	// ------------------------------------------------------------
	// Auth Result Shape
	// ------------------------------------------------------------

	cfg.Result.IncludeRole = true
	cfg.Result.IncludePermissions = true

	// ------------------------------------------------------------
	// Account Settings
	// ------------------------------------------------------------

	// Account.Enabled lets goAuth create accounts at all (cmd/createuser uses
	// it). The public registration endpoint is gated separately by
	// AUTH_REGISTRATION_ENABLED in the auth module. The role is never taken
	// from request input; new accounts always get DefaultRole.
	cfg.Account.Enabled = true
	cfg.Account.DefaultRole = "user"
	cfg.Account.AutoLogin = features.RegistrationAutoLogin

	// ------------------------------------------------------------
	// Password Reset (AUTH_PASSWORD_RESET_ENABLED)
	// ------------------------------------------------------------
	//
	// goAuth default strategy is an opaque high-entropy token, delivered
	// out-of-band by internal/core/notify. Switch to goauth.ResetOTP for
	// numeric codes.
	cfg.PasswordReset.Enabled = features.PasswordReset

	// ------------------------------------------------------------
	// Email Verification (AUTH_EMAIL_VERIFICATION_ENABLED)
	// ------------------------------------------------------------
	//
	// With verification enabled, CreateAccount stores new users as
	// pending_verification; RequireForLogin blocks login until verified.
	cfg.EmailVerification.Enabled = features.EmailVerification
	cfg.EmailVerification.RequireForLogin = features.EmailVerification && features.EmailVerificationRequired

	// ------------------------------------------------------------
	// TOTP + Backup Codes (AUTH_TOTP_ENABLED)
	// ------------------------------------------------------------
	//
	// Secrets are encrypted at rest by the provider (AUTH_TOTP_ENCRYPTION_KEY).
	// Replay protection stays on (goAuth default) and is also enforced in SQL.
	//
	// RequireForLogin makes goAuth challenge users who have enrolled in TOTP
	// (users without TOTP are unaffected); without it enrollment would have no
	// effect on login. RequireForPasswordReset is left off because goAuth then
	// demands a TOTP proof from every reset, including users who never
	// enrolled; enable it only if every account uses TOTP.
	cfg.TOTP.Enabled = features.TOTP
	cfg.TOTP.RequireForLogin = features.TOTP
	if features.TOTP {
		cfg.TOTP.Issuer = strings.TrimSpace(features.TOTPIssuer)
		if cfg.TOTP.Issuer == "" {
			cfg.TOTP.Issuer = "SuperAPI"
		}
	}

	// ------------------------------------------------------------
	// Session Ceiling / Remember-Me (goAuth v0.4.0)
	// ------------------------------------------------------------
	//
	// MaxSessionDuration is the absolute lifetime ceiling for any session,
	// including remember-me sessions. Unset (0) lets goAuth apply its per-mode
	// default. When set it must be >= 1m (goAuth validates this at Build).
	if d, ok, err := envDuration("AUTH_MAX_SESSION_DURATION"); err != nil {
		return goauth.Config{}, err
	} else if ok {
		cfg.Session.MaxSessionDuration = d
	}

	// ------------------------------------------------------------
	// Sliding-Window Auth Limiter (goAuth v0.4.0)
	// ------------------------------------------------------------
	//
	// Selects goAuth's internal auth-abuse limiter counting algorithm.
	// Accepts "fixed" (default) or "sliding"; goAuth validates the value at
	// Build. This governs goAuth's own login/refresh abuse limiter and is
	// independent of SuperAPI's route rate limiter in internal/core/ratelimit.
	if mode := strings.TrimSpace(os.Getenv("AUTH_LIMITER_WINDOW_MODE")); mode != "" {
		cfg.Security.LimiterWindowMode = strings.ToLower(mode)
	}

	// ------------------------------------------------------------
	// Ed25519 Key Rotation (goAuth v0.4.0)
	// ------------------------------------------------------------
	//
	// VerifyKeys maps key IDs (kid) to Ed25519 public-key material so tokens
	// signed under a retired kid still verify during an overlap window. goAuth
	// couples this: when VerifyKeys is non-empty, KeyID must be set AND present
	// in the map (else Build fails). We therefore require AUTH_KEY_ID and at
	// least the active key whenever any verify key is supplied — set both or
	// neither. Keys are PEM- or raw-encoded per AUTH_VERIFY_KEYS below.
	if err := applyKeyRotation(&cfg); err != nil {
		return goauth.Config{}, err
	}

	// template:begin webauthn
	// ------------------------------------------------------------
	// WebAuthn (goAuth v0.4.0) — scaffolded, disabled by default
	// ------------------------------------------------------------
	//
	// Enabled defaults to false. When left off, goAuth does not require the
	// WebAuthn capability at Build even though StoreUserProvider implements it,
	// and the ceremony endpoints are inert. Enabling is a config + optional
	// migration step (see docs/enabling-webauthn.md).
	applyWebAuthnConfig(&cfg)
	// template:end webauthn

	// ------------------------------------------------------------
	// Example: JWT Identity Overrides
	// ------------------------------------------------------------
	//
	// Uncomment and customize if your project requires fixed JWT identity.
	//
	// cfg.JWT.Issuer = getenv("AUTH_ISSUER", "my-service")
	// cfg.JWT.Audience = getenv("AUTH_AUDIENCE", "my-service-api")
	// cfg.JWT.KeyID = getenv("AUTH_KEY_ID", "v1")
	//
	// For production deployments you may also provide explicit key material:
	//
	// cfg.JWT.PrivateKey = []byte(getenv("AUTH_PRIVATE_KEY", ""))
	// cfg.JWT.PublicKey = []byte(getenv("AUTH_PUBLIC_KEY", ""))
	//
	// When keys are not provided, goAuth automatically generates an Ed25519
	// keypair for development and local testing.

	// ------------------------------------------------------------
	// Example: Production Security Profile
	// ------------------------------------------------------------
	//
	// Recommended for production deployments:
	//
	// cfg.Security.ProductionMode = true
	//
	// ProductionMode enables stricter validation for:
	//   - JWT TTL limits
	//   - password hashing parameters
	//   - OTP configuration
	//   - account recovery safeguards

	// ------------------------------------------------------------
	// Performance Testing Overrides
	// ------------------------------------------------------------
	//
	// AUTH_TEST_* variables are intended only for local benchmarking,
	// load testing, and deterministic multi-process test environments.
	//
	// These allow multiple benchmark workers/processes to share the same signer
	// and token lifetimes.
	//
	// These variables should not be used for normal application configuration.
	// They switch JWT signing to a shared HS256 secret, so they are refused
	// (startup fails) unless APP_ENV is dev or test.
	//
	// Examples:
	//
	// AUTH_TEST_SHARED_SECRET=benchmark-secret
	// AUTH_TEST_ACCESS_TTL=30s
	// AUTH_TEST_REFRESH_TTL=5m
	if err := applyTestOverrides(&cfg, features.AllowTestOverrides); err != nil {
		return goauth.Config{}, err
	}
	for _, mutate := range mutators {
		if mutate != nil {
			mutate(&cfg)
		}
	}
	// Run goAuth advisory lint checks.
	//
	// Lint warnings help identify risky or unusual configurations.
	// High-severity findings are treated as startup failures.
	warnings := cfg.Lint()

	for _, w := range warnings {
		slog.Warn(
			"goauth config lint",
			"code", w.Code,
			"severity", w.Severity.String(),
			"msg", w.Message,
		)
	}

	// Fail startup on high-severity security/configuration issues.
	if err := warnings.AsError(goauth.LintHigh); err != nil {
		return goauth.Config{}, err
	}
	return cfg, nil
}

// Common project customizations:
//
// Password reset / email verification strategy (numeric codes):
//
// cfg.PasswordReset.Strategy = goauth.ResetOTP
// cfg.EmailVerification.Strategy = goauth.VerificationOTP
//
// Require TOTP for every login:
//
// cfg.TOTP.RequireForLogin = true
//
// Session Hardening:
//
// cfg.SessionHardening.EnforceSingleSession = true
//
// Production Security:
//
// cfg.Security.ProductionMode = true

func toGoAuthValidationMode(mode Mode) goauth.ValidationMode {
	switch mode {
	case ModeJWTOnly:
		return goauth.ModeJWTOnly
	case ModeStrict:
		return goauth.ModeStrict
	case ModeHybrid:
		return goauth.ModeHybrid
	default:
		return goauth.ModeHybrid
	}
}

// testOverrideKeys are the AUTH_TEST_* perf overrides.
var testOverrideKeys = []string{"AUTH_TEST_SHARED_SECRET", "AUTH_TEST_ACCESS_TTL", "AUTH_TEST_REFRESH_TTL"}

// applyTestOverrides applies AUTH_TEST_* perf overrides. When allowed is
// false (APP_ENV is not dev/test) any override present is an error, so a
// shared HS256 signing secret can never silently reach production.
func applyTestOverrides(cfg *goauth.Config, allowed bool) error {
	if !allowed {
		for _, key := range testOverrideKeys {
			if strings.TrimSpace(os.Getenv(key)) != "" {
				return fmt.Errorf("%s is only allowed with APP_ENV=dev or APP_ENV=test", key)
			}
		}
		return nil
	}

	if sharedSecret := strings.TrimSpace(os.Getenv("AUTH_TEST_SHARED_SECRET")); sharedSecret != "" {
		cfg.JWT.SigningMethod = "hs256"
		cfg.JWT.PrivateKey = []byte(sharedSecret)
		cfg.JWT.PublicKey = []byte(sharedSecret)
		cfg.JWT.Issuer = "superapi-perf"
		cfg.JWT.Audience = "superapi-perf"
		cfg.JWT.KeyID = "superapi-perf-key"
	}
	if accessTTLRaw := strings.TrimSpace(os.Getenv("AUTH_TEST_ACCESS_TTL")); accessTTLRaw != "" {
		if d, err := time.ParseDuration(accessTTLRaw); err == nil && d > 0 {
			cfg.JWT.AccessTTL = d
		}
	}
	if refreshTTLRaw := strings.TrimSpace(os.Getenv("AUTH_TEST_REFRESH_TTL")); refreshTTLRaw != "" {
		if d, err := time.ParseDuration(refreshTTLRaw); err == nil && d > 0 {
			cfg.JWT.RefreshTTL = d
		}
	}
	return nil
}
