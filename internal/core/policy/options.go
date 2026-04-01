package policy

import (
	"strings"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

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
	cacheTags        []string
	cacheConfigured  bool
	cacheAllowAuth   bool
	cacheVaryBy      cache.CacheVaryBy
	invalidateTags   []string
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
		cacheTags:        []string{"resource"},
		cacheAllowAuth:   true,
		cacheVaryBy:      cache.CacheVaryBy{TenantID: true},
		invalidateTags:   []string{"resource"},
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

func WithAuthEngine(engine *goauth.Engine, mode auth.Mode) PresetOption {
	return func(cfg *presetConfig) {
		cfg.authEngine = engine
		if strings.TrimSpace(string(mode)) != "" {
			cfg.authMode = mode
		}
	}
}

func WithLimiter(limiter ratelimit.Limiter) PresetOption {
	return func(cfg *presetConfig) {
		cfg.limiter = limiter
	}
}

func WithCacheManager(manager *cache.Manager) PresetOption {
	return func(cfg *presetConfig) {
		cfg.cacheManager = manager
	}
}

func WithCache(ttl time.Duration, tags ...string) PresetOption {
	return func(cfg *presetConfig) {
		cfg.cacheConfigured = true
		if ttl > 0 {
			cfg.cacheTTL = ttl
		}
		if len(tags) > 0 {
			cfg.cacheTags = append([]string(nil), tags...)
		}
	}
}

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

func WithStrictAuth() PresetOption {
	return func(cfg *presetConfig) {
		cfg.strictAuth = true
	}
}

func WithInvalidateTags(tags ...string) PresetOption {
	return func(cfg *presetConfig) {
		if len(tags) == 0 {
			return
		}
		cfg.invalidateTagSet = true
		cfg.invalidateTags = append([]string(nil), tags...)
	}
}

func WithTenantMatchParam(param string) PresetOption {
	return func(cfg *presetConfig) {
		trimmed := strings.TrimSpace(param)
		if trimmed != "" {
			cfg.tenantMatchParam = trimmed
		}
	}
}

func WithCacheVaryBy(varyBy cache.CacheVaryBy) PresetOption {
	return func(cfg *presetConfig) {
		cfg.cacheVaryBy = varyBy
	}
}
