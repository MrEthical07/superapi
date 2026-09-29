package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// SecretCipher encrypts small secrets (TOTP seeds) at rest. The user id is
// bound as additional authenticated data, so a ciphertext copied onto another
// user's row fails to decrypt.
type SecretCipher interface {
	Seal(userID string, plaintext []byte) ([]byte, error)
	Open(userID string, ciphertext []byte) ([]byte, error)
}

// RotatableCipher is implemented by ciphers that hold more than one key.
// NeedsRotation reports whether ciphertext is not already in the current
// format under the active key, so callers can re-seal it lazily.
type RotatableCipher interface {
	SecretCipher
	NeedsRotation(ciphertext []byte) bool
}

// Ciphertext formats.
//
//	v1 (v0.9.0):  0x01 | nonce | sealed                  (single key, no key id)
//	v2:           0x02 | kid length | kid | nonce | sealed
//
// The version byte lets the format evolve; the key id in v2 says which key
// sealed the row, so keys can be rotated without re-enrolling every user.
const (
	secretFormatV1 byte = 1
	secretFormatV2 byte = 2
)

const (
	secretAADPrefixV1 = "superapi/totp/v1:"
	secretAADPrefixV2 = "superapi/totp/v2:"
)

// DefaultSecretKeyID is the key id given to the single AUTH_TOTP_ENCRYPTION_KEY
// when it is used on its own or alongside a keyring.
const DefaultSecretKeyID = "default"

// maxKeyIDLength bounds key ids so the v2 header stays small.
const maxKeyIDLength = 32

var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// ValidKeyID reports whether kid is usable as a key id: 1-32 characters from
// [A-Za-z0-9._-].
func ValidKeyID(kid string) bool { return keyIDPattern.MatchString(kid) }

// ErrSecretDecrypt is returned when a stored secret cannot be decrypted (wrong
// key, tampering, or a row copied between users).
var ErrSecretDecrypt = errors.New("stored secret could not be decrypted")

// ErrSecretKeyNotConfigured is returned when a stored secret was sealed under a
// key that is not configured any more: it names the key so the operator can
// restore it (or re-enroll the affected users). errors.Is also matches
// ErrSecretDecrypt.
var ErrSecretKeyNotConfigured = errors.New("stored secret was sealed with a key that is not configured")

// SecretKeyError is the concrete error for a missing key.
type SecretKeyError struct {
	// KeyID is the id recorded in the ciphertext; empty for a v1 ciphertext,
	// which was sealed by the legacy single key.
	KeyID string
}

func (e *SecretKeyError) Error() string {
	if e.KeyID == "" {
		return "stored secret was sealed with the legacy single key (format v1), but AUTH_TOTP_ENCRYPTION_KEY is not configured; restore it, or re-encrypt with the rotatetotpkey command before removing it"
	}
	return fmt.Sprintf("stored secret was sealed with key %q, which is not in AUTH_TOTP_ENCRYPTION_KEYS; restore it, or re-encrypt with the rotatetotpkey command before removing it", e.KeyID)
}

// Is makes a SecretKeyError match both sentinels.
func (e *SecretKeyError) Is(target error) bool {
	return target == ErrSecretKeyNotConfigured || target == ErrSecretDecrypt
}

// KeyRing seals with one active key and opens with any configured key.
type KeyRing struct {
	// Keys maps key id to a 32-byte AES-256 key.
	Keys map[string][]byte
	// ActiveKID is the key new secrets are sealed with; it must be in Keys.
	ActiveKID string
	// LegacyKey opens v1 ciphertexts (written before key ids existed). Nil
	// means v1 rows cannot be read.
	LegacyKey []byte
}

type aesGCMCipher struct {
	active string
	aeads  map[string]cipher.AEAD
	legacy cipher.AEAD // nil when v1 rows cannot be opened
}

// NewAESGCMCipher returns an AES-256-GCM SecretCipher for a single key
// (AUTH_TOTP_ENCRYPTION_KEY, base64-decoded). It seals in the current format
// under the key id DefaultSecretKeyID and still opens v1 ciphertexts written
// with the same key by v0.9.0.
func NewAESGCMCipher(key []byte) (SecretCipher, error) {
	return NewKeyRingCipher(KeyRing{
		Keys:      map[string][]byte{DefaultSecretKeyID: key},
		ActiveKID: DefaultSecretKeyID,
		LegacyKey: key,
	})
}

// NewKeyRingCipher builds a cipher from a keyring. It validates every key
// (32 bytes), every key id, and that the active key exists.
func NewKeyRingCipher(ring KeyRing) (RotatableCipher, error) {
	if len(ring.Keys) == 0 {
		return nil, errors.New("secret cipher: no keys configured")
	}
	c := &aesGCMCipher{active: ring.ActiveKID, aeads: make(map[string]cipher.AEAD, len(ring.Keys))}
	for kid, key := range ring.Keys {
		if !ValidKeyID(kid) {
			return nil, fmt.Errorf("secret cipher: invalid key id %q (1-%d characters from [A-Za-z0-9._-])", kid, maxKeyIDLength)
		}
		aead, err := newAEAD(key)
		if err != nil {
			return nil, fmt.Errorf("secret cipher key %q: %w", kid, err)
		}
		c.aeads[kid] = aead
	}
	if _, ok := c.aeads[ring.ActiveKID]; !ok {
		return nil, fmt.Errorf("secret cipher: active key id %q is not among the configured keys (%s)", ring.ActiveKID, strings.Join(sortedKeys(ring.Keys), ", "))
	}
	if ring.LegacyKey != nil {
		aead, err := newAEAD(ring.LegacyKey)
		if err != nil {
			return nil, fmt.Errorf("secret cipher legacy key: %w", err)
		}
		c.legacy = aead
	}
	return c, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secret cipher key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secret cipher: %w", err)
	}
	return aead, nil
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Seal encrypts plaintext under the active key in format v2.
func (c *aesGCMCipher) Seal(userID string, plaintext []byte) ([]byte, error) {
	aead := c.aeads[c.active]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secret cipher nonce: %w", err)
	}
	out := make([]byte, 0, 2+len(c.active)+len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, secretFormatV2, byte(len(c.active)))
	out = append(out, c.active...)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, []byte(secretAADPrefixV2+userID)), nil
}

// Open decrypts a v1 or v2 ciphertext. A secret sealed with a key that is no
// longer configured fails with a *SecretKeyError naming it; any other failure
// is ErrSecretDecrypt.
func (c *aesGCMCipher) Open(userID string, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, ErrSecretDecrypt
	}
	switch ciphertext[0] {
	case secretFormatV1:
		if c.legacy == nil {
			return nil, &SecretKeyError{}
		}
		return openWith(c.legacy, ciphertext[1:], secretAADPrefixV1+userID)
	case secretFormatV2:
		kid, rest, ok := splitV2(ciphertext)
		if !ok {
			return nil, ErrSecretDecrypt
		}
		aead, ok := c.aeads[kid]
		if !ok {
			return nil, &SecretKeyError{KeyID: kid}
		}
		return openWith(aead, rest, secretAADPrefixV2+userID)
	default:
		return nil, ErrSecretDecrypt
	}
}

// NeedsRotation reports whether ciphertext is anything other than a v2
// ciphertext sealed under the active key. Unparseable input is not reported:
// re-sealing garbage is not possible, and Open will say what is wrong with it.
func (c *aesGCMCipher) NeedsRotation(ciphertext []byte) bool {
	if len(ciphertext) == 0 {
		return false
	}
	switch ciphertext[0] {
	case secretFormatV1:
		return true
	case secretFormatV2:
		kid, _, ok := splitV2(ciphertext)
		return ok && kid != c.active
	default:
		return false
	}
}

// splitV2 parses the v2 header, returning the key id and the nonce+sealed
// remainder.
func splitV2(ciphertext []byte) (kid string, rest []byte, ok bool) {
	if len(ciphertext) < 2 {
		return "", nil, false
	}
	n := int(ciphertext[1])
	if n == 0 || n > maxKeyIDLength || len(ciphertext) < 2+n {
		return "", nil, false
	}
	return string(ciphertext[2 : 2+n]), ciphertext[2+n:], true
}

func openWith(aead cipher.AEAD, nonceAndSealed []byte, aad string) ([]byte, error) {
	nonceSize := aead.NonceSize()
	if len(nonceAndSealed) < nonceSize+aead.Overhead() {
		return nil, ErrSecretDecrypt
	}
	plaintext, err := aead.Open(nil, nonceAndSealed[:nonceSize], nonceAndSealed[nonceSize:], []byte(aad))
	if err != nil {
		return nil, ErrSecretDecrypt
	}
	return plaintext, nil
}
