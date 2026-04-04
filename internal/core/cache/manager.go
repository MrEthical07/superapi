package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

// ObserveFunc records cache operation outcomes for metrics integration.
type ObserveFunc func(route, outcome string)

// CacheVaryBy controls which request dimensions participate in cache key generation.
type CacheVaryBy struct {
	// Method includes HTTP method in cache key.
	Method bool
	// TenantID includes principal tenant in cache key.
	TenantID bool
	// UserID includes principal user in cache key.
	UserID bool
	// Role includes principal role in cache key.
	Role bool
	// PathParams includes selected route params in cache key.
	PathParams []string
	// QueryParams includes selected query params in cache key.
	QueryParams []string
	// Headers includes selected request headers in cache key.
	Headers []string
}

// CacheTagLiteral is a constant key/value dimension appended to a resolved tag.
type CacheTagLiteral struct {
	Key   string
	Value string
}

// CacheTagSpec defines how a route-level tag should be resolved at runtime.
type CacheTagSpec struct {
	// Name is the base tag family name (for example, "project" or "project-list").
	Name string
	// PathParams appends selected path params (for example, "id") to scope invalidation.
	PathParams []string
	// TenantID appends auth tenant id to scope invalidation.
	TenantID bool
	// UserID appends auth user id to scope invalidation.
	UserID bool
	// Literals appends constant dimensions to distinguish related scopes.
	Literals []CacheTagLiteral
}

// CacheReadConfig defines route-level read-cache behavior.
type CacheReadConfig struct {
	// Key optionally overrides route pattern as cache key route segment.
	Key string
	// TTL defines cache entry lifetime and must be greater than zero.
	TTL time.Duration
	// MaxBytes limits cacheable response body bytes for this route.
	MaxBytes int
	// TagSpecs identifies invalidation scopes resolved from request/auth context.
	TagSpecs []CacheTagSpec
	// Methods limits cache reads to selected methods (GET/HEAD by default).
	Methods []string
	// CacheStatuses limits cache writes to selected HTTP statuses (200 by default).
	CacheStatuses []int
	// VaryBy defines dynamic cache key dimensions.
	VaryBy CacheVaryBy
	// FailOpen overrides manager-level fail-open policy when set.
	FailOpen *bool
	// AllowAuthenticated marks authenticated responses as cache-eligible when safely varied.
	AllowAuthenticated bool
}

// CacheInvalidateConfig defines route-level cache invalidation behavior.
type CacheInvalidateConfig struct {
	// TagSpecs identifies invalidation scopes to bump on successful writes.
	TagSpecs []CacheTagSpec
	// FailOpen overrides manager-level fail-open policy when set.
	FailOpen *bool
}

// ReadKeyTemplate stores precomputed static key-building dimensions for a route.
type ReadKeyTemplate struct {
	RoutePart          string
	Method             bool
	TenantID           bool
	UserID             bool
	Role               bool
	PathParams         []string
	QueryParams        []string
	Headers            []string
	TagSpecs           []CacheTagSpec
	AllowAuthenticated bool
}

// ManagerConfig configures cache manager defaults.
type ManagerConfig struct {
	// Env namespaces cache keys by environment.
	Env string
	// FailOpen controls whether read/write failures should bypass instead of failing requests.
	FailOpen bool
	// DefaultMaxBytes sets fallback response size cap when route does not override MaxBytes.
	DefaultMaxBytes int
	// TagVersionCacheTTL controls in-process cache duration for tag version tokens.
	TagVersionCacheTTL time.Duration
	// Observe receives cache outcome signals for metrics.
	Observe ObserveFunc
}

// CachedResponse stores serialized HTTP response data in Redis.
type CachedResponse struct {
	// Status is the cached HTTP status code.
	Status int `json:"status"`
	// Body is the cached HTTP response body.
	Body []byte `json:"body"`
	// ContentType is the cached Content-Type header value.
	ContentType string `json:"content_type,omitempty"`
}

// Manager encapsulates Redis-backed cache operations and key building.
type Manager struct {
	client          redis.UniversalClient
	env             string
	failOpen        bool
	defaultMaxBytes int
	tagTokenTTL     time.Duration
	observe         ObserveFunc
	tagTokenCache   sync.Map
}

type tagTokenCacheEntry struct {
	token     string
	expiresAt time.Time
}

// NewManager constructs a cache manager with validated defaults.
func NewManager(client redis.UniversalClient, cfg ManagerConfig) (*Manager, error) {
	if client == nil {
		return nil, fmt.Errorf("cache manager requires redis client")
	}
	env := strings.TrimSpace(cfg.Env)
	if env == "" {
		env = "dev"
	}
	if cfg.DefaultMaxBytes <= 0 {
		cfg.DefaultMaxBytes = 256 * 1024
	}
	if cfg.TagVersionCacheTTL < 0 {
		cfg.TagVersionCacheTTL = 0
	}
	return &Manager{
		client:          client,
		env:             env,
		failOpen:        cfg.FailOpen,
		defaultMaxBytes: cfg.DefaultMaxBytes,
		tagTokenTTL:     cfg.TagVersionCacheTTL,
		observe:         cfg.Observe,
	}, nil
}

// NormalizeRoute returns a canonical low-cardinality route label used by cache keys.
func NormalizeRoute(route string) string {
	return normalizeRoute(route)
}

// PrepareReadKeyTemplate precomputes static route cache key dimensions.
func PrepareReadKeyTemplate(cfg CacheReadConfig) ReadKeyTemplate {
	routePart := ""
	if customKey := strings.TrimSpace(cfg.Key); customKey != "" {
		routePart = normalizeRoute(customKey)
	}

	return ReadKeyTemplate{
		RoutePart:          routePart,
		Method:             cfg.VaryBy.Method,
		TenantID:           cfg.VaryBy.TenantID,
		UserID:             cfg.VaryBy.UserID,
		Role:               cfg.VaryBy.Role,
		PathParams:         normalizedNames(cfg.VaryBy.PathParams),
		QueryParams:        normalizedNames(cfg.VaryBy.QueryParams),
		Headers:            normalizedNames(cfg.VaryBy.Headers),
		TagSpecs:           normalizeTagSpecs(cfg.TagSpecs),
		AllowAuthenticated: cfg.AllowAuthenticated,
	}
}

// PrepareTagSpecs normalizes tag specs for stable request-time resolution.
func PrepareTagSpecs(specs []CacheTagSpec) []CacheTagSpec {
	return normalizeTagSpecs(specs)
}

// ResolveTagNames resolves runtime tag names from a request and prepared specs.
func ResolveTagNames(r *http.Request, specs []CacheTagSpec) ([]string, error) {
	return resolveTagNamesPrepared(r, specs)
}

// ResolveFailOpen resolves route override or manager default fail-open policy.
func (m *Manager) ResolveFailOpen(override *bool) bool {
	if override != nil {
		return *override
	}
	return m != nil && m.failOpen
}

// DefaultMaxBytes returns manager-level fallback cache size cap.
func (m *Manager) DefaultMaxBytes() int {
	if m == nil {
		return 0
	}
	return m.defaultMaxBytes
}

// Observe emits cache operation signals to configured observer.
func (m *Manager) Observe(route, outcome string) {
	if m == nil || m.observe == nil {
		return
	}
	r := strings.TrimSpace(route)
	if r == "" {
		r = "unknown"
	}
	o := strings.TrimSpace(outcome)
	if o == "" {
		o = "unknown"
	}
	m.observe(r, o)
}

// BuildReadKey builds a deterministic read key for route cache lookup.
//
// Notes:
// - Uses low-cardinality route patterns
// - Supports selected vary dimensions only
// - Includes tag version token for O(1) invalidation
func (m *Manager) BuildReadKey(ctx context.Context, r *http.Request, route string, cfg CacheReadConfig) (string, error) {
	template := PrepareReadKeyTemplate(cfg)
	return m.BuildReadKeyWithTemplate(ctx, r, route, template)
}

// BuildReadKeyWithTemplate builds a deterministic read key using a precomputed template.
func (m *Manager) BuildReadKeyWithTemplate(ctx context.Context, r *http.Request, route string, template ReadKeyTemplate) (string, error) {
	if m == nil {
		return "", fmt.Errorf("cache manager is nil")
	}
	if r == nil {
		return "", fmt.Errorf("request is nil")
	}

	routePart := strings.TrimSpace(template.RoutePart)
	if routePart == "" {
		routePart = normalizeRoute(route)
	}

	queryHash, err := queryParamHashSelected(r.URL.Query(), template.QueryParams)
	if err != nil {
		return "", err
	}

	values := make([]string, 0, 16)
	values = append(values, "route="+routePart)

	if template.Method {
		values = append(values, "method="+strings.ToUpper(strings.TrimSpace(r.Method)))
	}

	principal, hasPrincipal := auth.FromContext(r.Context())
	if template.TenantID {
		values = append(values, "tenant="+strings.TrimSpace(principal.TenantID))
	}
	if template.UserID {
		values = append(values, "user="+strings.TrimSpace(principal.UserID))
	}
	if template.Role {
		values = append(values, "role="+strings.TrimSpace(principal.Role))
	}
	for _, pathParam := range template.PathParams {
		values = append(values, "path."+pathParam+"="+strings.TrimSpace(chi.URLParam(r, pathParam)))
	}
	for _, headerName := range template.Headers {
		values = append(values, "header."+headerName+"="+strings.Join(r.Header.Values(headerName), ","))
	}
	if queryHash != "" {
		values = append(values, "query_hash="+queryHash)
	}
	if hasPrincipal && template.AllowAuthenticated {
		values = append(values, "auth=allowed")
	}

	resolvedTags, err := resolveTagNamesPrepared(r, template.TagSpecs)
	if err != nil {
		return "", err
	}

	tagToken, err := m.tagVersionTokenNormalized(ctx, resolvedTags)
	if err != nil {
		return "", err
	}
	if tagToken != "" {
		values = append(values, "tags="+tagToken)
	}

	canonical := strings.Join(values, "|")
	hash := sha256.Sum256([]byte(canonical))
	shortHash := hex.EncodeToString(hash[:16])
	return fmt.Sprintf("cache:%s:%s:%s", m.env, routePart, shortHash), nil
}

// TagVersionToken returns a stable token representing current tag versions.
func (m *Manager) TagVersionToken(ctx context.Context, tags []string) (string, error) {
	tags = normalizeResolvedTags(tags)
	return m.tagVersionTokenNormalized(ctx, tags)
}

func (m *Manager) tagVersionTokenNormalized(ctx context.Context, tags []string) (string, error) {
	if len(tags) == 0 {
		return "", nil
	}

	cacheKey := strings.Join(tags, "\x1f")
	if token, ok := m.tagTokenFromCache(cacheKey); ok {
		return token, nil
	}

	keys := make([]string, 0, len(tags))
	for _, tag := range tags {
		keys = append(keys, m.tagVersionKey(tag))
	}

	vals, err := m.client.MGet(ctx, keys...).Result()
	if err != nil {
		return "", err
	}

	parts := make([]string, 0, len(tags))
	for i, tag := range tags {
		version := int64(0)
		if i < len(vals) && vals[i] != nil {
			switch n := vals[i].(type) {
			case string:
				if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(n), 10, 64); parseErr == nil && parsed > 0 {
					version = parsed
				}
			case int64:
				if n > 0 {
					version = n
				}
			}
		}
		parts = append(parts, tag+"="+strconv.FormatInt(version, 10))
	}
	token := strings.Join(parts, ",")
	m.storeTagToken(cacheKey, token)
	return token, nil
}

// Get fetches and decodes a cached response by key.
func (m *Manager) Get(ctx context.Context, key string) (CachedResponse, bool, error) {
	if m == nil {
		return CachedResponse{}, false, fmt.Errorf("cache manager is nil")
	}
	payload, err := m.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return CachedResponse{}, false, nil
		}
		return CachedResponse{}, false, err
	}
	var item CachedResponse
	if err := json.Unmarshal(payload, &item); err != nil {
		return CachedResponse{}, false, err
	}
	if item.Status <= 0 {
		return CachedResponse{}, false, nil
	}
	return item, true, nil
}

// Set stores a cached response for the given key and TTL.
func (m *Manager) Set(ctx context.Context, key string, value CachedResponse, ttl time.Duration) error {
	if m == nil {
		return fmt.Errorf("cache manager is nil")
	}
	if ttl <= 0 {
		return fmt.Errorf("cache ttl must be > 0")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return m.client.Set(ctx, key, payload, ttl).Err()
}

// BumpTags increments tag versions used for cache invalidation.
func (m *Manager) BumpTags(ctx context.Context, tags []string) error {
	if m == nil {
		return fmt.Errorf("cache manager is nil")
	}
	tags = normalizeResolvedTags(tags)
	if len(tags) == 0 {
		return nil
	}
	pipe := m.client.Pipeline()
	for _, tag := range tags {
		pipe.Incr(ctx, m.tagVersionKey(tag))
	}
	_, err := pipe.Exec(ctx)
	if err == nil {
		m.tagTokenCache.Range(func(key, value any) bool {
			m.tagTokenCache.Delete(key)
			return true
		})
	}
	return err
}

func (m *Manager) tagTokenFromCache(key string) (string, bool) {
	if m == nil || m.tagTokenTTL <= 0 || strings.TrimSpace(key) == "" {
		return "", false
	}

	entryRaw, ok := m.tagTokenCache.Load(key)
	if !ok {
		return "", false
	}

	entry, ok := entryRaw.(tagTokenCacheEntry)
	if !ok {
		m.tagTokenCache.Delete(key)
		return "", false
	}

	if time.Now().After(entry.expiresAt) {
		m.tagTokenCache.Delete(key)
		return "", false
	}

	return entry.token, true
}

func (m *Manager) storeTagToken(key, token string) {
	if m == nil || m.tagTokenTTL <= 0 || strings.TrimSpace(key) == "" {
		return
	}

	m.tagTokenCache.Store(key, tagTokenCacheEntry{
		token:     token,
		expiresAt: time.Now().Add(m.tagTokenTTL),
	})
}

func (m *Manager) tagVersionKey(tag string) string {
	return fmt.Sprintf("cver:%s:%s", m.env, tag)
}

func normalizedNames(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(strings.ToLower(item))
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func normalizeResolvedTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func normalizeTagSpecs(specs []CacheTagSpec) []CacheTagSpec {
	if len(specs) == 0 {
		return nil
	}

	out := make([]CacheTagSpec, 0, len(specs))
	for _, spec := range specs {
		name := normalizeTagName(spec.Name)
		if name == "" {
			continue
		}
		out = append(out, CacheTagSpec{
			Name:       name,
			PathParams: normalizedNames(spec.PathParams),
			TenantID:   spec.TenantID,
			UserID:     spec.UserID,
			Literals:   normalizeTagLiterals(spec.Literals),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return tagSpecKey(out[i]) < tagSpecKey(out[j])
	})

	dedup := out[:0]
	var last string
	for _, spec := range out {
		key := tagSpecKey(spec)
		if key == last {
			continue
		}
		dedup = append(dedup, spec)
		last = key
	}

	return dedup
}

func normalizeTagLiterals(items []CacheTagLiteral) []CacheTagLiteral {
	if len(items) == 0 {
		return nil
	}

	out := make([]CacheTagLiteral, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(strings.ToLower(item.Key))
		value := strings.TrimSpace(item.Value)
		if key == "" || value == "" {
			continue
		}
		out = append(out, CacheTagLiteral{Key: key, Value: value})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Key == out[j].Key {
			return out[i].Value < out[j].Value
		}
		return out[i].Key < out[j].Key
	})

	dedup := out[:0]
	last := ""
	for _, item := range out {
		key := item.Key + "=" + item.Value
		if key == last {
			continue
		}
		dedup = append(dedup, item)
		last = key
	}

	return dedup
}

func normalizeTagName(name string) string {
	tag := strings.TrimSpace(strings.ToLower(name))
	if tag == "" {
		return ""
	}
	tag = strings.ReplaceAll(tag, " ", "_")
	if len(tag) > 120 {
		return tag[:120]
	}
	return tag
}

func tagSpecKey(spec CacheTagSpec) string {
	parts := make([]string, 0, 8)
	parts = append(parts, spec.Name)
	for _, pathParam := range spec.PathParams {
		parts = append(parts, "path."+pathParam)
	}
	if spec.TenantID {
		parts = append(parts, "tenant")
	}
	if spec.UserID {
		parts = append(parts, "user")
	}
	for _, literal := range spec.Literals {
		parts = append(parts, "lit."+literal.Key+"="+literal.Value)
	}
	return strings.Join(parts, "|")
}

func resolveTagNamesPrepared(r *http.Request, specs []CacheTagSpec) ([]string, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	if r == nil {
		return nil, fmt.Errorf("request is nil")
	}

	principal, _ := auth.FromContext(r.Context())
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		tag, err := resolveTagName(r, principal, spec)
		if err != nil {
			return nil, err
		}
		if tag == "" {
			continue
		}
		out = append(out, tag)
	}

	return normalizeResolvedTags(out), nil
}

func resolveTagName(r *http.Request, principal auth.AuthContext, spec CacheTagSpec) (string, error) {
	if r == nil {
		return "", fmt.Errorf("request is nil")
	}
	if spec.Name == "" {
		return "", nil
	}

	parts := make([]string, 0, len(spec.PathParams)+len(spec.Literals)+2)
	for _, pathParam := range spec.PathParams {
		value := strings.TrimSpace(chi.URLParam(r, pathParam))
		if value == "" {
			return "", fmt.Errorf("missing path param %q for cache tag %q", pathParam, spec.Name)
		}
		parts = append(parts, "path."+pathParam+"="+escapeTagValue(value))
	}

	if spec.TenantID {
		tenantID := strings.TrimSpace(principal.TenantID)
		if tenantID == "" {
			return "", fmt.Errorf("missing tenant id for cache tag %q", spec.Name)
		}
		parts = append(parts, "tenant="+escapeTagValue(tenantID))
	}

	if spec.UserID {
		userID := strings.TrimSpace(principal.UserID)
		if userID == "" {
			return "", fmt.Errorf("missing user id for cache tag %q", spec.Name)
		}
		parts = append(parts, "user="+escapeTagValue(userID))
	}

	for _, literal := range spec.Literals {
		parts = append(parts, "lit."+literal.Key+"="+escapeTagValue(literal.Value))
	}

	if len(parts) == 0 {
		return spec.Name, nil
	}

	sort.Strings(parts)
	return spec.Name + "|" + strings.Join(parts, "|"), nil
}

func escapeTagValue(value string) string {
	return url.QueryEscape(strings.TrimSpace(value))
}

func normalizeRoute(route string) string {
	r := strings.TrimSpace(route)
	if r == "" {
		return "unknown"
	}
	r = strings.ReplaceAll(r, " ", "_")
	if len(r) > 240 {
		return r[:240]
	}
	return r
}

func queryParamHashSelected(values url.Values, selected []string) (string, error) {
	if len(selected) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(selected))
	for _, name := range selected {
		vals := append([]string(nil), values[name]...)
		sort.Strings(vals)
		parts = append(parts, name+"="+strings.Join(vals, ","))
	}
	canonical := strings.Join(parts, "&")
	hash := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(hash[:16]), nil
}
