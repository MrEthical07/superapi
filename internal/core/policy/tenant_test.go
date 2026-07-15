package policy

import (
	"net/http"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

// withTenancy sets the package tenancy flag for the duration of a test and
// restores the previous value on cleanup, so tests do not contaminate each
// other's view of the flag.
func withTenancy(t *testing.T, enabled bool) {
	t.Helper()
	prev := TenancyEnabled()
	SetTenancyEnabled(enabled)
	t.Cleanup(func() { SetTenancyEnabled(prev) })
}

func TestTenancyDefaultsToEnabled(t *testing.T) {
	// The package zero state must be strict/enabled so unit tests and any
	// consumer that never configures the flag keep tenant-aware behavior.
	if !TenancyEnabled() {
		t.Fatal("expected tenancy to default to enabled in the policy package")
	}
}

func TestTenantPathStrictWhenTenancyEnabled(t *testing.T) {
	withTenancy(t, true)

	// A {tenant_id} route without tenant policies must fail validation.
	err := ValidateRoute(
		http.MethodGet,
		"/api/v1/tenants/{tenant_id}/projects",
		AuthRequired(nil, auth.ModeHybrid),
	)
	if err == nil {
		t.Fatal("expected validation error for {tenant_id} route without tenant policies when tenancy enabled")
	}
}

func TestTenantPathLenientWhenTenancyDisabled(t *testing.T) {
	withTenancy(t, false)

	// With tenancy off, {tenant_id} is an ordinary parameter: the same route
	// must validate cleanly without tenant policies.
	if err := ValidateRoute(
		http.MethodGet,
		"/api/v1/tenants/{tenant_id}/projects",
		AuthRequired(nil, auth.ModeHybrid),
	); err != nil {
		t.Fatalf("expected {tenant_id} route to validate without tenant policies when tenancy disabled, got: %v", err)
	}
}

func TestTenantMatchStillRequiresTenantRequiredWhenDisabled(t *testing.T) {
	withTenancy(t, false)

	// The dependency rule (TenantMatchFromPath requires TenantRequired) still
	// holds whenever the tenant policies are explicitly used, regardless of the
	// flag.
	err := ValidateRoute(
		http.MethodGet,
		"/api/v1/tenants/{tenant_id}/projects",
		AuthRequired(nil, auth.ModeHybrid),
		TenantMatchFromPath("tenant_id"),
	)
	if err == nil {
		t.Fatal("expected TenantMatchFromPath without TenantRequired to fail validation even when tenancy disabled")
	}
}

func TestPresetCacheVaryDefaultFollowsTenancyFlag(t *testing.T) {
	t.Run("enabled -> tenant vary", func(t *testing.T) {
		withTenancy(t, true)
		cfg := defaultPresetConfig()
		if !cfg.cacheVaryBy.TenantID {
			t.Fatal("expected cache vary-by tenant when tenancy enabled")
		}
		if cfg.tenantMatchParam != "tenant_id" {
			t.Fatalf("expected tenant match param default when enabled, got %q", cfg.tenantMatchParam)
		}
	})

	t.Run("disabled -> user vary", func(t *testing.T) {
		withTenancy(t, false)
		cfg := defaultPresetConfig()
		if cfg.cacheVaryBy.TenantID {
			t.Fatal("expected cache vary-by NOT to include tenant when tenancy disabled")
		}
		if !cfg.cacheVaryBy.UserID {
			t.Fatal("expected cache vary-by user when tenancy disabled")
		}
		if cfg.tenantMatchParam != "" {
			t.Fatalf("expected empty tenant match param when disabled, got %q", cfg.tenantMatchParam)
		}
	})
}
