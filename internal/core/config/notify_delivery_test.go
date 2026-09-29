package config

import (
	"strings"
	"testing"
)

// deliveryCfg builds a config the way Load would from these settings, without
// depending on the rest of the process environment.
func deliveryCfg(env, driver string, reset, verify bool) *Config {
	cfg := &Config{Env: env}
	cfg.Notify.Driver = driver
	cfg.Auth.PasswordResetEnabled = reset
	cfg.Auth.EmailVerificationEnabled = verify
	return cfg
}

func TestLintNotifyDelivery(t *testing.T) {
	type combo struct {
		name          string
		reset, verify bool
	}
	features := []combo{
		{"nothing enabled", false, false},
		{"reset only", true, false},
		{"verification only", false, true},
		{"reset and verification", true, true},
	}
	envs := []struct {
		env        string
		relaxed    bool
		descriptor string
	}{
		{"dev", true, "dev"},
		{"test", true, "test"},
		{"development", true, "development"},
		{"local", true, "local"},
		{"DEV", true, "upper-case dev"},
		{"staging", false, "staging"},
		{"prod", false, "prod"},
		{"production", false, "production"},
		{"", false, "empty"},
	}
	drivers := []struct {
		name string
		noop bool
	}{
		{"noop", true},
		{"NOOP", true},
		{"", true}, // unset means noop
		{"log", false},
		{"smtp", false},
	}

	for _, f := range features {
		for _, e := range envs {
			for _, d := range drivers {
				name := f.name + "/env=" + e.descriptor + "/driver=" + d.name
				t.Run(name, func(t *testing.T) {
					err := deliveryCfg(e.env, d.name, f.reset, f.verify).lintNotifyDelivery()
					wantErr := (f.reset || f.verify) && d.noop && !e.relaxed
					if !wantErr {
						if err != nil {
							t.Fatalf("unexpected error: %v", err)
						}
						return
					}
					if err == nil {
						t.Fatal("expected startup to be refused")
					}
					msg := err.Error()
					if !strings.Contains(msg, "NOTIFY_DRIVER") || !strings.Contains(msg, "APP_ENV=dev or APP_ENV=test") {
						t.Fatalf("error should name NOTIFY_DRIVER and the dev/test exception: %v", err)
					}
					if f.reset && !strings.Contains(msg, "AUTH_PASSWORD_RESET_ENABLED") {
						t.Fatalf("error should name the reset flag: %v", err)
					}
					if f.verify && !strings.Contains(msg, "AUTH_EMAIL_VERIFICATION_ENABLED") {
						t.Fatalf("error should name the verification flag: %v", err)
					}
					if !f.reset && strings.Contains(msg, "AUTH_PASSWORD_RESET_ENABLED") {
						t.Fatalf("error names a flag that is off: %v", err)
					}
					if !f.verify && strings.Contains(msg, "AUTH_EMAIL_VERIFICATION_ENABLED") {
						t.Fatalf("error names a flag that is off: %v", err)
					}
				})
			}
		}
	}
}

func TestNotifyDeliveryWarnings(t *testing.T) {
	cases := []struct {
		name        string
		cfg         *Config
		wantWarning bool
	}{
		{"noop + reset in dev", deliveryCfg("dev", "noop", true, false), true},
		{"noop + verification in test", deliveryCfg("test", "noop", false, true), true},
		{"noop + both in dev", deliveryCfg("dev", "", true, true), true},
		{"noop, features off", deliveryCfg("dev", "noop", false, false), false},
		{"log driver + reset", deliveryCfg("dev", "log", true, false), false},
		{"smtp + verification", deliveryCfg("dev", "smtp", false, true), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.Warnings()
			if tc.wantWarning != (len(got) > 0) {
				t.Fatalf("warnings=%v, wantWarning=%v", got, tc.wantWarning)
			}
			if tc.wantWarning && (!strings.Contains(got[0], "NOTIFY_DRIVER=noop") || !strings.Contains(got[0], "NOTIFY_DRIVER=log")) {
				t.Fatalf("warning should explain the problem and the fix: %q", got[0])
			}
		})
	}
}

// End to end through Load and Lint, the way the process starts.
func TestLintRefusesResetWithNoopNotifierOutsideDev(t *testing.T) {
	base := map[string]string{
		"AUTH_ENABLED":     "true",
		"REDIS_ENABLED":    "true",
		"POSTGRES_ENABLED": "true",
		"POSTGRES_URL":     "postgres://x",
	}
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"staging reset noop", merge(base, map[string]string{"APP_ENV": "staging", "AUTH_PASSWORD_RESET_ENABLED": "true"}), true},
		{"staging verification noop", merge(base, map[string]string{"APP_ENV": "staging", "AUTH_EMAIL_VERIFICATION_ENABLED": "true"}), true},
		{"staging reset with log driver", merge(base, map[string]string{"APP_ENV": "staging", "AUTH_PASSWORD_RESET_ENABLED": "true", "NOTIFY_DRIVER": "log"}), false},
		{"staging no features", merge(base, map[string]string{"APP_ENV": "staging"}), false},
		{"dev reset noop", merge(base, map[string]string{"APP_ENV": "dev", "AUTH_PASSWORD_RESET_ENABLED": "true"}), false},
		{"test verification noop", merge(base, map[string]string{"APP_ENV": "test", "AUTH_EMAIL_VERIFICATION_ENABLED": "true"}), false},
		{"unset APP_ENV defaults to dev", merge(base, map[string]string{"AUTH_PASSWORD_RESET_ENABLED": "true"}), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			err = cfg.Lint()
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected lint error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "NOTIFY_DRIVER") {
				t.Fatalf("err=%v, want a NOTIFY_DRIVER error", err)
			}
		})
	}
}
