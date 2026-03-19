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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

type ObserveFunc func(route, outcome string)

type CacheVaryBy struct {
	Method      bool
	TenantID    bool
	UserID      bool
	Role        bool
	PathParams  []string
	QueryParams []string
	Headers     []string
}

type CacheReadConfig struct {
	Key                string
	TTL                time.Duration
	MaxBytes           int
	Tags               []string
	Methods            []string
	CacheStatuses      []int
	VaryBy             CacheVaryBy
	FailOpen           *bool
	AllowAuthenticated bool
}

type CacheInvalidateConfig struct {
	Tags     []string
	FailOpen *bool
}

type ManagerConfig struct {
	Env             string
	FailOpen        bool
	DefaultMaxBytes int
	Observe         ObserveFunc
}

type CachedResponse struct {
	Status      int    `json:"status"`
	Body        []byte `json:"body"`
	ContentType string `json:"content_type,omitempty"`
}

type Manager struct {
	client          redis.UniversalClient
	env             string
	failOpen        bool
	defaultMaxBytes int
	observe         ObserveFunc
}

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
	return &Manager{
		client:          client,
		env:             env,
		failOpen:        cfg.FailOpen,
		defaultMaxBytes: cfg.DefaultMaxBytes,
		observe:         cfg.Observe,
	}, nil
}

func (m *Manager) ResolveFailOpen(override *bool) bool {
	if override != nil {
		return *override
	}
	return m != nil && m.failOpen
}

func (m *Manager) DefaultMaxBytes() int {
	if m == nil {
		return 0
	}
	return m.defaultMaxBytes
}

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

func (m *Manager) BuildReadKey(ctx context.Context, r *http.Request, route string, cfg CacheReadConfig) (string, error) {
	if m == nil {
		return "", fmt.Errorf("cache manager is nil")
	}
	if r == nil {
		return "", fmt.Errorf("request is nil")
	}

	routePart := normalizeRoute(route)
	if customKey := strings.TrimSpace(cfg.Key); customKey != "" {
		routePart = normalizeRoute(customKey)
	}
	queryHash, err := queryParamHash(r.URL.Query(), cfg.VaryBy.QueryParams)
	if err != nil {
		return "", err
	}

	values := make([]string, 0, 16)
	values = append(values, "route="+routePart)

	if cfg.VaryBy.Method {
		values = append(values, "method="+strings.ToUpper(strings.TrimSpace(r.Method)))
	}

	principal, hasPrincipal := auth.FromContext(r.Context())
	if cfg.VaryBy.TenantID {
		values = append(values, "tenant="+strings.TrimSpace(principal.TenantID))
	}
	if cfg.VaryBy.UserID {
		values = append(values, "user="+strings.TrimSpace(principal.UserID))
	}
	if cfg.VaryBy.Role {
		values = append(values, "role="+strings.TrimSpace(principal.Role))
	}
	for _, pathParam := range normalizedNames(cfg.VaryBy.PathParams) {
		values = append(values, "path."+pathParam+"="+strings.TrimSpace(chi.URLParam(r, pathParam)))
	}
	for _, headerName := range normalizedNames(cfg.VaryBy.Headers) {
		values = append(values, "header."+headerName+"="+strings.Join(r.Header.Values(headerName), ","))
	}
	if queryHash != "" {
		values = append(values, "query_hash="+queryHash)
	}
	if hasPrincipal && cfg.AllowAuthenticated {
		values = append(values, "auth=allowed")
	}

	tagToken, err := m.TagVersionToken(ctx, cfg.Tags)
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

func (m *Manager) TagVersionToken(ctx context.Context, tags []string) (string, error) {
	tags = normalizedNames(tags)
	if len(tags) == 0 {
		return "", nil
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
	return strings.Join(parts, ","), nil
}

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

func (m *Manager) BumpTags(ctx context.Context, tags []string) error {
	if m == nil {
		return fmt.Errorf("cache manager is nil")
	}
	tags = normalizedNames(tags)
	if len(tags) == 0 {
		return nil
	}
	pipe := m.client.Pipeline()
	for _, tag := range tags {
		pipe.Incr(ctx, m.tagVersionKey(tag))
	}
	_, err := pipe.Exec(ctx)
	return err
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

func queryParamHash(values url.Values, selected []string) (string, error) {
	selected = normalizedNames(selected)
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
