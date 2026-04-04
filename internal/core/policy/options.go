package policy

import (
	"strings"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

// PresetOption mutates preset behavior used by TenantRead/TenantWrite/PublicRead.
type PresetOption func(*presetConfig)

type presetConfig struct {
	authEngine *goauth.Engine
	authMode   auth.Mode
	strictAuth bool

	limiter       ratelimit.Limiter
	rateLimitRule ratelimit.Rule
	rateLimitSet  bool

	cacheManager     *cache.Manager
	cacheTTL         time.Duration
	cacheTagSpecs    []cache.CacheTagSpec
	cacheConfigured  bool
	cacheAllowAuth   bool
	cacheVaryBy      cache.CacheVaryBy
	invalidateTagCfg []cache.CacheTagSpec
	invalidateTagSet bool
	tenantMatchParam string
}

func defaultPresetConfig() presetConfig {
	return presetConfig{
		authMode: auth.ModeHybrid,
		rateLimitRule: ratelimit.Rule{
			Limit:  30,
			Window: time.Minute,
		},
		cacheTTL:         30 * time.Second,
		cacheTagSpecs:    []cache.CacheTagSpec{{Name: "resource"}},
		cacheAllowAuth:   true,
		cacheVaryBy:      cache.CacheVaryBy{TenantID: true},
		invalidateTagCfg: []cache.CacheTagSpec{{Name: "resource"}},
		tenantMatchParam: "tenant_id",
	}
}

func applyPresetOptions(opts ...PresetOption) presetConfig {
	cfg := defaultPresetConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}
	if cfg.strictAuth {
		cfg.authMode = auth.ModeStrict
	}
	return cfg
}

// WithAuthEngine sets auth engine and mode for preset-generated auth policies.
func WithAuthEngine(engine *goauth.Engine, mode auth.Mode) PresetOption {
	return func(cfg *presetConfig) {
		cfg.authEngine = engine
		if strings.TrimSpace(string(mode)) != "" {
			cfg.authMode = mode
		}
	}
}

// WithLimiter sets the limiter used by preset-generated rate limit policies.
func WithLimiter(limiter ratelimit.Limiter) PresetOption {
	return func(cfg *presetConfig) {
		cfg.limiter = limiter
	}
}

// WithCacheManager sets the cache manager used by preset-generated cache policies.
func WithCacheManager(manager *cache.Manager) PresetOption {
	return func(cfg *presetConfig) {
		cfg.cacheManager = manager
	}
}

// WithCache configures cache TTL and tag specs used by preset-generated cache read policy.
func WithCache(ttl time.Duration, tagSpecs ...cache.CacheTagSpec) PresetOption {
	return func(cfg *presetConfig) {
		cfg.cacheConfigured = true
		if ttl > 0 {
			cfg.cacheTTL = ttl
		}
		if len(tagSpecs) > 0 {
			cfg.cacheTagSpecs = append([]cache.CacheTagSpec(nil), tagSpecs...)
		}
	}
}

// WithRateLimit configures default limit/window for preset-generated rate limit policy.
func WithRateLimit(limit int, window time.Duration) PresetOption {
	return func(cfg *presetConfig) {
		cfg.rateLimitSet = true
		if limit > 0 {
			cfg.rateLimitRule.Limit = limit
		}
		if window > 0 {
			cfg.rateLimitRule.Window = window
		}
	}
}

// WithStrictAuth forces auth mode strict regardless of previous mode options.
func WithStrictAuth() PresetOption {
	return func(cfg *presetConfig) {
		cfg.strictAuth = true
	}
}

// WithInvalidateTags sets tag specs used by preset-generated cache invalidation policy.
func WithInvalidateTags(tagSpecs ...cache.CacheTagSpec) PresetOption {
	return func(cfg *presetConfig) {
		if len(tagSpecs) == 0 {
			return
		}
		cfg.invalidateTagSet = true
		cfg.invalidateTagCfg = append([]cache.CacheTagSpec(nil), tagSpecs...)
	}
}

// WithTenantMatchParam overrides tenant path parameter name used by presets.
func WithTenantMatchParam(param string) PresetOption {
	return func(cfg *presetConfig) {
		trimmed := strings.TrimSpace(param)
		if trimmed != "" {
			cfg.tenantMatchParam = trimmed
		}
	}
}

// WithCacheVaryBy overrides vary dimensions for preset-generated cache reads.
func WithCacheVaryBy(varyBy cache.CacheVaryBy) PresetOption {
	return func(cfg *presetConfig) {
		cfg.cacheVaryBy = varyBy
	}
}
