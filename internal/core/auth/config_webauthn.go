package auth

import (
	"os"
	"strings"

	goauth "github.com/MrEthical07/goAuth"
)

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
