package tenancy

import (
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

// tenantPartName labels the tenant in cache keys and tags ("tenant=<id>").
const tenantPartName = "tenant"

func tenantPart(principal auth.AuthContext) string {
	return PrincipalTenant(principal)
}

// CacheVary is the cache key part that varies a cached response by the
// principal's tenant. It is identity-bearing, so it satisfies the rule that an
// authenticated cached response varies by user or identity:
//
//	VaryBy: cache.CacheVaryBy{Parts: []cache.KeyPart{tenancy.CacheVary()}}
func CacheVary() cache.KeyPart {
	return cache.KeyPart{
		Name:     tenantPartName,
		Identity: true,
		Extract:  func(_ *http.Request, principal auth.AuthContext) string { return tenantPart(principal) },
	}
}

// CacheTag is the cache tag part that scopes an invalidation tag to the
// principal's tenant; resolving a tag without a tenant is an error:
//
//	TagSpecs: []cache.CacheTagSpec{{Name: "project", Parts: []cache.KeyPart{tenancy.CacheTag()}}}
func CacheTag() cache.KeyPart {
	return cache.KeyPart{
		Name:     tenantPartName,
		Identity: true,
		Extract:  func(_ *http.Request, principal auth.AuthContext) string { return tenantPart(principal) },
	}
}

// ScopeTenant keys a rate limit by the authenticated tenant.
const ScopeTenant ratelimit.Scope = "tenant"

// KeyByTenant resolves rate-limit identity from the authenticated tenant id.
func KeyByTenant() ratelimit.Keyer {
	return func(r *http.Request) (ratelimit.Scope, string) {
		if r == nil {
			return ratelimit.ScopeAnon, "anonymous"
		}
		principal, ok := auth.FromContext(r.Context())
		if !ok || strings.TrimSpace(PrincipalTenant(principal)) == "" {
			return ratelimit.ScopeAnon, "anonymous"
		}
		return ScopeTenant, PrincipalTenant(principal)
	}
}

// KeyByUserOrTenantOrTokenHash resolves user, then tenant, then token-hash
// identity.
func KeyByUserOrTenantOrTokenHash(prefixLen int) ratelimit.Keyer {
	user := ratelimit.KeyByUser()
	tenant := KeyByTenant()
	token := ratelimit.KeyByTokenHash(prefixLen)
	return func(r *http.Request) (ratelimit.Scope, string) {
		if scope, id := user(r); scope != ratelimit.ScopeAnon {
			return scope, id
		}
		if scope, id := tenant(r); scope != ratelimit.ScopeAnon {
			return scope, id
		}
		if scope, id := token(r); scope != ratelimit.ScopeAnon {
			return scope, id
		}
		return ratelimit.ScopeAnon, "anonymous"
	}
}
