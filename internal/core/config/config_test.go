package config

import "testing"

func TestLintRejectsInvalidMiddlewareBoolEnv(t *testing.T) {
	t.Setenv("HTTP_MIDDLEWARE_REQUEST_ID_ENABLED", "not-a-bool")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for invalid middleware bool env")
	}
}

func TestLintRejectsNegativeMiddlewareBodyLimit(t *testing.T) {
	t.Setenv("HTTP_MIDDLEWARE_MAX_BODY_BYTES", "-1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for negative max body bytes")
	}
}

func TestLintRejectsEnabledPostgresWithoutURL(t *testing.T) {
	t.Setenv("POSTGRES_ENABLED", "true")
	t.Setenv("POSTGRES_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for enabled postgres without URL")
	}
}

func TestLintRejectsEnabledRedisWithoutAddr(t *testing.T) {
	t.Setenv("REDIS_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.Redis.Addr = ""

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for enabled redis without addr")
	}
}

func TestLintRejectsInvalidRedisPoolSize(t *testing.T) {
	t.Setenv("REDIS_POOL_SIZE", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for redis pool size")
	}
}

func TestLintRejectsInvalidMetricsPath(t *testing.T) {
	t.Setenv("METRICS_PATH", "metrics")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for metrics path")
	}
}

func TestLintRejectsInvalidAccessLogSampleRate(t *testing.T) {
	t.Setenv("HTTP_MIDDLEWARE_ACCESS_LOG_SAMPLE_RATE", "1.2")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for access log sample rate")
	}
}

func TestLintRejectsInvalidAccessLogExcludePath(t *testing.T) {
	t.Setenv("HTTP_MIDDLEWARE_ACCESS_LOG_EXCLUDE_PATHS", "healthz,/readyz")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for access log exclude path")
	}
}

func TestLintRejectsInvalidTracingExcludePath(t *testing.T) {
	t.Setenv("HTTP_MIDDLEWARE_TRACING_EXCLUDE_PATHS", "healthz,/readyz")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for tracing exclude path")
	}
}

func TestLintRejectsInvalidMetricsExcludePath(t *testing.T) {
	t.Setenv("METRICS_EXCLUDE_PATHS", "healthz,/readyz")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for metrics exclude path")
	}
}

func TestLintRejectsNegativeCacheTagVersionCacheTTL(t *testing.T) {
	t.Setenv("CACHE_TAG_VERSION_CACHE_TTL", "-1s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for negative cache tag version cache ttl")
	}
}

func TestLintRejectsMiddlewareTimeoutExceedingWriteTimeout(t *testing.T) {
	t.Setenv("HTTP_WRITE_TIMEOUT", "100ms")
	t.Setenv("HTTP_MIDDLEWARE_REQUEST_TIMEOUT", "200ms")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for middleware timeout > write timeout")
	}
}

func TestTracingDefaultsToDisabled(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Tracing.Enabled {
		t.Fatalf("expected tracing to be disabled by default")
	}
}

func TestDefaultsSetGlobalMaxBodyLimit(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.HTTP.Middleware.MaxBodyBytes, int64(1<<20); got != want {
		t.Fatalf("MaxBodyBytes=%d want=%d", got, want)
	}
}

func TestSecurityHeadersDefaultByEnv(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	cfgProd, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfgProd.HTTP.Middleware.SecurityHeadersEnabled {
		t.Fatalf("expected security headers enabled by default in prod")
	}

	t.Setenv("APP_ENV", "dev")
	cfgDev, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfgDev.HTTP.Middleware.SecurityHeadersEnabled {
		t.Fatalf("expected security headers disabled by default in dev")
	}
}

func TestTracingInsecureDefaultByEnv(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	cfgProd, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfgProd.Tracing.Insecure {
		t.Fatalf("expected tracing insecure=false by default in prod")
	}

	t.Setenv("APP_ENV", "dev")
	cfgDev, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfgDev.Tracing.Insecure {
		t.Fatalf("expected tracing insecure=true by default in dev")
	}
}

func TestLintRejectsMetricsWithoutAuthTokenInProd(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("METRICS_ENABLED", "true")
	t.Setenv("METRICS_AUTH_TOKEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for missing metrics auth token in prod")
	}
}

func TestLoadUsesFailClosedDefaultsInProd(t *testing.T) {
	t.Setenv("APP_ENV", "prod")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.RateLimit.FailOpen {
		t.Fatalf("expected ratelimit fail-open disabled by default in prod")
	}
	if cfg.Cache.FailOpen {
		t.Fatalf("expected cache fail-open disabled by default in prod")
	}
}

func TestLintRejectsRateLimitFailOpenInProd(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("RATELIMIT_ENABLED", "true")
	t.Setenv("RATELIMIT_FAIL_OPEN", "true")
	t.Setenv("REDIS_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for prod ratelimit fail-open")
	}
}

func TestLintRejectsCacheFailOpenInProd(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("CACHE_ENABLED", "true")
	t.Setenv("CACHE_FAIL_OPEN", "true")
	t.Setenv("REDIS_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for prod cache fail-open")
	}
}

func TestLintAllowsMetricsTokenInProd(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("METRICS_ENABLED", "true")
	t.Setenv("METRICS_AUTH_TOKEN", "super-secret-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err != nil {
		t.Fatalf("expected lint success, got: %v", err)
	}
}

func TestLintRejectsInvalidTracingSamplerWhenEnabled(t *testing.T) {
	t.Setenv("TRACING_ENABLED", "true")
	t.Setenv("TRACING_SAMPLER", "invalid")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for invalid tracing sampler")
	}
}

func TestLintRejectsInvalidTracingSampleRatioWhenEnabled(t *testing.T) {
	t.Setenv("TRACING_ENABLED", "true")
	t.Setenv("TRACING_SAMPLE_RATIO", "1.5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for invalid tracing sample ratio")
	}
}

func TestLintRejectsInvalidAuthMode(t *testing.T) {
	t.Setenv("AUTH_MODE", "invalid")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for invalid auth mode")
	}
}

func TestLintRejectsAuthEnabledWithoutRedis(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("REDIS_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for auth enabled without redis")
	}
}

func TestLintRejectsAuthEnabledWithoutPostgres(t *testing.T) {
	t.Setenv("AUTH_ENABLED", "true")
	t.Setenv("REDIS_ENABLED", "true")
	t.Setenv("POSTGRES_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for auth enabled without postgres")
	}
}

func TestLintRejectsRateLimitEnabledWithoutRedis(t *testing.T) {
	t.Setenv("RATELIMIT_ENABLED", "true")
	t.Setenv("REDIS_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for ratelimit enabled without redis")
	}
}

func TestLintRejectsCacheEnabledWithoutRedis(t *testing.T) {
	t.Setenv("CACHE_ENABLED", "true")
	t.Setenv("REDIS_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for cache enabled without redis")
	}
}

func TestLintRejectsInvalidCacheDefaultMaxBytes(t *testing.T) {
	t.Setenv("CACHE_DEFAULT_MAX_BYTES", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for invalid cache default max bytes")
	}
}

func TestLintRejectsInvalidRateLimitDefaults(t *testing.T) {
	t.Setenv("RATELIMIT_DEFAULT_LIMIT", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.Redis.Enabled = true

	if err := cfg.Lint(); err == nil {
		t.Fatalf("expected lint error for invalid ratelimit default limit")
	}
}

func TestLoadAppliesMinimalProfileDefaults(t *testing.T) {
	t.Setenv("APP_PROFILE", "minimal")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Auth.Enabled {
		t.Fatalf("expected auth disabled in minimal profile")
	}
	if cfg.Cache.Enabled {
		t.Fatalf("expected cache disabled in minimal profile")
	}
	if cfg.RateLimit.Enabled {
		t.Fatalf("expected ratelimit disabled in minimal profile")
	}
}

func TestLoadAppliesDevProfileDefaults(t *testing.T) {
	t.Setenv("APP_PROFILE", "dev")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Auth.Enabled {
		t.Fatalf("expected auth enabled in dev profile")
	}
	if cfg.Auth.Mode != "jwt_only" {
		t.Fatalf("auth mode=%q want=%q", cfg.Auth.Mode, "jwt_only")
	}
	if !cfg.Cache.Enabled {
		t.Fatalf("expected cache enabled in dev profile")
	}
	if !cfg.RateLimit.Enabled {
		t.Fatalf("expected ratelimit enabled in dev profile")
	}
}

func TestLoadEnvOverridesProfileDefaults(t *testing.T) {
	t.Setenv("APP_PROFILE", "dev")
	t.Setenv("AUTH_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Auth.Enabled {
		t.Fatalf("expected env override AUTH_ENABLED=false to win over profile")
	}
}

func TestLoadRejectsInvalidProfile(t *testing.T) {
	t.Setenv("APP_PROFILE", "unknown")

	if _, err := Load(); err == nil {
		t.Fatalf("expected load error for invalid profile")
	}
}
