package auth

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	goauth "github.com/MrEthical07/goAuth"
)

// envStrictBool reads an optional boolean env var. It returns set=false when
// the variable is unset or blank. Unlike envBool, a value that is not a
// recognised boolean is an error, so a typo in a security setting fails startup
// instead of silently keeping the default.
func envStrictBool(name string) (value, set bool, err error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false, false, nil
	}
	switch strings.ToLower(raw) {
	case "1", "t", "true", "yes", "y", "on":
		return true, true, nil
	case "0", "f", "false", "no", "n", "off":
		return false, true, nil
	}
	return false, false, fmt.Errorf("%s must be a boolean (true/false), got %q", name, raw)
}

// envPositiveInt reads an optional integer env var. It returns set=false when
// the variable is unset or blank, and an error for a value that is not a
// positive integer.
func envPositiveInt(name string) (int, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	if n <= 0 {
		return 0, false, fmt.Errorf("%s must be > 0", name)
	}
	return n, true, nil
}

// applyOptInSecurity applies the two opt-in goAuth hardening settings added in
// goAuth v0.8. Both are off by default, and an unset variable leaves goAuth's
// default untouched. Values are validated even while a setting is off, so a bad
// value fails startup rather than waiting for someone to switch it on.
//
//   - AUTH_LOGIN_IP_LIMITER_ENABLED, AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS (goAuth
//     default 20) and AUTH_LOGIN_IP_LIMITER_COOLDOWN (goAuth default 15m) set
//     Security.LoginIPLimiter, a failure budget per client IP.
//   - AUTH_REFRESH_RECHECK_USER sets Security.RecheckUserOnRefresh, which
//     re-reads the user from the store on every refresh.
//
// See docs/auth-goauth.md ("goAuth v0.8.x") for what each one costs.
func applyOptInSecurity(cfg *goauth.Config) error {
	ipEnabled, _, err := envStrictBool("AUTH_LOGIN_IP_LIMITER_ENABLED")
	if err != nil {
		return err
	}
	maxAttempts, maxSet, err := envPositiveInt("AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS")
	if err != nil {
		return err
	}
	cooldown, cooldownSet, err := envDuration("AUTH_LOGIN_IP_LIMITER_COOLDOWN")
	if err != nil {
		return err
	}
	recheck, _, err := envStrictBool("AUTH_REFRESH_RECHECK_USER")
	if err != nil {
		return err
	}

	if ipEnabled {
		cfg.Security.LoginIPLimiter.Enabled = true
		if maxSet {
			cfg.Security.LoginIPLimiter.MaxAttempts = maxAttempts
		}
		if cooldownSet {
			cfg.Security.LoginIPLimiter.Cooldown = cooldown
		}
	} else if maxSet || cooldownSet {
		slog.Warn("AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS / AUTH_LOGIN_IP_LIMITER_COOLDOWN are set but AUTH_LOGIN_IP_LIMITER_ENABLED is not true; the per-IP login limiter stays off")
	}
	cfg.Security.RecheckUserOnRefresh = recheck
	return nil
}
