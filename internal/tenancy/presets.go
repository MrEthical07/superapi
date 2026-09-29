package tenancy

import (
	"net/http"
	"sync/atomic"

	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/policy"
)

// active mirrors whether the feature is switched on (TENANCY_ENABLED). Feature
// Load sets it once at startup, before any module builds a preset; presets read
// it to pick their defaults.
var active atomic.Bool

// Enabled reports whether tenancy is switched on for this process.
func Enabled() bool { return active.Load() }

// presetVaryBy is the default cache key of the tenant presets: by tenant when
// tenancy is on, by user when it is off (a shared cache entry per tenant "0"
// would otherwise serve one user's response to another).
func presetVaryBy(s policy.PresetSettings) cache.CacheVaryBy {
	if s.CacheVaryBySet {
		return s.CacheVaryBy
	}
	if Enabled() {
		return cache.CacheVaryBy{Parts: []cache.KeyPart{CacheVary()}}
	}
	return cache.CacheVaryBy{UserID: true}
}

// TenantRead returns a validated policy chain for tenant-scoped read routes.
//
// Usage:
//
//	policies := tenancy.TenantRead(
//	    policy.WithAuthEngine(engine, auth.ModeStrict),
//	    policy.WithLimiter(limiter),
//	    policy.WithCacheManager(cacheMgr),
//	)
func TenantRead(opts ...policy.PresetOption) []policy.Policy {
	s := policy.ResolvePreset(opts...)
	s.Require("TenantRead", true, true, true)

	rule := s.RateLimit
	rule.Scope = ScopeTenant
	rule.Keyer = KeyByTenant()

	policies := []policy.Policy{
		policy.AuthRequired(s.AuthEngine, s.AuthMode),
		TenantRequired(),
		policy.RateLimit(s.Limiter, rule),
		policy.CacheRead(s.CacheManager, cache.CacheReadConfig{
			TTL:                s.CacheTTL,
			TagSpecs:           s.CacheTags,
			AllowAuthenticated: s.CacheAllowAuth,
			VaryBy:             presetVaryBy(s),
		}),
	}

	policy.MustValidatePreset("TenantRead", http.MethodGet, "/api/v1/resource/{id}", []policy.RouteRule{RouteRule(Enabled())}, policies)
	return policies
}

// TenantWrite returns a validated policy chain for tenant-scoped write routes.
func TenantWrite(opts ...policy.PresetOption) []policy.Policy {
	s := policy.ResolvePreset(opts...)
	s.Require("TenantWrite", true, true, true)

	rule := s.RateLimit
	rule.Scope = ScopeTenant
	rule.Keyer = KeyByTenant()

	policies := []policy.Policy{
		policy.AuthRequired(s.AuthEngine, s.AuthMode),
		TenantRequired(),
		policy.RateLimit(s.Limiter, rule),
		policy.CacheInvalidate(s.CacheManager, cache.CacheInvalidateConfig{TagSpecs: s.InvalidateTags}),
	}

	policy.MustValidatePreset("TenantWrite", http.MethodPost, "/api/v1/resource", []policy.RouteRule{RouteRule(Enabled())}, policies)
	return policies
}
