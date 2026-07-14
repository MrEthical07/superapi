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
	"log/slog"
	"os"
	"strings"
	"time"

	goauth "github.com/MrEthical07/goAuth"
)

// TenancySettings controls goAuth multi-tenant behavior. It is populated from
// application config (TENANCY_ENABLED / TENANCY_ENFORCE_ISOLATION) and passed
// into ProjectGoAuthConfig so the goAuth engine matches the app-wide tenancy
// decision. The zero value leaves multi-tenant behavior off.
type TenancySettings struct {
	// Enabled turns on goAuth multi-tenant handling.
	Enabled bool
	// EnforceIsolation requests strict tenant isolation checks (only meaningful
	// when Enabled is true).
	EnforceIsolation bool
}

func ProjectGoAuthConfig(mode Mode, tenancy TenancySettings) (goauth.Config, error) {
	cfg := goauth.DefaultConfig()

	// ------------------------------------------------------------
	// Validation Mode
	// ------------------------------------------------------------

	cfg.ValidationMode = toGoAuthValidationMode(mode)

	// ------------------------------------------------------------
	// Multi-Tenant
	// ------------------------------------------------------------
	//
	// Follows the application-wide tenancy decision (TENANCY_ENABLED). When
	// tenancy is off this leaves goAuth's tenant handling inert; the default
	// JWT carries no tenant, so enabling it only matters once principals carry
	// a tenant id. See docs/policies.md and the "Removing tenancy" guide.
	cfg.MultiTenant.Enabled = tenancy.Enabled
	cfg.MultiTenant.EnforceIsolation = tenancy.Enabled && tenancy.EnforceIsolation

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

	cfg.Account.Enabled = true
	cfg.Account.DefaultRole = "user"

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

	// ------------------------------------------------------------
	// WebAuthn (goAuth v0.4.0) — scaffolded, disabled by default
	// ------------------------------------------------------------
	//
	// Enabled defaults to false. When left off, goAuth does not require the
	// WebAuthn capability at Build even though StoreUserProvider implements it,
	// and the ceremony endpoints are inert. Enabling is a config + optional
	// migration step (see docs/enabling-webauthn.md).
	applyWebAuthnConfig(&cfg)

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
	//
	// Examples:
	//
	// AUTH_TEST_SHARED_SECRET=benchmark-secret
	// AUTH_TEST_ACCESS_TTL=30s
	// AUTH_TEST_REFRESH_TTL=5m
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
// Password Reset:
//
// cfg.PasswordReset.Enabled = true
// cfg.PasswordReset.Strategy = goauth.ResetOTP
//
// Email Verification:
//
// cfg.EmailVerification.Enabled = true
// cfg.EmailVerification.RequireForLogin = true
//
// TOTP:
//
// cfg.TOTP.Enabled = true
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
