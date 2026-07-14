package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"

	goauth "github.com/MrEthical07/goAuth"
)

func TestEnvDuration(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv("AUTH_TEST_DUR", "")
		d, ok, err := envDuration("AUTH_TEST_DUR")
		if err != nil || ok || d != 0 {
			t.Fatalf("unset: d=%v ok=%v err=%v", d, ok, err)
		}
	})
	t.Run("valid", func(t *testing.T) {
		t.Setenv("AUTH_TEST_DUR", "30m")
		d, ok, err := envDuration("AUTH_TEST_DUR")
		if err != nil || !ok || d.Minutes() != 30 {
			t.Fatalf("valid: d=%v ok=%v err=%v", d, ok, err)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		t.Setenv("AUTH_TEST_DUR", "not-a-duration")
		if _, _, err := envDuration("AUTH_TEST_DUR"); err == nil {
			t.Fatal("expected error for malformed duration")
		}
	})
	t.Run("non-positive", func(t *testing.T) {
		t.Setenv("AUTH_TEST_DUR", "0s")
		if _, _, err := envDuration("AUTH_TEST_DUR"); err == nil {
			t.Fatal("expected error for non-positive duration")
		}
	})
}

func ed25519PublicPEM(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func TestApplyKeyRotation(t *testing.T) {
	t.Run("neither set is a no-op", func(t *testing.T) {
		t.Setenv("AUTH_VERIFY_KEYS", "")
		t.Setenv("AUTH_KEY_ID", "")
		cfg := goauth.DefaultConfig()
		if err := applyKeyRotation(&cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.JWT.VerifyKeys) != 0 {
			t.Fatal("expected no verify keys")
		}
	})

	t.Run("verify keys without key id fails", func(t *testing.T) {
		t.Setenv("AUTH_VERIFY_KEYS", "v1="+ed25519PublicPEM(t))
		t.Setenv("AUTH_KEY_ID", "")
		cfg := goauth.DefaultConfig()
		if err := applyKeyRotation(&cfg); err == nil {
			t.Fatal("expected error: verify keys require key id")
		}
	})

	t.Run("key id absent from map fails", func(t *testing.T) {
		t.Setenv("AUTH_VERIFY_KEYS", "v1="+ed25519PublicPEM(t))
		t.Setenv("AUTH_KEY_ID", "v2")
		cfg := goauth.DefaultConfig()
		if err := applyKeyRotation(&cfg); err == nil {
			t.Fatal("expected error: key id must be present in verify keys")
		}
	})

	t.Run("valid pair wires verify keys and key id", func(t *testing.T) {
		pemText := ed25519PublicPEM(t)
		t.Setenv("AUTH_VERIFY_KEYS", "v1="+pemText)
		t.Setenv("AUTH_KEY_ID", "v1")
		cfg := goauth.DefaultConfig()
		if err := applyKeyRotation(&cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.JWT.KeyID != "v1" {
			t.Fatalf("key id=%q want v1", cfg.JWT.KeyID)
		}
		if key, ok := cfg.JWT.VerifyKeys["v1"]; !ok || len(key) != ed25519.PublicKeySize {
			t.Fatalf("verify key v1 missing or wrong size: ok=%v len=%d", ok, len(key))
		}
	})

	t.Run("malformed entry fails", func(t *testing.T) {
		t.Setenv("AUTH_VERIFY_KEYS", "not-a-kv-entry")
		t.Setenv("AUTH_KEY_ID", "v1")
		cfg := goauth.DefaultConfig()
		if err := applyKeyRotation(&cfg); err == nil {
			t.Fatal("expected error for malformed verify-keys entry")
		}
	})
}
