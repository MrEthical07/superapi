package policy

import (
	"bufio"
	"net"
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/requestid"
	"github.com/MrEthical07/superapi/internal/core/response"
)

const (
	cacheOutcomeHit    = "hit"
	cacheOutcomeMiss   = "miss"
	cacheOutcomeSet    = "set"
	cacheOutcomeBypass = "bypass"
	cacheOutcomeError  = "error"
)

// CacheRead enables route-level response caching backed by cache.Manager.
//
// Behavior:
// - Builds deterministic cache keys using route and selected vary dimensions
// - Serves cached responses on hit
// - Writes successful cacheable responses on miss
//
// Usage:
//
//	r.Handle(http.MethodGet, "/api/v1/projects/{id}", handler,
//	    policy.CacheRead(cacheMgr, cache.CacheReadConfig{
//	        TTL: 30 * time.Second,
//	        Tags: []string{"projects"},
//	        VaryBy: cache.CacheVaryBy{TenantID: true, UserID: true},
//	    }),
//	)
//
// Notes:
// - TTL must be > 0
// - Authenticated cache usage requires VaryBy.UserID or VaryBy.TenantID
func CacheRead(manager *cache.Manager, cfg cache.CacheReadConfig) Policy {
	if manager == nil {
		panicInvalidRouteConfigf("%s requires a non-nil cache manager", PolicyTypeCacheRead)
	}
	if cfg.TTL <= 0 {
		panicInvalidRouteConfigf("%s requires a TTL greater than zero", PolicyTypeCacheRead)
	}

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
			route := routePattern(r)
			failOpen := manager.ResolveFailOpen(cfg.FailOpen)

			if !methodAllowed(r.Method, cfg.Methods) {
				manager.Observe(route, cacheOutcomeBypass)
				next.ServeHTTP(w, r)
				return
			}

			ensureAuthCacheSafety(r, cfg)

			key, err := manager.BuildReadKey(r.Context(), r, route, cfg)
			if err != nil {
				manager.Observe(route, cacheOutcomeError)
				if !failOpen {
					response.Error(w, apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "cache unavailable"), rid)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			cached, hit, err := manager.Get(r.Context(), key)
			if err != nil {
				manager.Observe(route, cacheOutcomeError)
				if !failOpen {
					response.Error(w, apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "cache unavailable"), rid)
					return
				}
			} else if hit {
				if cached.ContentType != "" {
					w.Header().Set("Content-Type", cached.ContentType)
				}
				w.WriteHeader(cached.Status)
				_, _ = w.Write(cached.Body)
				manager.Observe(route, cacheOutcomeHit)
				return
			}

			manager.Observe(route, cacheOutcomeMiss)

			maxBytes := cfg.MaxBytes
			if maxBytes <= 0 {
				maxBytes = manager.DefaultMaxBytes()
			}
			writer := newCachingResponseWriter(w, maxBytes)
			next.ServeHTTP(writer, r)

			if !writer.Cacheable(cfg) {
				manager.Observe(route, cacheOutcomeBypass)
				return
			}

			err = manager.Set(r.Context(), key, cache.CachedResponse{
				Status:      writer.Status(),
				Body:        writer.Body(),
				ContentType: writer.Header().Get("Content-Type"),
			}, cfg.TTL)
			if err != nil {
				manager.Observe(route, cacheOutcomeError)
				return
			}

			manager.Observe(route, cacheOutcomeSet)
		})
	}

	return annotatePolicy(p, Metadata{
		Type: PolicyTypeCacheRead,
		Name: "CacheRead",
		CacheRead: CacheReadMetadata{
			AllowAuthenticated: cfg.AllowAuthenticated,
			VaryByUserID:       cfg.VaryBy.UserID,
			VaryByTenantID:     cfg.VaryBy.TenantID,
		},
	})
}

// CacheInvalidate bumps cache tag versions after successful write operations.
//
// Usage:
//
//	r.Handle(http.MethodPost, "/api/v1/projects", handler,
//	    policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{Tags: []string{"projects"}}),
//	)
func CacheInvalidate(manager *cache.Manager, cfg cache.CacheInvalidateConfig) Policy {
	if manager == nil {
		panicInvalidRouteConfigf("%s requires a non-nil cache manager", PolicyTypeCacheInvalidate)
	}
	if len(cfg.Tags) == 0 {
		panicInvalidRouteConfigf("%s requires at least one cache tag", PolicyTypeCacheInvalidate)
	}

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := routePattern(r)
			recorder := &statusCodeRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(recorder, r)

			if recorder.statusCode < http.StatusOK || recorder.statusCode >= http.StatusMultipleChoices {
				return
			}
			if err := manager.BumpTags(r.Context(), cfg.Tags); err != nil {
				manager.Observe(route, cacheOutcomeError)
			}
		})
	}

	return annotatePolicy(p, Metadata{
		Type: PolicyTypeCacheInvalidate,
		Name: "CacheInvalidate",
		CacheInvalidate: CacheInvalidateMetadata{
			TagCount: len(cfg.Tags),
		},
	})
}

type statusCodeRecorder struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader records response status for cache invalidation decisions.
func (w *statusCodeRecorder) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

type cachingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	maxBytes   int
	body       []byte
	tooLarge   bool
	streaming  bool
}

func newCachingResponseWriter(w http.ResponseWriter, maxBytes int) *cachingResponseWriter {
	return &cachingResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		maxBytes:       maxBytes,
		body:           make([]byte, 0, min(maxBytes, 2048)),
	}
}

// WriteHeader captures status code before forwarding to wrapped writer.
func (w *cachingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write buffers response bytes up to maxBytes for potential cache storage.
func (w *cachingResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if w.maxBytes <= 0 || w.tooLarge {
		return n, err
	}
	if len(w.body)+len(p) > w.maxBytes {
		w.tooLarge = true
		return n, err
	}
	w.body = append(w.body, p...)
	return n, err
}

// Flush marks response as streaming and forwards flush to wrapped writer.
func (w *cachingResponseWriter) Flush() {
	w.streaming = true
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack marks response as streaming and delegates connection hijacking.
func (w *cachingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	w.streaming = true
	return hijacker.Hijack()
}

// Push forwards HTTP/2 server push requests when supported.
func (w *cachingResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

// Status returns the captured response status code.
func (w *cachingResponseWriter) Status() int {
	return w.statusCode
}

// Body returns a copy of the buffered response body.
func (w *cachingResponseWriter) Body() []byte {
	return append([]byte(nil), w.body...)
}

// Cacheable reports whether response satisfies cache policy constraints.
func (w *cachingResponseWriter) Cacheable(cfg cache.CacheReadConfig) bool {
	if w.streaming {
		return false
	}
	if w.tooLarge {
		return false
	}
	if w.Header().Get("Set-Cookie") != "" {
		return false
	}
	status := w.Status()
	statuses := cfg.CacheStatuses
	if len(statuses) == 0 {
		return status == http.StatusOK
	}
	for _, candidate := range statuses {
		if candidate == status {
			return true
		}
	}
	return false
}

func hasAuthPrincipal(r *http.Request) bool {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return false
	}
	return strings.TrimSpace(principal.UserID) != "" || strings.TrimSpace(principal.TenantID) != "" || strings.TrimSpace(principal.Role) != ""
}

func ensureAuthCacheSafety(r *http.Request, cfg cache.CacheReadConfig) {
	if !hasAuthPrincipal(r) {
		return
	}
	if cfg.VaryBy.UserID || cfg.VaryBy.TenantID {
		return
	}
	panicInvalidRouteConfigf("%s on authenticated routes requires VaryBy.UserID or VaryBy.TenantID", PolicyTypeCacheRead)
}

func methodAllowed(method string, configured []string) bool {
	if len(configured) == 0 {
		return method == http.MethodGet || method == http.MethodHead
	}
	m := strings.ToUpper(strings.TrimSpace(method))
	for _, candidate := range configured {
		if m == strings.ToUpper(strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
