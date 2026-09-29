package tenancy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

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
	// validation (TENANCY_VALIDATE=false). The subdomain resolver cannot work
	// without it: it needs the directory to turn a slug into a tenant id.
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
// request context with WithRequestTenant (which also calls
// goauth.WithTenantID so goAuth scopes lookups, sessions and reset/verification
// records to it).
//
// The header resolver reads a tenant id from the request header. The subdomain
// resolver reads a slug from the host (acme.example.com -> "acme"), looks the
// tenant up by tenants.slug, and attaches that tenant's id: the slug itself
// never reaches goAuth, sessions, or module code.
//
// Responses:
//   - 400 bad_request "tenant required" when no tenant can be resolved
//   - 400 bad_request "tenant invalid" when the tenant id or slug is malformed
//   - 404 not_found "tenant not found" when the tenant is unknown or inactive
//   - 503 dependency_unavailable when validation cannot reach the database, or
//     the subdomain resolver has no directory to look slugs up in
//
// Exempt paths (health/readiness/metrics) pass through untouched.
//
// The feature installs it only when TENANCY_ENABLED=true (see Feature.Load).
func Middleware(cfg ResolverConfig) func(http.Handler) http.Handler {
	exempt := make(map[string]struct{}, len(cfg.ExemptPaths))
	for _, p := range cfg.ExemptPaths {
		if p = strings.TrimSpace(p); p != "" {
			exempt[p] = struct{}{}
		}
	}

	resolver := ResolverHeader
	extract := headerExtractor(cfg.Header)
	if strings.EqualFold(strings.TrimSpace(cfg.Resolver), ResolverSubdomain) {
		resolver = ResolverSubdomain
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
			value := extract(r)
			if value == "" {
				response.Error(w, errTenantRequired(), rid)
				return
			}
			valid := ValidTenantID
			if resolver == ResolverSubdomain {
				value = NormalizeSlug(value)
				valid = ValidSlug
			}
			if !valid(value) {
				response.Error(w, errTenantInvalid(), rid)
				return
			}

			tenantID := value
			switch {
			case cfg.Directory != nil:
				id, active, err := lookupTenant(r.Context(), cfg.Directory, cache, resolver, value)
				if err != nil {
					response.Error(w, errTenantUnavailable(err), rid)
					return
				}
				if !active {
					response.Error(w, errTenantNotFound(), rid)
					return
				}
				tenantID = id
			case resolver == ResolverSubdomain:
				// Misconfiguration (config lint refuses TENANCY_VALIDATE=false
				// with this resolver): a slug is not a tenant id, so fail closed
				// rather than attach it.
				response.Error(w, errTenantUnavailable(errNoDirectoryForSubdomain), rid)
				return
			}

			next.ServeHTTP(w, r.WithContext(WithRequestTenant(r.Context(), tenantID)))
		})
	}
}

var errNoDirectoryForSubdomain = errors.New("the subdomain resolver needs a tenant directory (TENANCY_VALIDATE=true)")

// NormalizeSlug returns the canonical form of a tenant slug: trimmed and
// lower-cased. Slugs are stored in this form (hostnames are case-insensitive).
func NormalizeSlug(slug string) string { return strings.ToLower(strings.TrimSpace(slug)) }

// ValidSlug reports whether slug is an acceptable tenant slug: the same
// character rules as ValidTenantID (1-64 characters from [a-z0-9._-] after
// lower-casing, starting with an alphanumeric), and already lower-case.
func ValidSlug(slug string) bool {
	return slug == strings.ToLower(slug) && ValidTenantID(slug)
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

// lookupTenant resolves value (a tenant id for the header resolver, a slug for
// the subdomain resolver) to the tenant's id and whether it may serve requests,
// through the cache. Unknown tenants are reported as inactive.
func lookupTenant(ctx context.Context, dir Directory, cache *validationCache, resolver, value string) (id string, active bool, err error) {
	key := cacheKey(resolver, value)
	if hit, ok := cache.get(key); ok {
		return hit.ID, hit.Active, nil
	}

	var record Record
	if resolver == ResolverSubdomain {
		record, err = dir.GetBySlug(ctx, value)
	} else {
		record, err = dir.Get(ctx, value)
	}
	switch {
	case errors.Is(err, ErrTenantNotFound):
		cache.put(key, cachedTenant{})
		return "", false, nil
	case err != nil:
		return "", false, err
	}

	result := cachedTenant{ID: record.ID, Active: record.Active()}
	cache.put(key, result)
	return result.ID, result.Active, nil
}
