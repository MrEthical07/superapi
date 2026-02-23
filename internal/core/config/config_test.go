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
