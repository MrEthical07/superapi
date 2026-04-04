package policy

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CacheControlConfig defines explicit client/proxy caching directives for a route.
type CacheControlConfig struct {
	// Public allows shared caches (CDN/proxy) to store this response.
	Public bool
	// Private restricts caching to end-user caches.
	Private bool
	// NoStore disables response storage in any cache.
	NoStore bool
	// NoCache requires revalidation before reuse.
	NoCache bool
	// MustRevalidate requires stale responses to be revalidated.
	MustRevalidate bool
	// Immutable marks content as immutable for its freshness lifetime.
	Immutable bool
	// MaxAge sets max-age directive for clients.
	MaxAge time.Duration
	// SharedMaxAge sets s-maxage directive for shared caches.
	SharedMaxAge time.Duration
	// StaleWhileRevalidate allows serving stale while revalidating in background.
	StaleWhileRevalidate time.Duration
	// StaleIfError allows serving stale responses when origin errors.
	StaleIfError time.Duration
	// Vary appends response Vary dimensions.
	Vary []string
}

// CacheControl applies explicit Cache-Control/Vary headers on matched routes.
func CacheControl(cfg CacheControlConfig) Policy {
	directive, vary, err := buildCacheControl(cfg)
	if err != nil {
		panicInvalidRouteConfigf("%s is invalid: %v", PolicyTypeCacheControl, err)
	}

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if directive != "" {
				w.Header().Set("Cache-Control", directive)
			}
			for _, value := range vary {
				appendVaryHeader(w.Header(), value)
			}
			next.ServeHTTP(w, r)
		})
	}

	return annotatePolicy(p, Metadata{Type: PolicyTypeCacheControl, Name: "CacheControl"})
}

func buildCacheControl(cfg CacheControlConfig) (string, []string, error) {
	if cfg.MaxAge < 0 || cfg.SharedMaxAge < 0 || cfg.StaleWhileRevalidate < 0 || cfg.StaleIfError < 0 {
		return "", nil, errInvalidCacheControl("durations must be >= 0")
	}
	if cfg.Public && cfg.Private {
		return "", nil, errInvalidCacheControl("public and private cannot both be set")
	}
	if cfg.NoStore {
		if cfg.MaxAge > 0 || cfg.SharedMaxAge > 0 || cfg.Immutable || cfg.StaleWhileRevalidate > 0 || cfg.StaleIfError > 0 {
			return "", nil, errInvalidCacheControl("no-store cannot be combined with max-age, s-maxage, stale directives, or immutable")
		}
	}

	directives := make([]string, 0, 10)
	if cfg.Public {
		directives = append(directives, "public")
	}
	if cfg.Private {
		directives = append(directives, "private")
	}
	if cfg.NoStore {
		directives = append(directives, "no-store")
	}
	if cfg.NoCache {
		directives = append(directives, "no-cache")
	}
	if cfg.MaxAge > 0 && !cfg.NoStore {
		directives = append(directives, "max-age="+strconv.Itoa(int(cfg.MaxAge.Seconds())))
	}
	if cfg.SharedMaxAge > 0 && !cfg.NoStore {
		directives = append(directives, "s-maxage="+strconv.Itoa(int(cfg.SharedMaxAge.Seconds())))
	}
	if cfg.StaleWhileRevalidate > 0 && !cfg.NoStore {
		directives = append(directives, "stale-while-revalidate="+strconv.Itoa(int(cfg.StaleWhileRevalidate.Seconds())))
	}
	if cfg.StaleIfError > 0 && !cfg.NoStore {
		directives = append(directives, "stale-if-error="+strconv.Itoa(int(cfg.StaleIfError.Seconds())))
	}
	if cfg.MustRevalidate {
		directives = append(directives, "must-revalidate")
	}
	if cfg.Immutable && !cfg.NoStore {
		directives = append(directives, "immutable")
	}

	vary := normalizeHeaderList(cfg.Vary)
	if len(directives) == 0 && len(vary) == 0 {
		return "", nil, errInvalidCacheControl("at least one cache directive or vary header is required")
	}
	return strings.Join(directives, ", "), vary, nil
}

func normalizeHeaderList(items []string) []string {
	if len(items) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func appendVaryHeader(h http.Header, value string) {
	existing := h.Values("Vary")
	for _, current := range existing {
		parts := strings.Split(current, ",")
		for _, part := range parts {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	h.Add("Vary", value)
}

type errInvalidCacheControl string

func (e errInvalidCacheControl) Error() string { return string(e) }
