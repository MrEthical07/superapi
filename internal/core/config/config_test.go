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
