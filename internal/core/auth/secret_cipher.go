package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// SecretCipher encrypts small secrets (TOTP seeds) at rest. The user id is
// bound as additional authenticated data, so a ciphertext copied onto another
// user's row fails to decrypt.
type SecretCipher interface {
	Seal(userID string, plaintext []byte) ([]byte, error)
	Open(userID string, ciphertext []byte) ([]byte, error)
}

// secretFormatV1 prefixes every ciphertext so the format (and a future key
// rotation scheme) can evolve without ambiguity.
const secretFormatV1 byte = 1

const secretAADPrefix = "superapi/totp/v1:"

// ErrSecretDecrypt is returned when a stored secret cannot be decrypted (wrong
// key, tampering, or a row copied between users).
var ErrSecretDecrypt = errors.New("stored secret could not be decrypted")

type aesGCMCipher struct {
	aead cipher.AEAD
}

// NewAESGCMCipher returns an AES-256-GCM SecretCipher. key must be 32 bytes
// (AUTH_TOTP_ENCRYPTION_KEY, base64-decoded).
func NewAESGCMCipher(key []byte) (SecretCipher, error) {
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
	return &aesGCMCipher{aead: aead}, nil
}

func (c *aesGCMCipher) Seal(userID string, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secret cipher nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+c.aead.Overhead())
	out = append(out, secretFormatV1)
	out = append(out, nonce...)
	return c.aead.Seal(out, nonce, plaintext, []byte(secretAADPrefix+userID)), nil
}

func (c *aesGCMCipher) Open(userID string, ciphertext []byte) ([]byte, error) {
	nonceSize := c.aead.NonceSize()
	if len(ciphertext) < 1+nonceSize+c.aead.Overhead() || ciphertext[0] != secretFormatV1 {
		return nil, ErrSecretDecrypt
	}
	nonce := ciphertext[1 : 1+nonceSize]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext[1+nonceSize:], []byte(secretAADPrefix+userID))
	if err != nil {
		return nil, ErrSecretDecrypt
	}
	return plaintext, nil
}
