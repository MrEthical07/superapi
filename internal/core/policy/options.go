package policy

import (
	"strings"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

// PresetOption mutates preset behavior used by PublicRead and by presets
// defined outside this package (see ResolvePreset).
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
	cacheVarySet     bool
	invalidateTagCfg []cache.CacheTagSpec
	invalidateTagSet bool
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
		cacheVaryBy:      cache.CacheVaryBy{UserID: true},
		invalidateTagCfg: []cache.CacheTagSpec{{Name: "resource"}},
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

// WithCacheVaryBy overrides vary dimensions for preset-generated cache reads.
func WithCacheVaryBy(varyBy cache.CacheVaryBy) PresetOption {
	return func(cfg *presetConfig) {
		cfg.cacheVaryBy = varyBy
		cfg.cacheVarySet = true
	}
}

// PresetSettings is the resolved configuration of a preset's options. Presets
// defined outside this package (an optional feature's) build their policy
// chains from it.
type PresetSettings struct {
	AuthEngine *goauth.Engine
	AuthMode   auth.Mode
	Limiter    ratelimit.Limiter
	// RateLimit is the default rule (limit and window).
	RateLimit      ratelimit.Rule
	CacheManager   *cache.Manager
	CacheTTL       time.Duration
	CacheTags      []cache.CacheTagSpec
	CacheAllowAuth bool
	CacheVaryBy    cache.CacheVaryBy
	// CacheVaryBySet reports whether WithCacheVaryBy overrode the default.
	CacheVaryBySet bool
	// InvalidateTags is the tag set for write presets (the read tags unless
	// WithInvalidateTags overrode them).
	InvalidateTags []cache.CacheTagSpec
}

// ResolvePreset applies opts over the preset defaults.
func ResolvePreset(opts ...PresetOption) PresetSettings {
	cfg := applyPresetOptions(opts...)
	tags := cfg.invalidateTagCfg
	if !cfg.invalidateTagSet {
		tags = cfg.cacheTagSpecs
	}
	return PresetSettings{
		AuthEngine:     cfg.authEngine,
		AuthMode:       cfg.authMode,
		Limiter:        cfg.limiter,
		RateLimit:      cfg.rateLimitRule,
		CacheManager:   cfg.cacheManager,
		CacheTTL:       cfg.cacheTTL,
		CacheTags:      append([]cache.CacheTagSpec(nil), cfg.cacheTagSpecs...),
		CacheAllowAuth: cfg.cacheAllowAuth,
		CacheVaryBy:    cfg.cacheVaryBy,
		CacheVaryBySet: cfg.cacheVarySet,
		InvalidateTags: append([]cache.CacheTagSpec(nil), tags...),
	}
}

// Require panics with a route-config error when a dependency the named preset
// needs was not supplied through its options.
func (s PresetSettings) Require(name string, needAuth, needLimiter, needCache bool) {
	if needAuth && s.AuthEngine == nil {
		panicInvalidRouteConfigf("%s preset requires WithAuthEngine(engine, mode)", name)
	}
	if needLimiter && s.Limiter == nil {
		panicInvalidRouteConfigf("%s preset requires WithLimiter(limiter)", name)
	}
	if needCache && s.CacheManager == nil {
		panicInvalidRouteConfigf("%s preset requires WithCacheManager(manager)", name)
	}
}

// MustValidatePreset validates a preset's chain against the built-in route
// rules plus the given feature rules and panics if it is invalid.
func MustValidatePreset(name, method, pattern string, rules []RouteRule, policies []Policy) {
	metas, err := DescribePolicies(policies...)
	if err != nil {
		panicInvalidRouteConfigf("%s preset policies are invalid: %v", name, err)
	}
	if err := ValidateRouteMetadataWith(rules, method, pattern, metas); err != nil {
		panicInvalidRouteConfigf("%s preset failed validator: %v", name, err)
	}
}
