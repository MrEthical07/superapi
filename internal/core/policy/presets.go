package policy

import (
	"fmt"
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

func TenantRead(opts ...PresetOption) []Policy {
	cfg := applyPresetOptions(opts...)
	requireAuthEngine("TenantRead", cfg)
	requireLimiter("TenantRead", cfg)
	requireCacheManager("TenantRead", cfg)

	rule := cfg.rateLimitRule
	rule.Scope = ratelimit.ScopeTenant

	policies := []Policy{
		AuthRequired(cfg.authEngine, cfg.authMode),
		TenantRequired(),
		RateLimit(cfg.limiter, rule),
		CacheRead(cfg.cacheManager, cache.CacheReadConfig{
			TTL:                cfg.cacheTTL,
			Tags:               append([]string(nil), cfg.cacheTags...),
			AllowAuthenticated: cfg.cacheAllowAuth,
			VaryBy:             cfg.cacheVaryBy,
		}),
	}

	mustValidatePreset("TenantRead", http.MethodGet, "/api/v1/resource/{id}", policies)
	return policies
}

func TenantWrite(opts ...PresetOption) []Policy {
	cfg := applyPresetOptions(opts...)
	requireAuthEngine("TenantWrite", cfg)
	requireLimiter("TenantWrite", cfg)
	requireCacheManager("TenantWrite", cfg)

	rule := cfg.rateLimitRule
	rule.Scope = ratelimit.ScopeTenant
	tags := cfg.invalidateTags
	if !cfg.invalidateTagSet {
		tags = cfg.cacheTags
	}

	policies := []Policy{
		AuthRequired(cfg.authEngine, cfg.authMode),
		TenantRequired(),
		RateLimit(cfg.limiter, rule),
		CacheInvalidate(cfg.cacheManager, cache.CacheInvalidateConfig{Tags: append([]string(nil), tags...)}),
	}

	mustValidatePreset("TenantWrite", http.MethodPost, "/api/v1/resource", policies)
	return policies
}

func PublicRead(opts ...PresetOption) []Policy {
	cfg := applyPresetOptions(opts...)
	requireLimiter("PublicRead", cfg)
	requireCacheManager("PublicRead", cfg)

	rule := cfg.rateLimitRule
	rule.Scope = ratelimit.ScopeIP

	policies := []Policy{
		RateLimit(cfg.limiter, rule),
		CacheRead(cfg.cacheManager, cache.CacheReadConfig{
			TTL:    cfg.cacheTTL,
			Tags:   append([]string(nil), cfg.cacheTags...),
			VaryBy: cache.CacheVaryBy{Method: true},
		}),
	}

	mustValidatePreset("PublicRead", http.MethodGet, "/api/v1/public/resource", policies)
	return policies
}

func requireAuthEngine(name string, cfg presetConfig) {
	if cfg.authEngine == nil {
		panicInvalidRouteConfigf("%s preset requires WithAuthEngine(engine, mode)", name)
	}
}

func requireLimiter(name string, cfg presetConfig) {
	if cfg.limiter == nil {
		panicInvalidRouteConfigf("%s preset requires WithLimiter(limiter)", name)
	}
}

func requireCacheManager(name string, cfg presetConfig) {
	if cfg.cacheManager == nil {
		panicInvalidRouteConfigf("%s preset requires WithCacheManager(manager)", name)
	}
}

func mustValidatePreset(name, method, pattern string, policies []Policy) {
	metas, err := DescribePolicies(policies...)
	if err != nil {
		panicInvalidRouteConfigf("%s preset policies are invalid: %v", name, err)
	}
	if err := ValidateRouteMetadata(method, pattern, metas); err != nil {
		panicInvalidRouteConfigf("%s preset failed validator: %v", name, err)
	}
}

func _validatePresetPolicyOrder(name string, policies []Policy) error {
	metas, err := DescribePolicies(policies...)
	if err != nil {
		return err
	}
	if len(metas) == 0 {
		return fmt.Errorf("%s preset produced no policies", name)
	}
	return ValidateRouteMetadata(http.MethodGet, "/api/v1/resource/{id}", metas)
}
