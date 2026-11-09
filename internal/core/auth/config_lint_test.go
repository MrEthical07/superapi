package auth

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	goauth "github.com/MrEthical07/goAuth"
)

// lintRecord is one captured "goauth config lint" log line.
type lintRecord struct {
	Level    string `json:"level"`
	Msg      string `json:"msg"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
}

func captureLint(t *testing.T, run func(logger *slog.Logger)) []lintRecord {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	run(logger)

	var out []lintRecord
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec lintRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if rec.Msg == "goauth config lint" {
			out = append(out, rec)
		}
	}
	return out
}

func TestLogLintResultsUsesSeverityLevels(t *testing.T) {
	results := goauth.LintResult{
		{Code: "info_code", Severity: goauth.LintInfo, Message: "note"},
		{Code: "warn_code", Severity: goauth.LintWarn, Message: "risky"},
		{Code: "high_code", Severity: goauth.LintHigh, Message: "dangerous"},
	}
	got := captureLint(t, func(l *slog.Logger) { logLintResults(l, results) })

	want := map[string]string{"info_code": "INFO", "warn_code": "WARN", "high_code": "ERROR"}
	if len(got) != len(want) {
		t.Fatalf("logged %d lint lines, want %d: %+v", len(got), len(want), got)
	}
	for _, rec := range got {
		if rec.Level != want[rec.Code] {
			t.Fatalf("lint %q logged at %s, want %s", rec.Code, rec.Level, want[rec.Code])
		}
	}
}

// knownDefaultLintWarnings are the Warn-level lint codes a default configuration
// is known to raise, with the reason. They are logged, not suppressed.
var knownDefaultLintWarnings = map[string]string{
	// SuperAPI wires no goAuth audit sink: audit events must never reach end
	// users or admins (AGENTS.md), so the template leaves Audit.Enabled off and
	// goAuth warns about it on every start.
	"audit_disabled": "Audit.Enabled is false: the template ships no audit sink",
}

// A default configuration must raise no Warn-level lint lines beyond the ones
// listed in knownDefaultLintWarnings. The INFO notes goAuth v0.8.1 added
// (login_ip_limiter_disabled, refresh_user_recheck_off) are expected and must
// not be promoted to warnings.
func TestProjectGoAuthConfigDefaultLintLevels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		features Features
	}{
		{"defaults", Features{}},
		{"all optional features", Features{
			RegistrationAutoLogin:     true,
			PasswordReset:             true,
			EmailVerification:         true,
			EmailVerificationRequired: true,
			TOTP:                      true,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var records []lintRecord
			prev := slog.Default()
			t.Cleanup(func() { slog.SetDefault(prev) })

			var buf bytes.Buffer
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
			if _, err := ProjectGoAuthConfig(ModeHybrid, tc.features); err != nil {
				t.Fatalf("ProjectGoAuthConfig: %v", err)
			}
			for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
				if line == "" {
					continue
				}
				var rec lintRecord
				if err := json.Unmarshal([]byte(line), &rec); err != nil {
					t.Fatalf("decode log line %q: %v", line, err)
				}
				if rec.Msg == "goauth config lint" {
					records = append(records, rec)
				}
			}

			codes := map[string]string{}
			for _, rec := range records {
				codes[rec.Code] = rec.Level
				if _, known := knownDefaultLintWarnings[rec.Code]; known {
					if rec.Level != "WARN" {
						t.Errorf("known warning %q logged at %s, want WARN", rec.Code, rec.Level)
					}
					continue
				}
				if rec.Level != "INFO" {
					t.Errorf("lint %q logged at %s (severity %s); a default config may log only INFO or a known warning", rec.Code, rec.Level, rec.Severity)
				}
			}
			for _, code := range []string{"login_ip_limiter_disabled", "refresh_user_recheck_off"} {
				if codes[code] != "INFO" {
					t.Errorf("expected INFO note %q on a default config, got %q (all: %v)", code, codes[code], codes)
				}
			}
		})
	}
}
