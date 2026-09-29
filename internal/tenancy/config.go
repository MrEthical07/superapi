package tenancy

import (
	"fmt"
	"strings"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
)

// Config configures multi-tenant behavior. It is loaded from TENANCY_* and
// linted by the feature (see docs/environment-variables.md); core config knows
// nothing about it.
//
// When Enabled is false (the default), tenancy is inert: no middleware, no
// goAuth multi-tenant mode, presets vary cached responses by user, and a
// {tenant_id} path parameter is an ordinary parameter. When Enabled is true,
// every request resolves to a tenant and {tenant_id} routes must carry the
// tenant policies.
type Config struct {
	// Enabled turns on multi-tenant policy, cache, and rate-limit behavior, the
	// tenant resolution middleware, and goAuth's tenant-scoped user lookup.
	Enabled bool
	// Resolver selects how the request tenant is resolved: "header" (default)
	// or "subdomain".
	Resolver string
	// Header is the request header carrying the tenant id for the header
	// resolver (default X-Tenant-ID).
	Header string
	// BaseDomain is the parent domain for the subdomain resolver: a host of
	// acme.example.com with BaseDomain example.com resolves tenant "acme".
	BaseDomain string
	// Validate requires the resolved tenant to exist in the tenants table and
	// be active (default true). Requires Postgres.
	Validate bool
	// ValidateCacheTTL bounds how long a tenant lookup result is cached
	// in-process (default 30s). 0 disables the cache.
	ValidateCacheTTL time.Duration
	// ExemptPaths are exact request paths that skip tenant resolution
	// (default /healthz, /readyz, /metrics). The metrics path is always exempt.
	ExemptPaths []string
}

// LoadConfig reads the TENANCY_* environment.
func LoadConfig() Config {
	return Config{
		Enabled:          config.EnvBool("TENANCY_ENABLED", false),
		Resolver:         strings.ToLower(strings.TrimSpace(config.EnvString("TENANCY_RESOLVER", ResolverHeader))),
		Header:           strings.TrimSpace(config.EnvString("TENANCY_HEADER", "X-Tenant-ID")),
		BaseDomain:       strings.ToLower(strings.Trim(strings.TrimSpace(config.EnvString("TENANCY_BASE_DOMAIN", "")), ".")),
		Validate:         config.EnvBool("TENANCY_VALIDATE", true),
		ValidateCacheTTL: config.EnvDuration("TENANCY_VALIDATE_CACHE_TTL", 30*time.Second),
		ExemptPaths:      config.EnvCSV("TENANCY_EXEMPT_PATHS", []string{"/healthz", "/readyz", "/metrics"}),
	}
}

// Lint validates the settings against each other and against core config.
func (c Config) Lint(core *config.Config) error {
	if !c.Enabled {
		return nil
	}
	switch c.Resolver {
	case ResolverHeader:
		if c.Header == "" || strings.ContainsAny(c.Header, " \t\r\n,") {
			return fmt.Errorf("tenancy header must be a single non-empty header name")
		}
	case ResolverSubdomain:
		if c.BaseDomain == "" {
			return fmt.Errorf("tenancy resolver %q requires TENANCY_BASE_DOMAIN", ResolverSubdomain)
		}
		// The subdomain is a slug; only the tenants table can turn it into
		// the tenant id that goAuth and modules use.
		if !c.Validate {
			return fmt.Errorf("tenancy resolver %q looks tenants up by slug in the tenants table, so it requires TENANCY_VALIDATE=true (and Postgres)", ResolverSubdomain)
		}
	default:
		return fmt.Errorf("invalid tenancy resolver: %q (valid: header, subdomain)", c.Resolver)
	}
	if c.Validate && (core == nil || !core.Postgres.Enabled) {
		return fmt.Errorf("tenancy validate requires postgres enabled (set TENANCY_VALIDATE=false to skip tenant existence checks)")
	}
	if c.ValidateCacheTTL < 0 {
		return fmt.Errorf("tenancy validate cache ttl must be >= 0")
	}
	for _, p := range c.ExemptPaths {
		if p == "" || p[0] != '/' {
			return fmt.Errorf("tenancy exempt path must start with '/': %q", p)
		}
	}
	return nil
}

// deprecations lists warnings for retired TENANCY_* settings that are set.
func deprecations() []string {
	if config.EnvDeprecated("TENANCY_ENFORCE_ISOLATION") {
		return []string{"TENANCY_ENFORCE_ISOLATION is deprecated and ignored: goAuth v0.5.0 made MultiTenant.EnforceIsolation a no-op; tenant enforcement is governed by TENANCY_ENABLED alone. Remove it from your environment; it will be rejected in a future release."}
	}
	return nil
}
