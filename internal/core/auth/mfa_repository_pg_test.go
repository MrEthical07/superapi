package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
)

// TestMFARepositorySQL runs against a real Postgres (SUPERAPI_TEST_DATABASE_URL)
// and exercises the single-statement semantics the provider relies on.
func TestMFARepositorySQL(t *testing.T) {
	pg := dbtest.NewPostgres(t)
	users := NewRelationalUserRepository(pg)
	mfa := NewMFARepository(pg)
	ctx := context.Background()

	u, err := users.Create(ctx, CreateStoredUserInput{TenantID: "tenant-a", Identifier: "mfa@example.com", PasswordHash: "h", Status: "active"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if u.AccountVersion != 1 || u.TOTPEnabled {
		t.Fatalf("new user: version=%d totp=%v", u.AccountVersion, u.TOTPEnabled)
	}
	version := func() (uint32, bool) {
		row, err := users.GetByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		return row.AccountVersion, row.TOTPEnabled
	}

	// Setup stores an unverified secret: TOTP stays off, version unchanged.
	if err := mfa.UpsertTOTPSecret(ctx, "tenant-a", u.ID, []byte("cipher-1")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if v, on := version(); v != 1 || on {
		t.Fatalf("after setup: version=%d enabled=%v", v, on)
	}
	// Other tenant cannot touch it.
	if err := mfa.UpsertTOTPSecret(ctx, "tenant-b", u.ID, []byte("evil")); !errors.Is(err, ErrAuthUserNotFound) {
		t.Fatalf("cross-tenant upsert err=%v", err)
	}
	if _, found, _ := mfa.GetTOTP(ctx, "tenant-b", u.ID); found {
		t.Fatal("cross-tenant GetTOTP must not find the record")
	}

	// Verify, then enable: version advances exactly once.
	if err := mfa.MarkTOTPVerified(ctx, "tenant-a", u.ID); err != nil {
		t.Fatalf("mark verified: %v", err)
	}
	if err := mfa.UpsertTOTPSecret(ctx, "tenant-a", u.ID, []byte("cipher-1")); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if v, on := version(); v != 2 || !on {
		t.Fatalf("after enable: version=%d enabled=%v", v, on)
	}
	state, found, err := mfa.GetTOTP(ctx, "", u.ID)
	if err != nil || !found || !state.Verified || !state.Enabled || string(state.SecretCiphertext) != "cipher-1" {
		t.Fatalf("state=%+v found=%v err=%v", state, found, err)
	}

	// Counter only moves forward.
	if err := mfa.AdvanceTOTPCounter(ctx, "tenant-a", u.ID, 10); err != nil {
		t.Fatalf("advance: %v", err)
	}
	for _, c := range []int64{10, 9} {
		if err := mfa.AdvanceTOTPCounter(ctx, "tenant-a", u.ID, c); !errors.Is(err, ErrTOTPCounterNotAdvanced) {
			t.Fatalf("counter %d: err=%v want ErrTOTPCounterNotAdvanced", c, err)
		}
	}

	// Backup codes: replace, single use under concurrency, replace again.
	h1, h2 := sha256.Sum256([]byte("code-1")), sha256.Sum256([]byte("code-2"))
	if err := mfa.ReplaceBackupCodes(ctx, "tenant-a", u.ID, [][32]byte{h1, h2}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := mfa.ReplaceBackupCodes(ctx, "tenant-b", u.ID, [][32]byte{h1}); !errors.Is(err, ErrAuthUserNotFound) {
		t.Fatalf("cross-tenant replace err=%v", err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := mfa.ConsumeBackupCode(ctx, "tenant-a", u.ID, h1)
			if err != nil {
				t.Errorf("consume: %v", err)
			}
			if ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("backup code consumed %d times, want exactly 1", wins.Load())
	}
	if ok, _ := mfa.ConsumeBackupCode(ctx, "tenant-b", u.ID, h2); ok {
		t.Fatal("cross-tenant consume succeeded")
	}
	codes, err := mfa.ListUnusedBackupCodes(ctx, "tenant-a", u.ID)
	if err != nil || len(codes) != 1 || codes[0] != h2 {
		t.Fatalf("unused codes=%v err=%v", codes, err)
	}
	if err := mfa.ReplaceBackupCodes(ctx, "tenant-a", u.ID, [][32]byte{h1, h2}); err != nil {
		t.Fatalf("replace again (reusing hashes): %v", err)
	}
	if codes, _ := mfa.ListUnusedBackupCodes(ctx, "tenant-a", u.ID); len(codes) != 2 {
		t.Fatalf("after replace: %d codes", len(codes))
	}

	// Disable removes secret and codes, clears the flag, advances version.
	if err := mfa.DisableTOTP(ctx, "tenant-b", u.ID); !errors.Is(err, ErrAuthUserNotFound) {
		t.Fatalf("cross-tenant disable err=%v", err)
	}
	if err := mfa.DisableTOTP(ctx, "tenant-a", u.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if v, on := version(); v != 3 || on {
		t.Fatalf("after disable: version=%d enabled=%v", v, on)
	}
	if _, found, _ := mfa.GetTOTP(ctx, "", u.ID); found {
		t.Fatal("secret must be gone")
	}
	if codes, _ := mfa.ListUnusedBackupCodes(ctx, "", u.ID); len(codes) != 0 {
		t.Fatalf("backup codes must be gone, got %d", len(codes))
	}

	// Status transitions advance the account version (goAuth requires it).
	if row, err := users.UpdateStatus(ctx, u.ID, "disabled"); err != nil || row.AccountVersion != 4 {
		t.Fatalf("status update: version=%d err=%v", row.AccountVersion, err)
	}
	// Duplicate identifiers map to ErrAuthUserExists.
	if _, err := users.Create(ctx, CreateStoredUserInput{Identifier: "mfa@example.com", PasswordHash: "h", Status: "active"}); !errors.Is(err, ErrAuthUserExists) {
		t.Fatalf("duplicate create err=%v", err)
	}
}
