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
