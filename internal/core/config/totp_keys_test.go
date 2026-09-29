package config

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func b64(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

func TestTOTPKeyring(t *testing.T) {
	cases := []struct {
		name       string
		cfg        AuthConfig
		wantErr    string
		wantActive string
		wantKIDs   []string
		wantLegacy bool
	}{
		{
			name:       "single key (the v0.9.0 setup)",
			cfg:        AuthConfig{TOTPEncryptionKey: b64(1)},
			wantActive: "default", wantKIDs: []string{"default"}, wantLegacy: true,
		},
		{
			name:       "keyring with an active key",
			cfg:        AuthConfig{TOTPEncryptionKeys: "k1:" + b64(1) + ",k2:" + b64(2), TOTPEncryptionActiveKID: "k2"},
			wantActive: "k2", wantKIDs: []string{"k1", "k2"},
		},
		{
			name:       "rotation in progress: keyring plus the old single key",
			cfg:        AuthConfig{TOTPEncryptionKey: b64(1), TOTPEncryptionKeys: "2026-09:" + b64(2), TOTPEncryptionActiveKID: "2026-09"},
			wantActive: "2026-09", wantKIDs: []string{"2026-09", "default"}, wantLegacy: true,
		},
		{
			name:       "the single key can stay active while a new key is staged",
			cfg:        AuthConfig{TOTPEncryptionKey: b64(1), TOTPEncryptionKeys: "next:" + b64(2), TOTPEncryptionActiveKID: "default"},
			wantActive: "default", wantKIDs: []string{"default", "next"}, wantLegacy: true,
		},
		{
			name:       "single key with the implicit active id spelled out",
			cfg:        AuthConfig{TOTPEncryptionKey: b64(1), TOTPEncryptionActiveKID: "default"},
			wantActive: "default", wantKIDs: []string{"default"}, wantLegacy: true,
		},
		{
			name:       "whitespace and url-safe base64 are tolerated",
			cfg:        AuthConfig{TOTPEncryptionKeys: " a : " + strings.NewReplacer("+", "-", "/", "_").Replace(b64(255)) + " , b:" + b64(2) + " ", TOTPEncryptionActiveKID: " a "},
			wantActive: "a", wantKIDs: []string{"a", "b"},
		},

		{name: "nothing configured", cfg: AuthConfig{}, wantErr: "AUTH_TOTP_ENCRYPTION_KEY"},
		{name: "bad single key", cfg: AuthConfig{TOTPEncryptionKey: "c2hvcnQ="}, wantErr: "AUTH_TOTP_ENCRYPTION_KEY: must decode to 32 bytes"},
		{name: "single key not base64", cfg: AuthConfig{TOTPEncryptionKey: "!!!"}, wantErr: "base64"},
		{name: "keyring without an active id", cfg: AuthConfig{TOTPEncryptionKeys: "k1:" + b64(1)}, wantErr: "AUTH_TOTP_ENCRYPTION_ACTIVE_KID"},
		{name: "active id not in the ring", cfg: AuthConfig{TOTPEncryptionKeys: "k1:" + b64(1), TOTPEncryptionActiveKID: "k9"}, wantErr: `"k9" is not one of the configured keys (k1)`},
		{name: "active id without a keyring", cfg: AuthConfig{TOTPEncryptionKey: b64(1), TOTPEncryptionActiveKID: "k1"}, wantErr: "AUTH_TOTP_ENCRYPTION_KEYS is not set"},
		{name: "key of the wrong length", cfg: AuthConfig{TOTPEncryptionKeys: "k1:c2hvcnQ=", TOTPEncryptionActiveKID: "k1"}, wantErr: `key "k1": must decode to 32 bytes`},
		{name: "key not base64", cfg: AuthConfig{TOTPEncryptionKeys: "k1:%%%", TOTPEncryptionActiveKID: "k1"}, wantErr: `key "k1"`},
		{name: "duplicate ids", cfg: AuthConfig{TOTPEncryptionKeys: "k1:" + b64(1) + ",k1:" + b64(2), TOTPEncryptionActiveKID: "k1"}, wantErr: `"k1" appears more than once`},
		{name: "id clashes with the single key", cfg: AuthConfig{TOTPEncryptionKey: b64(1), TOTPEncryptionKeys: "default:" + b64(2), TOTPEncryptionActiveKID: "default"}, wantErr: `"default" is already taken by AUTH_TOTP_ENCRYPTION_KEY`},
		{name: "invalid id characters", cfg: AuthConfig{TOTPEncryptionKeys: "k 1:" + b64(1), TOTPEncryptionActiveKID: "k 1"}, wantErr: `invalid key id "k 1"`},
		{name: "id too long", cfg: AuthConfig{TOTPEncryptionKeys: strings.Repeat("k", 33) + ":" + b64(1), TOTPEncryptionActiveKID: strings.Repeat("k", 33)}, wantErr: "invalid key id"},
		{name: "entry without a colon", cfg: AuthConfig{TOTPEncryptionKeys: "k1" + b64(1), TOTPEncryptionActiveKID: "k1"}, wantErr: "must be kid:base64key"},
		{name: "entry without a key", cfg: AuthConfig{TOTPEncryptionKeys: "k1:", TOTPEncryptionActiveKID: "k1"}, wantErr: "must be kid:base64key"},
		{name: "entry without an id", cfg: AuthConfig{TOTPEncryptionKeys: ":" + b64(1), TOTPEncryptionActiveKID: "k1"}, wantErr: "must be kid:base64key"},
		{name: "stray comma", cfg: AuthConfig{TOTPEncryptionKeys: "k1:" + b64(1) + ",", TOTPEncryptionActiveKID: "k1"}, wantErr: "entry 2 is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ring, err := tc.cfg.TOTPKeyring()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ring.ActiveKID != tc.wantActive {
				t.Fatalf("active = %q, want %q", ring.ActiveKID, tc.wantActive)
			}
			if got := sortedKeyIDs(ring.Keys); strings.Join(got, ",") != strings.Join(tc.wantKIDs, ",") {
				t.Fatalf("key ids = %v, want %v", got, tc.wantKIDs)
			}
			if (ring.LegacyKey != nil) != tc.wantLegacy {
				t.Fatalf("legacy key present = %v, want %v", ring.LegacyKey != nil, tc.wantLegacy)
			}
			for kid, k := range ring.Keys {
				if len(k) != 32 {
					t.Fatalf("key %q has %d bytes", kid, len(k))
				}
			}
		})
	}
}

// The error text never contains the key material itself.
func TestTOTPKeyringErrorsDoNotLeakKeys(t *testing.T) {
	secret := b64(42)
	cfg := AuthConfig{TOTPEncryptionKeys: "k1:" + secret + ",k1:" + secret, TOTPEncryptionActiveKID: "nope"}
	_, err := cfg.TOTPKeyring()
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks key material: %v", err)
	}
}

// Through Load and Lint, with the env vars an operator would set.
func TestTOTPKeyringLint(t *testing.T) {
	base := map[string]string{"AUTH_ENABLED": "true", "REDIS_ENABLED": "true", "POSTGRES_ENABLED": "true", "POSTGRES_URL": "postgres://x", "AUTH_TOTP_ENABLED": "true"}
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"single key", merge(base, map[string]string{"AUTH_TOTP_ENCRYPTION_KEY": b64(1)}), ""},
		{"keyring", merge(base, map[string]string{"AUTH_TOTP_ENCRYPTION_KEYS": "k1:" + b64(1) + ",k2:" + b64(2), "AUTH_TOTP_ENCRYPTION_ACTIVE_KID": "k2"}), ""},
		{"rotation state", merge(base, map[string]string{"AUTH_TOTP_ENCRYPTION_KEY": b64(1), "AUTH_TOTP_ENCRYPTION_KEYS": "k2:" + b64(2), "AUTH_TOTP_ENCRYPTION_ACTIVE_KID": "k2"}), ""},
		{"no key at all", base, "AUTH_TOTP_ENCRYPTION_KEY"},
		{"active kid missing", merge(base, map[string]string{"AUTH_TOTP_ENCRYPTION_KEYS": "k1:" + b64(1)}), "AUTH_TOTP_ENCRYPTION_ACTIVE_KID"},
		{"active kid unknown", merge(base, map[string]string{"AUTH_TOTP_ENCRYPTION_KEYS": "k1:" + b64(1), "AUTH_TOTP_ENCRYPTION_ACTIVE_KID": "zz"}), `"zz"`},
		{"short key in the ring", merge(base, map[string]string{"AUTH_TOTP_ENCRYPTION_KEYS": "k1:c2hvcnQ=", "AUTH_TOTP_ENCRYPTION_ACTIVE_KID": "k1"}), "32 bytes"},
		{"duplicate ids", merge(base, map[string]string{"AUTH_TOTP_ENCRYPTION_KEYS": "k1:" + b64(1) + ",k1:" + b64(2), "AUTH_TOTP_ENCRYPTION_ACTIVE_KID": "k1"}), "more than once"},
		// TOTP off: none of it is checked.
		{"totp disabled ignores everything", map[string]string{"AUTH_TOTP_ENCRYPTION_KEYS": "garbage"}, ""},
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
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
