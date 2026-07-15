package policy

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/params"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
	"github.com/MrEthical07/superapi/internal/core/requestid"
	"github.com/MrEthical07/superapi/internal/core/response"
	"github.com/MrEthical07/superapi/internal/core/tenant"
)

// tenancyEnabled gates tenant-aware defaults and validator strictness across the
// policy package. It defaults to true so that the package's zero state — used by
// unit tests and any consumer that never configures it — preserves strict tenant
// behavior. Application startup calls SetTenancyEnabled from config
// (TENANCY_ENABLED, default false), so a default deployment runs with tenancy
// off and {tenant_id} routes treated as ordinary parameters.
var tenancyEnabled atomic.Bool

func init() {
	tenancyEnabled.Store(true)
}

// SetTenancyEnabled configures whether tenancy is active for preset defaults and
// route validation. It is intended to be called once during startup, before any
// route registration or preset construction.
func SetTenancyEnabled(enabled bool) {
	tenancyEnabled.Store(enabled)
}

// TenancyEnabled reports whether tenancy-aware behavior is active.
func TenancyEnabled() bool {
	return tenancyEnabled.Load()
}

// TenantRequired ensures authenticated requests carry tenant scope.
//
// Behavior:
// - Returns 401 when authentication context is absent
// - Returns 403 when tenant scope is missing
//
// Notes:
// - Required for tenant-isolated routes
// - Place after AuthRequired
func TenantRequired() Policy {
	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
			if _, ok := auth.FromContext(r.Context()); !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), rid)
				return
			}
			if err := tenant.RequireTenant(r.Context()); err != nil {
				response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required"), rid)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	return annotatePolicy(p, Metadata{Type: PolicyTypeTenantRequired, Name: "TenantRequired"})
}

// TenantMatchFromPath enforces tenant isolation using a route path parameter.
//
// Behavior:
// - Returns 400 when route tenant parameter is missing
// - Returns 401 when auth context is missing
// - Returns 404 when principal tenant and route tenant mismatch
//
// Usage:
//
//	r.Handle(http.MethodGet, "/api/v1/tenants/{tenant_id}/projects", handler,
//	    policy.AuthRequired(engine, mode),
//	    policy.TenantRequired(),
//	    policy.TenantMatchFromPath("tenant_id"),
//	)
func TenantMatchFromPath(paramName string) Policy {
	paramName = strings.TrimSpace(paramName)
	if paramName == "" {
		panicInvalidRouteConfigf("%s requires a non-empty path parameter name", PolicyTypeTenantMatchFromPath)
	}

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
			principal, ok := auth.FromContext(r.Context())
			if !ok {
				response.Error(w, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), rid)
				return
			}

			resourceTenant := strings.TrimSpace(params.URLParam(r, paramName))
			if resourceTenant == "" {
				response.Error(w, apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, paramName+" is required"), rid)
				return
			}
			if strings.TrimSpace(principal.TenantID) == "" {
				response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "tenant scope required"), rid)
				return
			}
			if !tenant.IsSameTenant(principal.TenantID, resourceTenant) {
				response.Error(w, apperr.New(apperr.CodeNotFound, http.StatusNotFound, "not found"), rid)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	return annotatePolicy(p, Metadata{
		Type:            PolicyTypeTenantMatchFromPath,
		Name:            "TenantMatchFromPath",
		TenantPathParam: paramName,
	})
}

// TenantRead returns a validated policy chain for tenant-scoped read routes.
//
// Usage:
//
//	policies := policy.TenantRead(
//	    policy.WithAuthEngine(engine, auth.ModeStrict),
//	    policy.WithLimiter(limiter),
//	    policy.WithCacheManager(cacheMgr),
//	)
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
			TagSpecs:           append([]cache.CacheTagSpec(nil), cfg.cacheTagSpecs...),
			AllowAuthenticated: cfg.cacheAllowAuth,
			VaryBy:             cfg.cacheVaryBy,
		}),
	}

	mustValidatePreset("TenantRead", http.MethodGet, "/api/v1/resource/{id}", policies)
	return policies
}

// TenantWrite returns a validated policy chain for tenant-scoped write routes.
func TenantWrite(opts ...PresetOption) []Policy {
	cfg := applyPresetOptions(opts...)
	requireAuthEngine("TenantWrite", cfg)
	requireLimiter("TenantWrite", cfg)
	requireCacheManager("TenantWrite", cfg)

	rule := cfg.rateLimitRule
	rule.Scope = ratelimit.ScopeTenant
	tagSpecs := cfg.invalidateTagCfg
	if !cfg.invalidateTagSet {
		tagSpecs = cfg.cacheTagSpecs
	}

	policies := []Policy{
		AuthRequired(cfg.authEngine, cfg.authMode),
		TenantRequired(),
		RateLimit(cfg.limiter, rule),
		CacheInvalidate(cfg.cacheManager, cache.CacheInvalidateConfig{TagSpecs: append([]cache.CacheTagSpec(nil), tagSpecs...)}),
	}

	mustValidatePreset("TenantWrite", http.MethodPost, "/api/v1/resource", policies)
	return policies
}
