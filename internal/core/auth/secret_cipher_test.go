package auth

import (
	"bytes"
	"errors"
	"testing"
)

func TestAESGCMCipher(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	c, err := NewAESGCMCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	secret := []byte("totp-seed-20-bytes!!")

	sealed, err := c.Seal("user-1", secret)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if bytes.Contains(sealed, secret) {
		t.Fatal("ciphertext contains the plaintext")
	}
	again, _ := c.Seal("user-1", secret)
	if bytes.Equal(sealed, again) {
		t.Fatal("nonce reuse: identical ciphertexts")
	}
	opened, err := c.Open("user-1", sealed)
	if err != nil || !bytes.Equal(opened, secret) {
		t.Fatalf("open: %q %v", opened, err)
	}

	// Bound to the user id: a row copied to another user fails.
	if _, err := c.Open("user-2", sealed); !errors.Is(err, ErrSecretDecrypt) {
		t.Fatalf("cross-user open err=%v", err)
	}
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := c.Open("user-1", tampered); !errors.Is(err, ErrSecretDecrypt) {
		t.Fatalf("tampered open err=%v", err)
	}
	other, _ := NewAESGCMCipher(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open("user-1", sealed); !errors.Is(err, ErrSecretDecrypt) {
		t.Fatalf("wrong key open err=%v", err)
	}
	if _, err := c.Open("user-1", []byte{1, 2, 3}); !errors.Is(err, ErrSecretDecrypt) {
		t.Fatalf("short input err=%v", err)
	}
	if _, err := NewAESGCMCipher([]byte("short")); err == nil {
		t.Fatal("expected error for short key")
	}
}
