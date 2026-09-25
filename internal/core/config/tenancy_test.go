package config

import (
	"strings"
	"testing"
	"time"
)

func TestTenancyDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Tenancy.Enabled {
		t.Fatal("tenancy must default to disabled")
	}
	if cfg.Tenancy.Resolver != TenancyResolverHeader || cfg.Tenancy.Header != "X-Tenant-ID" {
		t.Fatalf("resolver=%q header=%q", cfg.Tenancy.Resolver, cfg.Tenancy.Header)
	}
	if !cfg.Tenancy.Validate || cfg.Tenancy.ValidateCacheTTL != 30*time.Second {
		t.Fatalf("validate=%v ttl=%v", cfg.Tenancy.Validate, cfg.Tenancy.ValidateCacheTTL)
	}
	if len(cfg.Deprecations()) != 0 {
		t.Fatalf("default config must not report deprecations: %v", cfg.Deprecations())
	}
}

func TestTenancyLint(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "header resolver ok", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false"}},
		{name: "unknown resolver", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_RESOLVER": "path"}, wantErr: "invalid tenancy resolver"},
		{name: "subdomain needs base domain", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_RESOLVER": "subdomain"}, wantErr: "TENANCY_BASE_DOMAIN"},
		{name: "subdomain ok", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_RESOLVER": "subdomain", "TENANCY_BASE_DOMAIN": "example.com"}},
		{name: "bad header", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_HEADER": "X Tenant"}, wantErr: "tenancy header"},
		{name: "validate needs postgres", env: map[string]string{"TENANCY_ENABLED": "true"}, wantErr: "requires postgres"},
		{name: "bad exempt path", env: map[string]string{"TENANCY_ENABLED": "true", "TENANCY_VALIDATE": "false", "TENANCY_EXEMPT_PATHS": "healthz"}, wantErr: "exempt path"},
		{name: "resolver ignored when disabled", env: map[string]string{"TENANCY_RESOLVER": "bogus"}},
		// Deprecated: accepted even without TENANCY_ENABLED (no longer an error).
		{name: "deprecated enforce isolation accepted", env: map[string]string{"TENANCY_ENFORCE_ISOLATION": "true"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			err = cfg.Lint()
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
	t.Setenv("TENANCY_ENFORCE_ISOLATION", "false")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	warnings := cfg.Deprecations()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "TENANCY_ENFORCE_ISOLATION") {
		t.Fatalf("warnings=%v", warnings)
	}
}
