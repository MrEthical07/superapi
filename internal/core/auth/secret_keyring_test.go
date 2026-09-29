package auth

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func mustRing(t *testing.T, ring KeyRing) RotatableCipher {
	t.Helper()
	c, err := NewKeyRingCipher(ring)
	if err != nil {
		t.Fatalf("NewKeyRingCipher: %v", err)
	}
	return c
}

// goldenV1 is a ciphertext produced by the v0.9.0 cipher (format v1) with key
// bytes.Repeat({7}, 32) for user "user-1". It must stay readable forever with
// the legacy key: it is what already-enrolled users have in the database.
const goldenV1 = "01b88dd70e4f076208f3db9727a4420ce0a061542545bb9a6923e392273f6cbb9c005e1eb0aba842a4d0fd59b2faad66ef"

const goldenSecret = "totp-seed-20-bytes!!"

func goldenBytes(t *testing.T) []byte {
	t.Helper()
	b, err := hex.DecodeString(goldenV1)
	if err != nil {
		t.Fatalf("golden vector: %v", err)
	}
	return b
}

func TestV1CiphertextsFromV090StillOpen(t *testing.T) {
	// Single-key mode (the v0.9.0 configuration, unchanged).
	single, err := NewAESGCMCipher(key(7))
	if err != nil {
		t.Fatalf("NewAESGCMCipher: %v", err)
	}
	got, err := single.Open("user-1", goldenBytes(t))
	if err != nil || string(got) != goldenSecret {
		t.Fatalf("single-key Open of a v0.9.0 ciphertext: %q %v", got, err)
	}

	// Keyring mode with the old key as the legacy key: still readable, and
	// flagged for rotation.
	ring := mustRing(t, KeyRing{
		Keys:      map[string][]byte{"k2": key(9), DefaultSecretKeyID: key(7)},
		ActiveKID: "k2",
		LegacyKey: key(7),
	})
	got, err = ring.Open("user-1", goldenBytes(t))
	if err != nil || string(got) != goldenSecret {
		t.Fatalf("ring Open of a v0.9.0 ciphertext: %q %v", got, err)
	}
	if !ring.NeedsRotation(goldenBytes(t)) {
		t.Fatal("a v1 ciphertext must be flagged for rotation")
	}

	// The v1 AAD still binds the user.
	if _, err := single.Open("user-2", goldenBytes(t)); !errors.Is(err, ErrSecretDecrypt) {
		t.Fatalf("v1 ciphertext opened for another user: %v", err)
	}
}

func TestV2FormatAndRoundTrip(t *testing.T) {
	c := mustRing(t, KeyRing{Keys: map[string][]byte{"2026-09": key(1)}, ActiveKID: "2026-09"})
	secret := []byte("another-seed-value")

	sealed, err := c.Seal("user-1", secret)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// 0x02 | kid length | kid | nonce(12) | sealed
	if sealed[0] != 2 || sealed[1] != byte(len("2026-09")) || string(sealed[2:2+len("2026-09")]) != "2026-09" {
		t.Fatalf("header = % x, want 02 | len | kid", sealed[:12])
	}
	wantLen := 2 + len("2026-09") + 12 + len(secret) + 16
	if len(sealed) != wantLen {
		t.Fatalf("len = %d, want %d (version, kid length, kid, 12-byte nonce, data, 16-byte tag)", len(sealed), wantLen)
	}
	if bytes.Contains(sealed, secret) {
		t.Fatal("ciphertext contains the plaintext")
	}
	if again, _ := c.Seal("user-1", secret); bytes.Equal(sealed, again) {
		t.Fatal("nonce reuse")
	}

	got, err := c.Open("user-1", sealed)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("Open: %q %v", got, err)
	}
	if c.NeedsRotation(sealed) {
		t.Fatal("a ciphertext under the active key must not need rotation")
	}

	// Bound to the user id.
	if _, err := c.Open("user-2", sealed); !errors.Is(err, ErrSecretDecrypt) {
		t.Fatalf("cross-user Open: %v", err)
	}
	// Tampering anywhere fails closed.
	for i := range sealed {
		tampered := append([]byte(nil), sealed...)
		tampered[i] ^= 0x01
		if _, err := c.Open("user-1", tampered); err == nil {
			t.Fatalf("byte %d flipped and Open still succeeded", i)
		}
	}
	for _, bad := range [][]byte{nil, {}, {2}, {2, 0}, {2, 5, 'a'}, {2, 33}, {9, 1, 2, 3}, {0}} {
		if _, err := c.Open("user-1", bad); err == nil {
			t.Fatalf("Open(% x) must fail", bad)
		}
	}
}

func TestSingleKeyCipherSealsInV2UnderTheDefaultKeyID(t *testing.T) {
	c, err := NewAESGCMCipher(key(3))
	if err != nil {
		t.Fatalf("NewAESGCMCipher: %v", err)
	}
	sealed, _ := c.Seal("u", []byte("s"))
	if sealed[0] != 2 || string(sealed[2:2+len(DefaultSecretKeyID)]) != DefaultSecretKeyID {
		t.Fatalf("header = % x, want v2 under %q", sealed[:12], DefaultSecretKeyID)
	}
	if _, err := NewAESGCMCipher([]byte("short")); err == nil {
		t.Fatal("expected an error for a short key")
	}
}

// The rotation story end to end: seal under the old key, add a new active key,
// read the old row, re-seal it, then drop the old key.
func TestRotationMovesRowsToTheNewKey(t *testing.T) {
	oldRing := mustRing(t, KeyRing{Keys: map[string][]byte{"old": key(1)}, ActiveKID: "old"})
	sealedOld, _ := oldRing.Seal("u1", []byte("secret-1"))

	newRing := mustRing(t, KeyRing{Keys: map[string][]byte{"old": key(1), "new": key(2)}, ActiveKID: "new"})
	if !newRing.NeedsRotation(sealedOld) {
		t.Fatal("a row under a non-active key must need rotation")
	}
	plain, err := newRing.Open("u1", sealedOld)
	if err != nil || string(plain) != "secret-1" {
		t.Fatalf("open under the new ring: %q %v", plain, err)
	}
	moved, err := newRing.Seal("u1", plain)
	if err != nil {
		t.Fatalf("re-seal: %v", err)
	}
	if newRing.NeedsRotation(moved) {
		t.Fatal("a re-sealed row must be current")
	}

	// Now the old key is retired.
	retired := mustRing(t, KeyRing{Keys: map[string][]byte{"new": key(2)}, ActiveKID: "new"})
	if got, err := retired.Open("u1", moved); err != nil || string(got) != "secret-1" {
		t.Fatalf("a rotated row must survive removing the old key: %q %v", got, err)
	}
}

// Removing a key breaks only the rows still sealed with it, with an error that
// names the key.
func TestRemovedKeyOnlyBreaksRowsThatStillUseIt(t *testing.T) {
	both := mustRing(t, KeyRing{Keys: map[string][]byte{"old": key(1), "new": key(2)}, ActiveKID: "old"})
	stale, _ := both.Seal("u1", []byte("stale"))
	fresh := mustRing(t, KeyRing{Keys: map[string][]byte{"new": key(2)}, ActiveKID: "new"})
	current, _ := fresh.Seal("u2", []byte("current"))

	retired := mustRing(t, KeyRing{Keys: map[string][]byte{"new": key(2)}, ActiveKID: "new"})

	if got, err := retired.Open("u2", current); err != nil || string(got) != "current" {
		t.Fatalf("a row under the kept key must still open: %q %v", got, err)
	}

	_, err := retired.Open("u1", stale)
	var keyErr *SecretKeyError
	if !errors.As(err, &keyErr) {
		t.Fatalf("err = %v, want a *SecretKeyError", err)
	}
	if keyErr.KeyID != "old" {
		t.Fatalf("KeyID = %q, want the missing key %q", keyErr.KeyID, "old")
	}
	if !errors.Is(err, ErrSecretKeyNotConfigured) || !errors.Is(err, ErrSecretDecrypt) {
		t.Fatalf("err must match both ErrSecretKeyNotConfigured and ErrSecretDecrypt: %v", err)
	}
	for _, want := range []string{`"old"`, "AUTH_TOTP_ENCRYPTION_KEYS", "rotatetotpkey"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should mention %s: %v", want, err)
		}
	}
}

func TestLegacyV1RowsNeedTheLegacyKey(t *testing.T) {
	noLegacy := mustRing(t, KeyRing{Keys: map[string][]byte{"k": key(2)}, ActiveKID: "k"})
	_, err := noLegacy.Open("user-1", goldenBytes(t))
	var keyErr *SecretKeyError
	if !errors.As(err, &keyErr) || keyErr.KeyID != "" {
		t.Fatalf("err = %v, want a legacy-key *SecretKeyError", err)
	}
	if !strings.Contains(err.Error(), "AUTH_TOTP_ENCRYPTION_KEY") || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("error should point at the legacy key: %v", err)
	}
}

func TestNeedsRotation(t *testing.T) {
	c := mustRing(t, KeyRing{Keys: map[string][]byte{"a": key(1), "b": key(2)}, ActiveKID: "b", LegacyKey: key(1)})
	other := mustRing(t, KeyRing{Keys: map[string][]byte{"a": key(1)}, ActiveKID: "a"})

	underA, _ := other.Seal("u", []byte("s"))
	underB, _ := c.Seal("u", []byte("s"))
	tests := []struct {
		name string
		ct   []byte
		want bool
	}{
		{"v1", goldenBytes(t), true},
		{"v2 non-active key", underA, true},
		{"v2 active key", underB, false},
		{"empty", nil, false},
		{"unknown version", []byte{9, 1, 2}, false},
		{"truncated v2 header", []byte{2, 5, 'a'}, false},
	}
	for _, tc := range tests {
		if got := c.NeedsRotation(tc.ct); got != tc.want {
			t.Errorf("%s: NeedsRotation = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNewKeyRingCipherValidation(t *testing.T) {
	tests := []struct {
		name    string
		ring    KeyRing
		wantErr string
	}{
		{"no keys", KeyRing{ActiveKID: "a"}, "no keys"},
		{"active key missing", KeyRing{Keys: map[string][]byte{"a": key(1)}, ActiveKID: "b"}, `active key id "b"`},
		{"short key", KeyRing{Keys: map[string][]byte{"a": []byte("short")}, ActiveKID: "a"}, "32 bytes"},
		{"invalid kid characters", KeyRing{Keys: map[string][]byte{"a b": key(1)}, ActiveKID: "a b"}, "invalid key id"},
		{"kid too long", KeyRing{Keys: map[string][]byte{strings.Repeat("k", 33): key(1)}, ActiveKID: strings.Repeat("k", 33)}, "invalid key id"},
		{"empty kid", KeyRing{Keys: map[string][]byte{"": key(1)}, ActiveKID: ""}, "invalid key id"},
		{"bad legacy key", KeyRing{Keys: map[string][]byte{"a": key(1)}, ActiveKID: "a", LegacyKey: []byte("x")}, "legacy key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewKeyRingCipher(tc.ring)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

// The auth package and the config package each validate key ids (auth cannot
// import config); they must agree.
func TestKeyIDRulesMatchConfig(t *testing.T) {
	for _, kid := range []string{"a", "default", "2026-09", "k.1_x", strings.Repeat("k", 32), strings.Repeat("k", 33), "", "a b", "a:b", "a,b", "é", "-x", "x/y"} {
		if ValidKeyID(kid) != config.ValidTOTPKeyID(kid) {
			t.Errorf("key id %q: auth=%v config=%v", kid, ValidKeyID(kid), config.ValidTOTPKeyID(kid))
		}
	}
	if DefaultSecretKeyID != config.DefaultTOTPKeyID {
		t.Fatalf("default key ids differ: %q vs %q", DefaultSecretKeyID, config.DefaultTOTPKeyID)
	}
}

func TestSealsAreUsableAcrossManyRotations(t *testing.T) {
	// Sealing is cheap enough to do in bulk (the rotation command does).
	c := mustRing(t, KeyRing{Keys: map[string][]byte{"a": key(1)}, ActiveKID: "a"})
	start := time.Now()
	for i := 0; i < 2000; i++ {
		s, err := c.Seal("user", []byte("secret"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Open("user", s); err != nil {
			t.Fatal(err)
		}
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("sealing is unexpectedly slow")
	}
}
