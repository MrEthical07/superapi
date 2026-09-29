package config

import (
	"strings"
	"testing"
)

func smtpLintCfg(env string, mutate func(*Config)) *Config {
	cfg := &Config{Env: env}
	cfg.Notify.Driver = "smtp"
	cfg.Notify.SMTP = SMTPConfig{
		Host: "smtp.example.com",
		Port: 587,
		From: "SuperAPI <noreply@example.com>",
		TLS:  SMTPTLSStartTLS,
	}
	if mutate != nil {
		mutate(cfg)
	}
	return cfg
}

func TestLintSMTP(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "minimal valid", env: "prod"},
		{name: "not the smtp driver ignores every field", env: "prod", mutate: func(c *Config) {
			c.Notify.Driver = "log"
			c.Notify.SMTP = SMTPConfig{}
		}},
		{name: "missing host", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.Host = "" }, wantErr: "SMTP_HOST"},
		{name: "host with scheme", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.Host = "smtp://mail.example.com" }, wantErr: "SMTP_HOST"},
		{name: "host with port", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.Host = "mail.example.com:587" }, wantErr: "SMTP_HOST"},
		{name: "port too low", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.Port = 0 }, wantErr: "SMTP_PORT"},
		{name: "port too high", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.Port = 70000 }, wantErr: "SMTP_PORT"},
		{name: "missing from", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.From = "" }, wantErr: "SMTP_FROM"},
		{name: "invalid from", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.From = "not an address" }, wantErr: "SMTP_FROM"},
		{name: "from with line break", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.From = "a@example.com\r\nBcc: b@example.com" }, wantErr: "SMTP_FROM"},
		{name: "invalid tls mode", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.TLS = "ssl" }, wantErr: "SMTP_TLS"},
		{name: "implicit tls", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.TLS = SMTPTLSImplicit; c.Notify.SMTP.Port = 465 }},
		{name: "tls none in dev", env: "dev", mutate: func(c *Config) { c.Notify.SMTP.TLS = SMTPTLSNone }},
		{name: "tls none in test", env: "test", mutate: func(c *Config) { c.Notify.SMTP.TLS = SMTPTLSNone }, wantErr: "APP_ENV=dev"},
		{name: "tls none in staging", env: "staging", mutate: func(c *Config) { c.Notify.SMTP.TLS = SMTPTLSNone }, wantErr: "APP_ENV=dev"},
		{name: "tls none in prod", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.TLS = SMTPTLSNone }, wantErr: "APP_ENV=dev"},
		{name: "username without password", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.Username = "u" }, wantErr: "SMTP_USERNAME and SMTP_PASSWORD"},
		{name: "password without username", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.Password = "p" }, wantErr: "SMTP_USERNAME and SMTP_PASSWORD"},
		{name: "credentials with line break", env: "prod", mutate: func(c *Config) {
			c.Notify.SMTP.Username, c.Notify.SMTP.Password = "u\r\nRCPT", "p"
		}, wantErr: "line breaks"},
		{name: "credentials ok", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.Username, c.Notify.SMTP.Password = "u", "p" }},

		{name: "reset enabled needs reset url", env: "prod", mutate: func(c *Config) { c.Auth.PasswordResetEnabled = true }, wantErr: "NOTIFY_RESET_URL"},
		{name: "verification enabled needs verify url", env: "prod", mutate: func(c *Config) { c.Auth.EmailVerificationEnabled = true }, wantErr: "NOTIFY_VERIFY_URL"},
		{name: "reset url ok", env: "prod", mutate: func(c *Config) {
			c.Auth.PasswordResetEnabled = true
			c.Notify.SMTP.ResetURL = "https://app.example.com/reset?token={token}"
		}},
		{name: "both urls ok", env: "prod", mutate: func(c *Config) {
			c.Auth.PasswordResetEnabled, c.Auth.EmailVerificationEnabled = true, true
			c.Notify.SMTP.ResetURL = "https://app.example.com/reset?token={token}"
			c.Notify.SMTP.VerifyURL = "https://app.example.com/verify/{token}"
		}},
		{name: "reset url without token", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.ResetURL = "https://app.example.com/reset" }, wantErr: "NOTIFY_RESET_URL: must contain {token} exactly once"},
		{name: "verify url with two tokens", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.VerifyURL = "https://a.example.com/{token}/{token}" }, wantErr: "NOTIFY_VERIFY_URL"},
		{name: "reset url not absolute", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.ResetURL = "/reset?token={token}" }, wantErr: "NOTIFY_RESET_URL"},
		{name: "reset url with other scheme", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.ResetURL = "ftp://app.example.com/{token}" }, wantErr: "http(s)"},
		{name: "reset url with whitespace", env: "prod", mutate: func(c *Config) { c.Notify.SMTP.ResetURL = "https://app.example.com/a b?t={token}" }, wantErr: "whitespace"},
		{name: "reset url http localhost", env: "dev", mutate: func(c *Config) { c.Notify.SMTP.ResetURL = "http://localhost:3000/reset?token={token}" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := smtpLintCfg(tc.env, tc.mutate).lintSMTP()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

// Through Load and Lint, with the env vars an operator would set.
func TestLoadReadsSMTPEnvironment(t *testing.T) {
	for k, v := range map[string]string{
		"APP_ENV":                     "dev",
		"NOTIFY_DRIVER":               "SMTP",
		"SMTP_HOST":                   "smtp.example.com",
		"SMTP_FROM":                   "SuperAPI <noreply@example.com>",
		"SMTP_USERNAME":               "mailer",
		"SMTP_PASSWORD":               "pw",
		"NOTIFY_RESET_URL":            "https://app.example.com/reset?token={token}",
		"NOTIFY_VERIFY_URL":           "https://app.example.com/verify?token={token}",
		"AUTH_ENABLED":                "true",
		"REDIS_ENABLED":               "true",
		"POSTGRES_ENABLED":            "true",
		"POSTGRES_URL":                "postgres://x",
		"AUTH_PASSWORD_RESET_ENABLED": "true",
	} {
		t.Setenv(k, v)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := cfg.Notify.SMTP
	if cfg.Notify.Driver != "smtp" || s.Host != "smtp.example.com" || s.Port != 587 || s.TLS != SMTPTLSStartTLS || s.Username != "mailer" || s.From == "" {
		t.Fatalf("loaded %+v (driver %q)", s, cfg.Notify.Driver)
	}
	if err := cfg.Lint(); err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(cfg.Warnings()) != 0 {
		t.Fatalf("the smtp driver must not trigger the noop warning: %v", cfg.Warnings())
	}
}

func TestSMTPDefaultPortFollowsTLSMode(t *testing.T) {
	for mode, want := range map[string]int{SMTPTLSStartTLS: 587, SMTPTLSImplicit: 465, SMTPTLSNone: 25} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SMTP_TLS", mode)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Notify.SMTP.Port != want {
				t.Fatalf("port = %d, want %d", cfg.Notify.SMTP.Port, want)
			}
		})
	}
	t.Setenv("SMTP_TLS", SMTPTLSImplicit)
	t.Setenv("SMTP_PORT", "2465")
	cfg, _ := Load()
	if cfg.Notify.SMTP.Port != 2465 {
		t.Fatalf("explicit SMTP_PORT must win, got %d", cfg.Notify.SMTP.Port)
	}
}
