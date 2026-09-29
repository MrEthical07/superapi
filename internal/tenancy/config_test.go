package tenancy

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
)

// clearEnv keeps the tests hermetic with respect to TENANCY_* settings: the
// quality gate also runs the suite with TENANCY_ENABLED=true to exercise the
// tenancy paths elsewhere, and these tests set what they need explicitly.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if key, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(key, "TENANCY_") {
			t.Setenv(key, "")
			_ = os.Unsetenv(key)
		}
	}
}

func coreConfig(postgres bool) *config.Config {
	return &config.Config{Postgres: config.PostgresConfig{Enabled: postgres}}
}

func TestDefaults(t *testing.T) {
	clearEnv(t)
	cfg := LoadConfig()
	if cfg.Enabled {
		t.Fatal("tenancy must default to disabled")
	}
	if cfg.Resolver != ResolverHeader || cfg.Header != "X-Tenant-ID" {
		t.Fatalf("resolver=%q header=%q", cfg.Resolver, cfg.Header)
	}
	if !cfg.Validate || cfg.ValidateCacheTTL != 30*time.Second {
		t.Fatalf("validate=%v ttl=%v", cfg.Validate, cfg.ValidateCacheTTL)
	}
	if got := strings.Join(cfg.ExemptPaths, ","); got != "/healthz,/readyz,/metrics" {
		t.Fatalf("exempt paths = %s", got)
	}
	if len(deprecations()) != 0 {
		t.Fatalf("default config must not report deprecations: %v", deprecations())
	}
}

func TestLint(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		postgres bool
		wantErr  string
	}{
		{name: "header resolver ok", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false"}},
		{name: "unknown resolver", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_RESOLVER": "path"}, wantErr: "invalid tenancy resolver"},
		{name: "subdomain needs base domain", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_RESOLVER": "subdomain"}, wantErr: "TENANCY_BASE_DOMAIN"},
		{name: "subdomain needs validation (slug lookup)", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_RESOLVER": "subdomain", "TENANCY_BASE_DOMAIN": "example.com"}, wantErr: "TENANCY_VALIDATE=true"},
		{name: "subdomain needs postgres", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_RESOLVER": "subdomain", "TENANCY_BASE_DOMAIN": "example.com"}, wantErr: "requires postgres"},
		{name: "subdomain ok", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_RESOLVER": "subdomain", "TENANCY_BASE_DOMAIN": "example.com"}, postgres: true},
		{name: "bad header", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_HEADER": "X Tenant"}, wantErr: "tenancy header"},
		{name: "validate needs postgres", env: map[string]string{"TENANCY_ENABLED": "true"}, wantErr: "requires postgres"},
		{name: "bad exempt path", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_EXEMPT_PATHS": "healthz"}, wantErr: "exempt path"},
		{name: "negative cache ttl", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_VALIDATE_CACHE_TTL": "-1s"}, wantErr: "cache ttl"},
		{name: "resolver ignored when disabled", env: map[string]string{"TENANCY_RESOLVER": "bogus"}},
		// Deprecated: accepted even without TENANCY_ENABLED (no longer an error).
		{name: "deprecated enforce isolation accepted", env: map[string]string{"TENANCY_ENFORCE_ISOLATION": "true"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			err := LoadConfig().Lint(coreConfig(tc.postgres))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected lint error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("lint err=%v want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestEnforceIsolationDeprecationWarning(t *testing.T) {
	clearEnv(t)
	t.Setenv("TENANCY_ENFORCE_ISOLATION", "false")
	warnings := deprecations()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "TENANCY_ENFORCE_ISOLATION") {
		t.Fatalf("warnings=%v", warnings)
	}
}

// Load returns the hooks for the configuration: with tenancy off only the
// tenant attribute and the route rule, with it on the full set.
func TestLoadHooks(t *testing.T) {
	clearEnv(t)

	off, err := New().Load(coreConfig(false))
	if err != nil {
		t.Fatalf("Load (off): %v", err)
	}
	if Enabled() {
		t.Fatal("Enabled() must be false after loading a disabled configuration")
	}
	if off.AuthExtension == nil || off.AuthExtension.Attribute == nil || off.AuthExtension.Check != nil {
		t.Fatalf("off: want the tenant attribute without the binding check, got %+v", off.AuthExtension)
	}
	if off.Middleware != nil || off.UserProvider != nil || off.UserRepository != nil || off.GoAuthConfig != nil {
		t.Fatal("off: tenancy must install nothing else")
	}
	if len(off.RouteRules) != 1 {
		t.Fatalf("off: route rules = %d, want 1", len(off.RouteRules))
	}

	t.Setenv("TENANCY_ENABLED", "true")
	t.Setenv("TENANCY_VALIDATE", "false")
	on, err := New().Load(coreConfig(false))
	if err != nil {
		t.Fatalf("Load (on): %v", err)
	}
	t.Cleanup(func() { active.Store(false) })
	if !Enabled() {
		t.Fatal("Enabled() must be true after loading an enabled configuration")
	}
	if on.AuthExtension.Check == nil || on.Middleware == nil || on.UserProvider == nil || on.UserRepository == nil || on.GoAuthConfig == nil {
		t.Fatalf("on: every hook must be set, got %+v", on)
	}

	t.Setenv("TENANCY_RESOLVER", "bogus")
	if _, err := New().Load(coreConfig(false)); err == nil {
		t.Fatal("Load must fail on an invalid configuration")
	}
}
