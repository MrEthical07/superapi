package auth

import (
	"strings"
	"testing"
	"time"
)

var optInSecurityVars = []string{
	"AUTH_LOGIN_IP_LIMITER_ENABLED",
	"AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS",
	"AUTH_LOGIN_IP_LIMITER_COOLDOWN",
	"AUTH_REFRESH_RECHECK_USER",
}

// clearOptInSecurity blanks the four variables for the test (t.Setenv restores
// them), so the developer's shell cannot leak into the result.
func clearOptInSecurity(t *testing.T) {
	t.Helper()
	for _, name := range optInSecurityVars {
		t.Setenv(name, "")
	}
}

// Both settings are off by default, and goAuth's own defaults (20 attempts per
// 15 minutes) stay in place when the limiter is switched on without values.
func TestOptInSecurityDefaults(t *testing.T) {
	clearOptInSecurity(t)

	cfg, err := ProjectGoAuthConfig(ModeHybrid, Features{})
	if err != nil {
		t.Fatalf("ProjectGoAuthConfig: %v", err)
	}
	ip := cfg.Security.LoginIPLimiter
	if ip.Enabled || cfg.Security.RecheckUserOnRefresh {
		t.Fatalf("defaults must be off: ip limiter enabled=%v recheck=%v", ip.Enabled, cfg.Security.RecheckUserOnRefresh)
	}
	if ip.MaxAttempts != 20 || ip.Cooldown != 15*time.Minute {
		t.Fatalf("goAuth defaults changed: max=%d cooldown=%v, want 20 and 15m", ip.MaxAttempts, ip.Cooldown)
	}

	t.Setenv("AUTH_LOGIN_IP_LIMITER_ENABLED", "true")
	cfg, err = ProjectGoAuthConfig(ModeHybrid, Features{})
	if err != nil {
		t.Fatalf("ProjectGoAuthConfig: %v", err)
	}
	ip = cfg.Security.LoginIPLimiter
	if !ip.Enabled || ip.MaxAttempts != 20 || ip.Cooldown != 15*time.Minute {
		t.Fatalf("enabled without values = %+v, want enabled with goAuth's 20 and 15m", ip)
	}
	if cfg.Security.RecheckUserOnRefresh {
		t.Fatal("enabling the IP limiter must not enable the refresh recheck")
	}
}

func TestOptInSecurityParsesValues(t *testing.T) {
	clearOptInSecurity(t)
	t.Setenv("AUTH_LOGIN_IP_LIMITER_ENABLED", "yes")
	t.Setenv("AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS", "7")
	t.Setenv("AUTH_LOGIN_IP_LIMITER_COOLDOWN", "2m")
	t.Setenv("AUTH_REFRESH_RECHECK_USER", "1")

	cfg, err := ProjectGoAuthConfig(ModeHybrid, Features{})
	if err != nil {
		t.Fatalf("ProjectGoAuthConfig: %v", err)
	}
	ip := cfg.Security.LoginIPLimiter
	if !ip.Enabled || ip.MaxAttempts != 7 || ip.Cooldown != 2*time.Minute {
		t.Fatalf("limiter = %+v, want enabled, 7, 2m", ip)
	}
	if !cfg.Security.RecheckUserOnRefresh {
		t.Fatal("AUTH_REFRESH_RECHECK_USER=1 did not enable the refresh recheck")
	}
}

// Tuning values without the enable flag leave the limiter off.
func TestOptInSecurityTuningWithoutEnableStaysOff(t *testing.T) {
	clearOptInSecurity(t)
	t.Setenv("AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS", "7")
	t.Setenv("AUTH_LOGIN_IP_LIMITER_COOLDOWN", "2m")

	cfg, err := ProjectGoAuthConfig(ModeHybrid, Features{})
	if err != nil {
		t.Fatalf("ProjectGoAuthConfig: %v", err)
	}
	if cfg.Security.LoginIPLimiter.Enabled {
		t.Fatal("the limiter must stay off without AUTH_LOGIN_IP_LIMITER_ENABLED")
	}
}

// A bad value fails startup with a message naming the variable, whether or not
// the setting it belongs to is switched on.
func TestOptInSecurityRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"AUTH_LOGIN_IP_LIMITER_ENABLED", "maybe"},
		{"AUTH_LOGIN_IP_LIMITER_ENABLED", "2"},
		{"AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS", "many"},
		{"AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS", "0"},
		{"AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS", "-3"},
		{"AUTH_LOGIN_IP_LIMITER_MAX_ATTEMPTS", "1.5"},
		{"AUTH_LOGIN_IP_LIMITER_COOLDOWN", "soon"},
		{"AUTH_LOGIN_IP_LIMITER_COOLDOWN", "0s"},
		{"AUTH_LOGIN_IP_LIMITER_COOLDOWN", "-5m"},
		{"AUTH_REFRESH_RECHECK_USER", "sometimes"},
	} {
		for _, enabled := range []string{"", "true"} {
			t.Run(tc.name+"="+tc.value+"/enabled="+enabled, func(t *testing.T) {
				clearOptInSecurity(t)
				if enabled != "" && tc.name != "AUTH_LOGIN_IP_LIMITER_ENABLED" {
					t.Setenv("AUTH_LOGIN_IP_LIMITER_ENABLED", enabled)
				}
				t.Setenv(tc.name, tc.value)

				_, err := ProjectGoAuthConfig(ModeHybrid, Features{})
				if err == nil {
					t.Fatalf("%s=%q was accepted", tc.name, tc.value)
				}
				if !strings.Contains(err.Error(), tc.name) {
					t.Fatalf("error %q does not name %s", err, tc.name)
				}
			})
		}
	}
}
