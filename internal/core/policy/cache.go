package policy

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

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

type cacheReadRuntime struct {
	manager           *cache.Manager
	cfg               cache.CacheReadConfig
	template          cache.ReadKeyTemplate
	allowedMethods    map[string]struct{}
	cacheStatuses     map[int]struct{}
	maxBytes          int
	requireAuthSafety bool
	routeParts        sync.Map
}

func newCacheReadRuntime(manager *cache.Manager, cfg cache.CacheReadConfig) cacheReadRuntime {
	template := cache.PrepareReadKeyTemplate(cfg)
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = manager.DefaultMaxBytes()
	}

	return cacheReadRuntime{
		manager:           manager,
		cfg:               cfg,
		template:          template,
		allowedMethods:    buildMethodSet(cfg.Methods),
		cacheStatuses:     buildCacheStatusSet(cfg.CacheStatuses),
		maxBytes:          maxBytes,
		requireAuthSafety: !template.UserID && !template.TenantID,
	}
}

func buildMethodSet(configured []string) map[string]struct{} {
	out := make(map[string]struct{}, max(len(configured), 2))
	if len(configured) == 0 {
		out[http.MethodGet] = struct{}{}
		out[http.MethodHead] = struct{}{}
		return out
	}

	for _, candidate := range configured {
		method := strings.ToUpper(strings.TrimSpace(candidate))
		if method == "" {
			continue
		}
		out[method] = struct{}{}
	}

	if len(out) == 0 {
		out[http.MethodGet] = struct{}{}
		out[http.MethodHead] = struct{}{}
	}

	return out
}

func buildCacheStatusSet(statuses []int) map[int]struct{} {
	out := make(map[int]struct{}, max(len(statuses), 1))
	if len(statuses) == 0 {
		out[http.StatusOK] = struct{}{}
		return out
	}

	for _, status := range statuses {
		out[status] = struct{}{}
	}

	if len(out) == 0 {
		out[http.StatusOK] = struct{}{}
	}

	return out
}

func (c *cacheReadRuntime) methodAllowed(method string) bool {
	if c == nil {
		return false
	}
	_, ok := c.allowedMethods[strings.ToUpper(strings.TrimSpace(method))]
	return ok
}

func (c *cacheReadRuntime) routeLabel(route string) string {
	if c == nil {
		return cache.NormalizeRoute(route)
	}
	if c.template.RoutePart != "" {
		return c.template.RoutePart
	}

	key := strings.TrimSpace(route)
	if key == "" {
		key = "unknown"
	}

	if cached, ok := c.routeParts.Load(key); ok {
		value, _ := cached.(string)
		if value != "" {
			return value
		}
	}

	normalized := cache.NormalizeRoute(route)
	c.routeParts.Store(key, normalized)
	return normalized
}

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
//	        TagSpecs: []cache.CacheTagSpec{{Name: "project", PathParams: []string{"id"}}},
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
	if err := validateCacheTagSpecs(cfg.TagSpecs, false); err != nil {
		panicInvalidRouteConfigf("%s tag specs are invalid: %v", PolicyTypeCacheRead, err)
	}

	runtime := newCacheReadRuntime(manager, cfg)

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
			route := routePattern(r)
			routePart := runtime.routeLabel(route)
			failOpen := manager.ResolveFailOpen(cfg.FailOpen)

			if !runtime.methodAllowed(r.Method) {
				manager.Observe(route, cacheOutcomeBypass)
				next.ServeHTTP(w, r)
				return
			}

			if runtime.requireAuthSafety {
				ensureAuthCacheSafety(r)
			}

			key, err := manager.BuildReadKeyWithTemplate(r.Context(), r, routePart, runtime.template)
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

			writer := newCachingResponseWriter(w, runtime.maxBytes, runtime.cacheStatuses)
			next.ServeHTTP(writer, r)

			if !writer.Cacheable() {
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
//	    policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
//	        TagSpecs: []cache.CacheTagSpec{{Name: "project", PathParams: []string{"id"}}},
//	    }),
//	)
func CacheInvalidate(manager *cache.Manager, cfg cache.CacheInvalidateConfig) Policy {
	if manager == nil {
		panicInvalidRouteConfigf("%s requires a non-nil cache manager", PolicyTypeCacheInvalidate)
	}
	if err := validateCacheTagSpecs(cfg.TagSpecs, true); err != nil {
		panicInvalidRouteConfigf("%s tag specs are invalid: %v", PolicyTypeCacheInvalidate, err)
	}

	preparedTagSpecs := cache.PrepareTagSpecs(cfg.TagSpecs)

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := routePattern(r)
			recorder := &statusCodeRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(recorder, r)

			if recorder.statusCode < http.StatusOK || recorder.statusCode >= http.StatusMultipleChoices {
				return
			}
			resolvedTags, err := cache.ResolveTagNames(r, preparedTagSpecs)
			if err != nil {
				manager.Observe(route, cacheOutcomeError)
				return
			}
			if len(resolvedTags) == 0 {
				return
			}
			if err := manager.BumpTags(r.Context(), resolvedTags); err != nil {
				manager.Observe(route, cacheOutcomeError)
			}
		})
	}

	return annotatePolicy(p, Metadata{
		Type: PolicyTypeCacheInvalidate,
		Name: "CacheInvalidate",
		CacheInvalidate: CacheInvalidateMetadata{
			TagSpecCount: len(preparedTagSpecs),
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
	statusCode    int
	maxBytes      int
	cacheStatuses map[int]struct{}
	body          []byte
	tooLarge      bool
	streaming     bool
}

func newCachingResponseWriter(w http.ResponseWriter, maxBytes int, cacheStatuses map[int]struct{}) *cachingResponseWriter {
	return &cachingResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		maxBytes:       maxBytes,
		cacheStatuses:  cacheStatuses,
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
func (w *cachingResponseWriter) Cacheable() bool {
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
	_, ok := w.cacheStatuses[status]
	return ok
}

func hasAuthPrincipal(r *http.Request) bool {
	principal, ok := auth.FromContext(r.Context())
	if !ok {
		return false
	}
	return strings.TrimSpace(principal.UserID) != "" || strings.TrimSpace(principal.TenantID) != "" || strings.TrimSpace(principal.Role) != ""
}

func ensureAuthCacheSafety(r *http.Request) {
	if !hasAuthPrincipal(r) {
		return
	}
	panicInvalidRouteConfigf("%s on authenticated routes requires VaryBy.UserID or VaryBy.TenantID", PolicyTypeCacheRead)
}

func validateCacheTagSpecs(specs []cache.CacheTagSpec, requireAtLeastOne bool) error {
	if len(specs) == 0 {
		if requireAtLeastOne {
			return fmt.Errorf("at least one tag spec is required")
		}
		return nil
	}

	for i, spec := range specs {
		if strings.TrimSpace(spec.Name) == "" {
			return fmt.Errorf("tag spec at index %d has empty name", i)
		}
		for _, param := range spec.PathParams {
			if strings.TrimSpace(param) == "" {
				return fmt.Errorf("tag spec %q contains empty path param", spec.Name)
			}
		}
		for _, literal := range spec.Literals {
			if strings.TrimSpace(literal.Key) == "" {
				return fmt.Errorf("tag spec %q contains literal with empty key", spec.Name)
			}
			if strings.TrimSpace(literal.Value) == "" {
				return fmt.Errorf("tag spec %q contains literal %q with empty value", spec.Name, literal.Key)
			}
		}
	}

	return nil
}
