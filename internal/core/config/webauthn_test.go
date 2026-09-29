package config

import (
	"strings"
	"testing"
)

func TestWebAuthnRPIDLint(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "disabled ignores a bad id", env: map[string]string{"WEBAUTHN_RP_ID": "127.0.0.1"}},
		{name: "localhost", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "localhost"}},
		{name: "domain", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "auth.example.com"}},
		{name: "missing", env: map[string]string{"WEBAUTHN_ENABLED": "true"}, wantErr: "WEBAUTHN_RP_ID is required"},
		{name: "ipv4", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "127.0.0.1"}, wantErr: "WEBAUTHN_RP_ID"},
		{name: "ipv6", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "::1"}, wantErr: "WEBAUTHN_RP_ID"},
		{name: "with scheme", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "https://example.com"}, wantErr: "WEBAUTHN_RP_ID"},
		{name: "with port", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "example.com:8080"}, wantErr: "WEBAUTHN_RP_ID"},
		{name: "single label", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "myhost"}, wantErr: "single-label"},
		{name: "numeric tld", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "example.123"}, wantErr: "WEBAUTHN_RP_ID"},
		{name: "empty label", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "example..com"}, wantErr: "empty label"},
		{name: "hyphen label", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "-example.com"}, wantErr: "hyphen"},
		{name: "non ascii", env: map[string]string{"WEBAUTHN_ENABLED": "true", "WEBAUTHN_RP_ID": "exämple.com"}, wantErr: "non-ASCII"},
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
