package tenant

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MrEthical07/superapi/internal/core/auth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/requestid"
	"github.com/MrEthical07/superapi/internal/core/response"
)

// Resolver names.
const (
	ResolverHeader    = "header"
	ResolverSubdomain = "subdomain"
)

// maxTenantIDLength bounds tenant ids accepted from the wire. Tenant ids end up
// in Redis keys (sessions, rate limits, cache), so they are kept short and to a
// conservative character set.
const maxTenantIDLength = 64

// maxCacheEntries bounds the in-process validation cache so a flood of random
// tenant ids cannot grow it without limit.
const maxCacheEntries = 10_000

// ResolverConfig configures the tenant resolution middleware.
type ResolverConfig struct {
	// Resolver is "header" (default) or "subdomain".
	Resolver string
	// Header is the header carrying the tenant id for the header resolver.
	Header string
	// BaseDomain is the parent domain for the subdomain resolver.
	BaseDomain string
	// ExemptPaths are exact request paths that skip resolution (health and
	// metrics endpoints).
	ExemptPaths []string
	// Directory validates that the tenant exists and is active. Nil disables
	// validation (TENANCY_VALIDATE=false).
	Directory Directory
	// CacheTTL caches validation results in-process. 0 disables caching.
	CacheTTL time.Duration
}

// Errors returned to clients are built per request (apperr values are mutable).
// Unknown and inactive tenants share one response so the endpoint does not
// reveal which tenant ids exist but are disabled.
func errTenantRequired() *apperr.AppError {
	return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "tenant required")
}

func errTenantInvalid() *apperr.AppError {
	return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "tenant invalid")
}

func errTenantNotFound() *apperr.AppError {
	return apperr.New(apperr.CodeNotFound, http.StatusNotFound, "tenant not found")
}

func errTenantUnavailable(cause error) *apperr.AppError {
	return apperr.WithCause(apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "tenant lookup unavailable"), cause)
}

// Middleware resolves the request tenant, validates it, and attaches it to the
// request context with auth.WithRequestTenant (which also calls
// goauth.WithTenantID so goAuth scopes lookups, sessions and reset/verification
// records to it).
//
// Responses:
//   - 400 bad_request "tenant required" when no tenant can be resolved
//   - 400 bad_request "tenant invalid" when the tenant id is malformed
//   - 404 not_found "tenant not found" when the tenant is unknown or inactive
//   - 503 dependency_unavailable when validation cannot reach the database
//
// Exempt paths (health/readiness/metrics) pass through untouched.
//
// Wire it only when TENANCY_ENABLED=true; see internal/core/app.
func Middleware(cfg ResolverConfig) func(http.Handler) http.Handler {
	exempt := make(map[string]struct{}, len(cfg.ExemptPaths))
	for _, p := range cfg.ExemptPaths {
		if p = strings.TrimSpace(p); p != "" {
			exempt[p] = struct{}{}
		}
	}

	extract := headerExtractor(cfg.Header)
	if strings.EqualFold(strings.TrimSpace(cfg.Resolver), ResolverSubdomain) {
		extract = subdomainExtractor(cfg.BaseDomain)
	}

	var cache *validationCache
	if cfg.Directory != nil && cfg.CacheTTL > 0 {
		cache = newValidationCache(cfg.CacheTTL)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := exempt[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}

			rid := requestid.FromContext(r.Context())
			tenantID := extract(r)
			if tenantID == "" {
				response.Error(w, errTenantRequired(), rid)
				return
			}
			if !ValidTenantID(tenantID) {
				response.Error(w, errTenantInvalid(), rid)
				return
			}

			if cfg.Directory != nil {
				active, err := lookupActive(r.Context(), cfg.Directory, cache, tenantID)
				if err != nil {
					response.Error(w, errTenantUnavailable(err), rid)
					return
				}
				if !active {
					response.Error(w, errTenantNotFound(), rid)
					return
				}
			}

			next.ServeHTTP(w, r.WithContext(auth.WithRequestTenant(r.Context(), tenantID)))
		})
	}
}

// ValidTenantID reports whether id is an acceptable tenant id: 1-64 characters
// from [A-Za-z0-9._-], starting with an alphanumeric.
func ValidTenantID(id string) bool {
	if id == "" || len(id) > maxTenantIDLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case i > 0 && (c == '.' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return true
}

func headerExtractor(header string) func(*http.Request) string {
	header = strings.TrimSpace(header)
	if header == "" {
		header = "X-Tenant-ID"
	}
	return func(r *http.Request) string {
		values := r.Header.Values(header)
		if len(values) != 1 {
			// Absent, or ambiguous (repeated header): refuse to pick one.
			return ""
		}
		return strings.TrimSpace(values[0])
	}
}

func subdomainExtractor(baseDomain string) func(*http.Request) string {
	suffix := "." + strings.ToLower(strings.Trim(strings.TrimSpace(baseDomain), "."))
	return func(r *http.Request) string {
		host := strings.ToLower(strings.TrimSpace(r.Host))
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.TrimSuffix(host, ".")
		if !strings.HasSuffix(host, suffix) {
			return ""
		}
		label := strings.TrimSuffix(host, suffix)
		if label == "" || strings.Contains(label, ".") {
			// Only a single label directly under the base domain is a tenant.
			return ""
		}
		return label
	}
}

func lookupActive(ctx context.Context, dir Directory, cache *validationCache, tenantID string) (bool, error) {
	if cache != nil {
		if active, ok := cache.get(tenantID); ok {
			return active, nil
		}
	}

	record, err := dir.Get(ctx, tenantID)
	switch {
	case errors.Is(err, ErrTenantNotFound):
		cache.put(tenantID, false)
		return false, nil
	case err != nil:
		return false, err
	}

	active := record.Active()
	cache.put(tenantID, active)
	return active, nil
}

type cacheEntry struct {
	active  bool
	expires time.Time
}

// validationCache is a small TTL cache of tenant validation results. Negative
// results are cached too so unknown ids cannot be used to hammer the database.
type validationCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]cacheEntry
	now     func() time.Time
}

func newValidationCache(ttl time.Duration) *validationCache {
	return &validationCache{ttl: ttl, entries: make(map[string]cacheEntry), now: time.Now}
}

func (c *validationCache) get(tenantID string) (bool, bool) {
	if c == nil {
		return false, false
	}
	c.mu.RLock()
	entry, ok := c.entries[tenantID]
	c.mu.RUnlock()
	if !ok || c.now().After(entry.expires) {
		return false, false
	}
	return entry.active, true
}

func (c *validationCache) put(tenantID string, active bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxCacheEntries {
		// Simple bounded behavior: drop everything rather than track LRU order.
		c.entries = make(map[string]cacheEntry)
	}
	c.entries[tenantID] = cacheEntry{active: active, expires: c.now().Add(c.ttl)}
}
