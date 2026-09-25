package config

import (
	"strings"
	"testing"
)

const validKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // 32 bytes

func TestAuthFeatureLint(t *testing.T) {
	base := map[string]string{"AUTH_ENABLED": "true", "REDIS_ENABLED": "true", "POSTGRES_ENABLED": "true", "POSTGRES_URL": "postgres://x"}
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "defaults off", env: map[string]string{}},
		{name: "feature needs auth", env: map[string]string{"AUTH_REGISTRATION_ENABLED": "true"}, wantErr: "requires AUTH_ENABLED"},
		{name: "registration ok", env: merge(base, map[string]string{"AUTH_REGISTRATION_ENABLED": "true"})},
		{name: "auto-login needs registration", env: merge(base, map[string]string{"AUTH_REGISTRATION_AUTO_LOGIN": "true"}), wantErr: "AUTH_REGISTRATION_AUTO_LOGIN"},
		{name: "totp needs key", env: merge(base, map[string]string{"AUTH_TOTP_ENABLED": "true"}), wantErr: "AUTH_TOTP_ENCRYPTION_KEY"},
		{name: "totp bad key", env: merge(base, map[string]string{"AUTH_TOTP_ENABLED": "true", "AUTH_TOTP_ENCRYPTION_KEY": "c2hvcnQ="}), wantErr: "32 bytes"},
		{name: "totp ok", env: merge(base, map[string]string{"AUTH_TOTP_ENABLED": "true", "AUTH_TOTP_ENCRYPTION_KEY": validKey})},
		{name: "notify bad driver", env: map[string]string{"NOTIFY_DRIVER": "smtp"}, wantErr: "notify driver"},
		{name: "notify secrets outside dev", env: map[string]string{"APP_ENV": "staging", "NOTIFY_DRIVER": "log", "NOTIFY_LOG_SECRETS": "true"}, wantErr: "NOTIFY_LOG_SECRETS"},
		{name: "notify secrets in dev", env: map[string]string{"APP_ENV": "dev", "NOTIFY_DRIVER": "log", "NOTIFY_LOG_SECRETS": "true"}},
		{name: "auth test override in dev", env: map[string]string{"APP_ENV": "dev", "AUTH_TEST_SHARED_SECRET": "x"}},
		{name: "auth test override in test", env: map[string]string{"APP_ENV": "test", "AUTH_TEST_ACCESS_TTL": "30s"}},
		{name: "auth test override in staging", env: map[string]string{"APP_ENV": "staging", "AUTH_TEST_SHARED_SECRET": "x"}, wantErr: "AUTH_TEST_SHARED_SECRET"},
		{name: "auth test override in prod", env: map[string]string{"APP_ENV": "prod", "AUTH_TEST_REFRESH_TTL": "5m"}, wantErr: "AUTH_TEST_REFRESH_TTL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			err = cfg.Lint()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected lint error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestAuthFeatureDefaults(t *testing.T) {
	t.Setenv("APP_SERVICE_NAME", "acme-api")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	a := cfg.Auth
	if a.RegistrationEnabled || a.RegistrationAutoLogin || a.PasswordResetEnabled || a.EmailVerificationEnabled || a.TOTPEnabled {
		t.Fatalf("every optional auth feature must default off: %+v", a)
	}
	if !a.EmailVerificationRequired {
		t.Fatal("verification should block login by default once enabled")
	}
	if a.TOTPIssuer != "acme-api" {
		t.Fatalf("totp issuer=%q want service name", a.TOTPIssuer)
	}
	if cfg.Notify.Driver != NotifyDriverNoop {
		t.Fatalf("notify driver=%q want noop", cfg.Notify.Driver)
	}
}

func TestDecodeKey32(t *testing.T) {
	if _, err := DecodeKey32(validKey); err != nil {
		t.Fatalf("std: %v", err)
	}
	if _, err := DecodeKey32(strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(validKey), "=")); err != nil {
		t.Fatalf("raw url: %v", err)
	}
	if _, err := DecodeKey32("!!!"); err == nil {
		t.Fatal("expected error for non-base64")
	}
}

func merge(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
