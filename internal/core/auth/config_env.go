package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	goauth "github.com/MrEthical07/goAuth"
)

// envDuration reads an optional duration env var. It returns (0,false,nil) when
// unset/blank, (d,true,nil) for a valid positive duration, and an error for a
// malformed or non-positive value so misconfiguration fails fast at startup.
func envDuration(name string) (time.Duration, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%s must be a valid Go duration: %w", name, err)
	}
	if d <= 0 {
		return 0, false, fmt.Errorf("%s must be > 0", name)
	}
	return d, true, nil
}

// envBool reads an optional boolean env var, returning def when unset/blank.
func envBool(name string, def bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	switch strings.ToLower(raw) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	case "0", "f", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

// envCSV reads an optional comma-separated env var into a trimmed, non-empty
// slice. Returns nil when unset/blank.
func envCSV(name string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// applyKeyRotation wires Ed25519 verification keys for zero-downtime rotation.
//
// Env:
//   - AUTH_KEY_ID: the active signing key id (kid). Required whenever any
//     verify key is supplied.
//   - AUTH_VERIFY_KEYS: a comma-separated list of "kid=<pem>" entries mapping a
//     kid to its Ed25519 public key in PEM form. The PEM may contain literal
//     newlines or the two-character escape "\n" (for single-line env values).
//
// goAuth couples VerifyKeys and KeyID: with VerifyKeys set, KeyID must be
// non-empty and present in the map, else Build fails. This function enforces
// the same invariant early with a clearer message, and treats "neither set" as
// a no-op (goAuth generates a dev keypair).
func applyKeyRotation(cfg *goauth.Config) error {
	entries := envCSV("AUTH_VERIFY_KEYS")
	keyID := strings.TrimSpace(os.Getenv("AUTH_KEY_ID"))

	if len(entries) == 0 {
		if keyID != "" {
			// KeyID alone (without verify keys) is a valid signing-kid override
			// and is left to goAuth; nothing to assemble here.
			cfg.JWT.KeyID = keyID
		}
		return nil
	}

	if keyID == "" {
		return fmt.Errorf("AUTH_VERIFY_KEYS requires AUTH_KEY_ID (the active signing kid)")
	}

	verifyKeys := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		kid, pemText, ok := strings.Cut(entry, "=")
		kid = strings.TrimSpace(kid)
		if !ok || kid == "" {
			return fmt.Errorf("AUTH_VERIFY_KEYS entry must be \"kid=<pem>\": %q", entry)
		}
		pub, err := parseEd25519PublicKey(pemText)
		if err != nil {
			return fmt.Errorf("AUTH_VERIFY_KEYS kid %q: %w", kid, err)
		}
		verifyKeys[kid] = pub
	}

	if _, ok := verifyKeys[keyID]; !ok {
		return fmt.Errorf("AUTH_KEY_ID %q must be present in AUTH_VERIFY_KEYS", keyID)
	}

	cfg.JWT.KeyID = keyID
	cfg.JWT.VerifyKeys = verifyKeys
	return nil
}

// parseEd25519PublicKey decodes a PEM-encoded Ed25519 public key into the raw
// 32-byte key material goAuth's VerifyKeys map expects.
func parseEd25519PublicKey(pemText string) ([]byte, error) {
	normalized := strings.ReplaceAll(pemText, `\n`, "\n")
	block, _ := pem.Decode([]byte(strings.TrimSpace(normalized)))
	if block == nil {
		return nil, fmt.Errorf("invalid PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	edPub, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an Ed25519 public key")
	}
	return edPub, nil
}

// applyWebAuthnConfig populates goAuth's WebAuthn config from env. Enabled
// defaults to false; when off, the remaining fields are irrelevant (goAuth does
// not require the WebAuthn capability at Build). See docs/enabling-webauthn.md.
func applyWebAuthnConfig(cfg *goauth.Config) {
	cfg.WebAuthn.Enabled = envBool("WEBAUTHN_ENABLED", false)
	if !cfg.WebAuthn.Enabled {
		return
	}

	cfg.WebAuthn.RPID = strings.TrimSpace(os.Getenv("WEBAUTHN_RP_ID"))
	cfg.WebAuthn.RPDisplayName = strings.TrimSpace(os.Getenv("WEBAUTHN_RP_DISPLAY_NAME"))
	cfg.WebAuthn.RPOrigins = envCSV("WEBAUTHN_RP_ORIGINS")

	if v := strings.TrimSpace(os.Getenv("WEBAUTHN_ATTESTATION_PREFERENCE")); v != "" {
		cfg.WebAuthn.AttestationPreference = strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("WEBAUTHN_USER_VERIFICATION")); v != "" {
		cfg.WebAuthn.UserVerification = strings.ToLower(v)
	}
	if d, ok, err := envDuration("WEBAUTHN_CEREMONY_TTL"); err == nil && ok {
		cfg.WebAuthn.CeremonyTTL = d
	}
	cfg.WebAuthn.RequireForLogin = envBool("WEBAUTHN_REQUIRE_FOR_LOGIN", false)
	cfg.WebAuthn.RejectClonedAuthenticators = envBool("WEBAUTHN_REJECT_CLONED_AUTHENTICATORS", true)
}
