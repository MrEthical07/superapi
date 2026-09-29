package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// DefaultTOTPKeyID is the key id given to AUTH_TOTP_ENCRYPTION_KEY, the single
// key, when it is used alone or next to a keyring.
const DefaultTOTPKeyID = "default"

var totpKeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// ValidTOTPKeyID reports whether kid is an acceptable key id: 1-32 characters
// from [A-Za-z0-9._-]. (The auth package enforces the same rule when it builds
// the cipher; a test keeps them in step.)
func ValidTOTPKeyID(kid string) bool { return totpKeyIDPattern.MatchString(kid) }

// TOTPKeyring is the decoded TOTP encryption key set.
type TOTPKeyring struct {
	// Keys maps key id to a 32-byte key.
	Keys map[string][]byte
	// ActiveKID is the key new secrets are sealed with.
	ActiveKID string
	// LegacyKey opens secrets written by v0.9.0 (format v1, no key id). It is
	// AUTH_TOTP_ENCRYPTION_KEY when that is set, otherwise nil.
	LegacyKey []byte
}

// TOTPKeyring decodes and validates the TOTP encryption settings:
//
//   - AUTH_TOTP_ENCRYPTION_KEY alone is one key with the implicit id "default",
//     which is also the active key (the v0.9.0 setup, unchanged).
//   - AUTH_TOTP_ENCRYPTION_KEYS is a comma-separated list of kid:base64key and
//     needs AUTH_TOTP_ENCRYPTION_ACTIVE_KID to say which one seals new secrets.
//     When AUTH_TOTP_ENCRYPTION_KEY is set as well it stays readable as
//     "default" (and opens v0.9.0 rows), which is the state during a rotation.
//
// Key ids must be unique and valid, every key must decode to 32 bytes, and the
// active id must be one of the keys.
func (a AuthConfig) TOTPKeyring() (TOTPKeyring, error) {
	single := strings.TrimSpace(a.TOTPEncryptionKey)
	list := strings.TrimSpace(a.TOTPEncryptionKeys)
	active := strings.TrimSpace(a.TOTPEncryptionActiveKID)

	if single == "" && list == "" {
		return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENABLED requires AUTH_TOTP_ENCRYPTION_KEY (base64-encoded 32-byte key, e.g. `openssl rand -base64 32`) or AUTH_TOTP_ENCRYPTION_KEYS with AUTH_TOTP_ENCRYPTION_ACTIVE_KID")
	}

	ring := TOTPKeyring{Keys: map[string][]byte{}}
	if single != "" {
		key, err := DecodeKey32(single)
		if err != nil {
			return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_KEY: %w", err)
		}
		ring.Keys[DefaultTOTPKeyID] = key
		ring.LegacyKey = key
	}

	if list == "" {
		if active != "" && active != DefaultTOTPKeyID {
			return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_ACTIVE_KID=%q but AUTH_TOTP_ENCRYPTION_KEYS is not set; with only AUTH_TOTP_ENCRYPTION_KEY the active key is %q", active, DefaultTOTPKeyID)
		}
		ring.ActiveKID = DefaultTOTPKeyID
		return ring, nil
	}

	for i, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_KEYS entry %d is empty (a stray comma?)", i+1)
		}
		kid, encoded, ok := strings.Cut(entry, ":")
		kid, encoded = strings.TrimSpace(kid), strings.TrimSpace(encoded)
		if !ok || kid == "" || encoded == "" {
			return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_KEYS entry %d must be kid:base64key", i+1)
		}
		if !ValidTOTPKeyID(kid) {
			return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_KEYS: invalid key id %q (1-32 characters from [A-Za-z0-9._-])", kid)
		}
		if _, dup := ring.Keys[kid]; dup {
			if kid == DefaultTOTPKeyID && single != "" {
				return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_KEYS: key id %q is already taken by AUTH_TOTP_ENCRYPTION_KEY; use another id for the new key", kid)
			}
			return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_KEYS: key id %q appears more than once", kid)
		}
		key, err := DecodeKey32(encoded)
		if err != nil {
			return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_KEYS key %q: %w", kid, err)
		}
		ring.Keys[kid] = key
	}

	if active == "" {
		return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_KEYS requires AUTH_TOTP_ENCRYPTION_ACTIVE_KID (one of: %s)", strings.Join(sortedKeyIDs(ring.Keys), ", "))
	}
	if _, ok := ring.Keys[active]; !ok {
		return TOTPKeyring{}, fmt.Errorf("AUTH_TOTP_ENCRYPTION_ACTIVE_KID=%q is not one of the configured keys (%s)", active, strings.Join(sortedKeyIDs(ring.Keys), ", "))
	}
	ring.ActiveKID = active
	return ring, nil
}

func sortedKeyIDs(keys map[string][]byte) []string {
	out := make([]string, 0, len(keys))
	for kid := range keys {
		out = append(out, kid)
	}
	sort.Strings(out)
	return out
}
