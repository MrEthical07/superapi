package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

// sealV1 reproduces the v0.9.0 ciphertext format exactly, so tests can create
// rows the way an existing deployment has them.
func sealV1(t *testing.T, keyBytes []byte, userID string, plaintext []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	out := append([]byte{1}, nonce...)
	return aead.Seal(out, nonce, plaintext, []byte("superapi/totp/v1:"+userID))
}

type rotationFixture struct {
	pg     *storage.Postgres
	users  UserRepository
	mfa    MFARepository
	userID string
}

func newRotationFixture(t *testing.T) rotationFixture {
	t.Helper()
	pg := dbtest.NewPostgres(t)
	users := NewRelationalUserRepository(pg)
	u, err := users.Create(context.Background(), CreateStoredUserInput{Identifier: "rot@example.com", PasswordHash: "h", Status: "active"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return rotationFixture{pg: pg, users: users, mfa: NewMFARepository(pg), userID: u.ID}
}

// enroll stores ciphertext for the fixture user as a verified, enabled secret.
func (f rotationFixture) enroll(t *testing.T, ciphertext []byte) {
	t.Helper()
	ctx := context.Background()
	if err := f.mfa.UpsertTOTPSecret(ctx, "", f.userID, ciphertext); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := f.mfa.MarkTOTPVerified(ctx, "", f.userID); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := f.mfa.UpsertTOTPSecret(ctx, "", f.userID, ciphertext); err != nil {
		t.Fatalf("enable: %v", err)
	}
}

func (f rotationFixture) stored(t *testing.T) []byte {
	t.Helper()
	state, found, err := f.mfa.GetTOTP(context.Background(), "", f.userID)
	if err != nil || !found {
		t.Fatalf("GetTOTP: found=%v err=%v", found, err)
	}
	return state.SecretCiphertext
}

func (f rotationFixture) provider(c SecretCipher) *StoreUserProvider {
	return NewStoreUserProvider(f.users).WithMFA(f.mfa, c).WithTx(f.pg)
}

// A v0.9.0 row (format v1, single key) is read after the keyring is introduced
// and moved to the active key on first use, changing nothing else about the
// account.
func TestProviderRotatesV1SecretsLazily(t *testing.T) {
	f := newRotationFixture(t)
	ctx := context.Background()
	secret := []byte("totp-seed-from-v0.9.0")
	f.enroll(t, sealV1(t, key(7), f.userID, secret))

	before, _ := f.users.GetByID(ctx, f.userID)
	ring := mustRing(t, KeyRing{Keys: map[string][]byte{"new": key(9), DefaultSecretKeyID: key(7)}, ActiveKID: "new", LegacyKey: key(7)})
	p := f.provider(ring)

	rec, err := p.GetTOTPSecret(ctx, f.userID)
	if err != nil || string(rec.Secret) != string(secret) || !rec.Enabled || !rec.Verified {
		t.Fatalf("record = %+v err=%v", rec, err)
	}

	stored := f.stored(t)
	if stored[0] != 2 || ring.NeedsRotation(stored) {
		t.Fatalf("the row was not moved to the active key: % x", stored[:6])
	}
	// Reading again keeps working, and does not rewrite a current row.
	if rec, err := p.GetTOTPSecret(ctx, f.userID); err != nil || string(rec.Secret) != string(secret) {
		t.Fatalf("second read: %+v %v", rec, err)
	}
	if string(f.stored(t)) != string(stored) {
		t.Fatal("a current row must not be rewritten")
	}

	after, _ := f.users.GetByID(ctx, f.userID)
	if after.AccountVersion != before.AccountVersion || after.TOTPEnabled != before.TOTPEnabled {
		t.Fatalf("rotation changed account state: version %d->%d enabled %v->%v",
			before.AccountVersion, after.AccountVersion, before.TOTPEnabled, after.TOTPEnabled)
	}

	// The old key can now go.
	retired := mustRing(t, KeyRing{Keys: map[string][]byte{"new": key(9)}, ActiveKID: "new"})
	if rec, err := f.provider(retired).GetTOTPSecret(ctx, f.userID); err != nil || string(rec.Secret) != string(secret) {
		t.Fatalf("after retiring the old key: %+v %v", rec, err)
	}
}

func TestProviderRotatesSecretsFromAnOlderKeyID(t *testing.T) {
	f := newRotationFixture(t)
	ctx := context.Background()
	old := mustRing(t, KeyRing{Keys: map[string][]byte{"old": key(1)}, ActiveKID: "old"})
	sealed, _ := old.Seal(f.userID, []byte("seed"))
	f.enroll(t, sealed)

	ring := mustRing(t, KeyRing{Keys: map[string][]byte{"old": key(1), "new": key(2)}, ActiveKID: "new"})
	if _, err := f.provider(ring).GetTOTPSecret(ctx, f.userID); err != nil {
		t.Fatalf("GetTOTPSecret: %v", err)
	}
	if got := f.stored(t); ring.NeedsRotation(got) {
		t.Fatalf("row still under the old key: % x", got[:8])
	}
	if kid, _, _ := splitV2(f.stored(t)); kid != "new" {
		t.Fatalf("stored key id = %q, want new", kid)
	}
}

// The write-back never fails the request.
type failingRotateRepo struct {
	MFARepository
	calls int
}

func (r *failingRotateRepo) RotateTOTPSecret(context.Context, string, string, []byte, []byte) (bool, error) {
	r.calls++
	return false, errors.New("database unavailable")
}

func TestLazyRotationFailureDoesNotFailTheRequest(t *testing.T) {
	f := newRotationFixture(t)
	ctx := context.Background()
	f.enroll(t, sealV1(t, key(7), f.userID, []byte("seed")))

	ring := mustRing(t, KeyRing{Keys: map[string][]byte{"new": key(9)}, ActiveKID: "new", LegacyKey: key(7)})
	repo := &failingRotateRepo{MFARepository: f.mfa}
	p := NewStoreUserProvider(f.users).WithMFA(repo, ring).WithTx(f.pg)

	rec, err := p.GetTOTPSecret(ctx, f.userID)
	if err != nil || string(rec.Secret) != "seed" {
		t.Fatalf("a failed write-back must not fail the read: %+v %v", rec, err)
	}
	if repo.calls != 1 {
		t.Fatalf("write-back attempted %d times, want 1", repo.calls)
	}
	if f.stored(t)[0] != 1 {
		t.Fatal("the row must be unchanged when the write-back failed")
	}
}

// The write-back is a compare-and-swap: it never overwrites a secret that
// changed since it was read (for example a user who re-enrolled).
func TestRotateTOTPSecretIsCompareAndSwap(t *testing.T) {
	f := newRotationFixture(t)
	ctx := context.Background()
	original := []byte("ciphertext-original")
	f.enroll(t, original)

	swapped, err := f.mfa.RotateTOTPSecret(ctx, "", f.userID, []byte("stale-ciphertext"), []byte("next"))
	if err != nil || swapped {
		t.Fatalf("stale prev: swapped=%v err=%v, want no swap", swapped, err)
	}
	if string(f.stored(t)) != string(original) {
		t.Fatal("a stale swap changed the row")
	}

	swapped, err = f.mfa.RotateTOTPSecret(ctx, "", f.userID, original, []byte("ciphertext-next"))
	if err != nil || !swapped {
		t.Fatalf("matching prev: swapped=%v err=%v", swapped, err)
	}
	if string(f.stored(t)) != "ciphertext-next" {
		t.Fatalf("stored = %q", f.stored(t))
	}

	// Tenant scoping applies: a wrong tenant matches nothing.
	if swapped, _ := f.mfa.RotateTOTPSecret(ctx, "some-other-tenant", f.userID, []byte("ciphertext-next"), []byte("x")); swapped {
		t.Fatal("a swap must respect tenant scope")
	}
	// A missing row is a no-op, not an error.
	if swapped, err := f.mfa.RotateTOTPSecret(ctx, "", "00000000-0000-0000-0000-000000000000", []byte("a"), []byte("b")); err != nil || swapped {
		t.Fatalf("unknown user: swapped=%v err=%v", swapped, err)
	}

	// Disabled and re-enrolled meanwhile: nothing resurrected.
	if err := f.mfa.DisableTOTP(ctx, "", f.userID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if swapped, _ := f.mfa.RotateTOTPSecret(ctx, "", f.userID, []byte("ciphertext-next"), []byte("zombie")); swapped {
		t.Fatal("a swap must not recreate a removed secret")
	}
	if _, found, _ := f.mfa.GetTOTP(ctx, "", f.userID); found {
		t.Fatal("the secret must stay removed")
	}
}

// A retired key breaks only the rows still under it, with an error that names
// the key: the rows that were rotated keep working.
func TestRemovedKeyBreaksOnlyUnrotatedRowsThroughTheProvider(t *testing.T) {
	f := newRotationFixture(t)
	ctx := context.Background()
	other, err := f.users.Create(ctx, CreateStoredUserInput{Identifier: "other@example.com", PasswordHash: "h", Status: "active"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	both := mustRing(t, KeyRing{Keys: map[string][]byte{"old": key(1), "new": key(2)}, ActiveKID: "old"})
	sealedOld, _ := both.Seal(f.userID, []byte("seed-1"))
	f.enroll(t, sealedOld) // never rotated

	retired := mustRing(t, KeyRing{Keys: map[string][]byte{"new": key(2)}, ActiveKID: "new"})
	sealedNew, _ := retired.Seal(other.ID, []byte("seed-2"))
	if err := f.mfa.UpsertTOTPSecret(ctx, "", other.ID, sealedNew); err != nil {
		t.Fatalf("upsert other: %v", err)
	}

	p := f.provider(retired)
	if rec, err := p.GetTOTPSecret(ctx, other.ID); err != nil || string(rec.Secret) != "seed-2" {
		t.Fatalf("rotated row: %+v %v", rec, err)
	}
	_, err = p.GetTOTPSecret(ctx, f.userID)
	var keyErr *SecretKeyError
	if !errors.As(err, &keyErr) || keyErr.KeyID != "old" {
		t.Fatalf("un-rotated row: err = %v, want a *SecretKeyError for %q", err, "old")
	}
}
