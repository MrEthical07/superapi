package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	maxRPIDLength      = 253
	maxRPIDLabelLength = 63
	// forbiddenRPIDCodePoints are characters that mark a value as a URL,
	// host:port or IPv6 literal rather than a bare domain.
	forbiddenRPIDCodePoints = " #/:<>?@[\\]^|%"
)

// lintWebAuthn checks WEBAUTHN_RP_ID when WEBAUTHN_ENABLED=true, so a value
// goAuth would reject at Build() fails at config load with the env var's name
// instead. WEBAUTHN_* are read by internal/core/auth; only the RP ID is
// validated here because it is the one goAuth v0.6.0 made strict.
func (c *Config) lintWebAuthn() error {
	// Same accepted spellings as internal/core/auth's envBool.
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WEBAUTHN_ENABLED"))) {
	case "1", "t", "true", "yes", "y", "on":
	default:
		return nil
	}
	rpID := strings.TrimSpace(os.Getenv("WEBAUTHN_RP_ID"))
	if rpID == "" {
		return errors.New(`WEBAUTHN_RP_ID is required when WEBAUTHN_ENABLED=true (use "localhost" for local development)`)
	}
	if err := ValidateRPID(rpID); err != nil {
		return fmt.Errorf(`WEBAUTHN_RP_ID %q is not a valid relying party ID: %w (use a bare domain such as "localhost" or "example.com": no scheme, port or IP address)`, rpID, err)
	}
	return nil
}

// ValidateRPID reports why value is not usable as a WebAuthn relying party ID.
//
// It mirrors the domain rules goAuth v0.6.0 applies at Build() (through
// go-webauthn's protocol.ValidateRPID): a bare ASCII domain, no IP address, no
// scheme, port or path, no empty or hyphen-bounded labels, and a single-label
// name only for "localhost". A test in internal/core/auth checks this stays in
// step with goAuth's own decision.
func ValidateRPID(value string) error {
	if value == "" {
		return errors.New("empty value")
	}
	if net.ParseIP(value) != nil {
		return errors.New("an IP address is not a domain")
	}
	if len(value) > maxRPIDLength {
		return fmt.Errorf("longer than %d characters", maxRPIDLength)
	}

	labels := strings.Split(value, ".")
	for _, label := range labels {
		switch {
		case label == "":
			return errors.New("contains an empty label")
		case len(label) > maxRPIDLabelLength:
			return fmt.Errorf("a label is longer than %d characters", maxRPIDLabelLength)
		case label[0] == '-' || label[len(label)-1] == '-':
			return errors.New("a label begins or ends with a hyphen")
		case strings.ContainsAny(label, forbiddenRPIDCodePoints):
			return errors.New("contains a forbidden character (scheme, port, path and similar are not allowed)")
		}
		for _, r := range label {
			if r < 0x20 || r == 0x7F {
				return errors.New("contains a control character")
			}
			if r > 0x7F {
				return errors.New("contains a non-ASCII character (apply IDNA/punycode first)")
			}
		}
	}

	if isNumericLabel(labels[len(labels)-1]) {
		return errors.New("the last label must not be a number")
	}
	if len(labels) == 1 && value != "localhost" {
		return errors.New(`a single-label name is not a domain (only "localhost" is allowed)`)
	}
	return nil
}

func isNumericLabel(label string) bool {
	if label == "" {
		return false
	}
	if strings.Trim(label, "0123456789") == "" {
		return true
	}
	_, err := strconv.ParseUint(label, 0, 64)
	return err == nil
}
