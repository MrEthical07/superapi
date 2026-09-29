package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

func hashOf(s string) [32]byte { return sha256.Sum256([]byte(s)) }

type backupFixture struct {
	pg     *storage.Postgres
	mfa    MFARepository
	userID string
}

func newBackupFixture(t *testing.T) backupFixture {
	t.Helper()
	pg := dbtest.NewPostgres(t)
	user, err := NewRelationalUserRepository(pg).Create(context.Background(), CreateStoredUserInput{
		TenantID: "tenant-a", Identifier: "codes@example.com", PasswordHash: "h", Status: "active",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return backupFixture{pg: pg, mfa: NewMFARepository(pg), userID: user.ID}
}

func (f backupFixture) replace(t *testing.T, tenantID string, hashes ...[32]byte) error {
	t.Helper()
	return f.pg.WithTx(context.Background(), func(ctx context.Context) error {
		return f.mfa.ReplaceBackupCodes(ctx, tenantID, f.userID, hashes)
	})
}

func (f backupFixture) unused(t *testing.T) map[[32]byte]bool {
	t.Helper()
	list, err := f.mfa.ListUnusedBackupCodes(context.Background(), "tenant-a", f.userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := make(map[[32]byte]bool, len(list))
	for _, h := range list {
		out[h] = true
	}
	return out
}

// Replacing with a set that overlaps the old one is the case the old
// single-statement version could not support once the unique constraint
// existed.
func TestReplaceBackupCodesWithAnOverlappingSet(t *testing.T) {
	f := newBackupFixture(t)
	ctx := context.Background()
	a, b, c, d := hashOf("a"), hashOf("b"), hashOf("c"), hashOf("d")

	if err := f.replace(t, "tenant-a", a, b, c); err != nil {
		t.Fatalf("first replace: %v", err)
	}
	if ok, err := f.mfa.ConsumeBackupCode(ctx, "tenant-a", f.userID, a); err != nil || !ok {
		t.Fatalf("consume a: ok=%v err=%v", ok, err)
	}

	// {b, c} are kept from the old set, d is new, a is dropped.
	if err := f.replace(t, "tenant-a", b, c, d); err != nil {
		t.Fatalf("overlapping replace: %v", err)
	}
	got := f.unused(t)
	if len(got) != 3 || !got[b] || !got[c] || !got[d] || got[a] {
		t.Fatalf("unused after replace = %v, want exactly {b,c,d}", got)
	}

	// Re-including a hash that had been consumed makes it usable again (it is
	// a fresh row), once.
	if err := f.replace(t, "tenant-a", a, b); err != nil {
		t.Fatalf("replace with a previously consumed hash: %v", err)
	}
	if got := f.unused(t); len(got) != 2 || !got[a] || !got[b] {
		t.Fatalf("unused = %v, want {a,b}", got)
	}
	if ok, _ := f.mfa.ConsumeBackupCode(ctx, "tenant-a", f.userID, a); !ok {
		t.Fatal("a fresh row must be consumable")
	}
	if ok, _ := f.mfa.ConsumeBackupCode(ctx, "tenant-a", f.userID, a); ok {
		t.Fatal("a code must not be consumable twice")
	}

	// Replacing with the identical set works too.
	if err := f.replace(t, "tenant-a", a, b); err != nil {
		t.Fatalf("replace with the same set: %v", err)
	}
	if got := f.unused(t); len(got) != 2 {
		t.Fatalf("unused = %v", got)
	}
	// And with an empty set: every code removed.
	if err := f.replace(t, "tenant-a"); err != nil {
		t.Fatalf("replace with an empty set: %v", err)
	}
	if got := f.unused(t); len(got) != 0 {
		t.Fatalf("unused = %v, want none", got)
	}
}

func TestReplaceBackupCodesDeduplicatesTheNewSet(t *testing.T) {
	f := newBackupFixture(t)
	a, b := hashOf("a"), hashOf("b")
	if err := f.replace(t, "tenant-a", a, b, a, a); err != nil {
		t.Fatalf("replace with repeated hashes: %v", err)
	}
	if got := f.unused(t); len(got) != 2 {
		t.Fatalf("unused = %v, want a and b once each", got)
	}
}

// A code is single-use, sequentially and under a race.
func TestBackupCodeCannotBeConsumedTwice(t *testing.T) {
	f := newBackupFixture(t)
	ctx := context.Background()
	a := hashOf("only")
	if err := f.replace(t, "tenant-a", a); err != nil {
		t.Fatalf("replace: %v", err)
	}

	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := f.mfa.ConsumeBackupCode(ctx, "tenant-a", f.userID, a)
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
		t.Fatalf("consumed %d times, want exactly 1", wins.Load())
	}
	if ok, _ := f.mfa.ConsumeBackupCode(ctx, "tenant-a", f.userID, a); ok {
		t.Fatal("consumed again after the race")
	}
}

// The two statements are only safe together: outside a transaction the
// repository refuses to run, and changes nothing.
func TestReplaceBackupCodesRequiresATransaction(t *testing.T) {
	f := newBackupFixture(t)
	a, b := hashOf("a"), hashOf("b")
	if err := f.replace(t, "tenant-a", a); err != nil {
		t.Fatalf("seed: %v", err)
	}

	err := f.mfa.ReplaceBackupCodes(context.Background(), "tenant-a", f.userID, [][32]byte{b})
	if !errors.Is(err, ErrTransactionRequired) {
		t.Fatalf("err = %v, want ErrTransactionRequired", err)
	}
	if got := f.unused(t); len(got) != 1 || !got[a] {
		t.Fatalf("a refused replace changed the codes: %v", got)
	}
}

// A failure after the delete rolls the delete back: the user keeps the old
// codes instead of being left with none.
func TestReplaceBackupCodesRollsBackAsOne(t *testing.T) {
	f := newBackupFixture(t)
	a, b := hashOf("a"), hashOf("b")
	if err := f.replace(t, "tenant-a", a); err != nil {
		t.Fatalf("seed: %v", err)
	}

	boom := errors.New("boom")
	err := f.pg.WithTx(context.Background(), func(ctx context.Context) error {
		if err := f.mfa.ReplaceBackupCodes(ctx, "tenant-a", f.userID, [][32]byte{b}); err != nil {
			return err
		}
		return boom // something later in the same unit of work fails
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if got := f.unused(t); len(got) != 1 || !got[a] {
		t.Fatalf("after rollback unused = %v, want the original {a}", got)
	}

	// A user that is not in scope: nothing inserted, error returned, and the
	// transaction rolls the delete back.
	err = f.replace(t, "tenant-b", b)
	if !errors.Is(err, ErrAuthUserNotFound) {
		t.Fatalf("cross-tenant err = %v", err)
	}
	if got := f.unused(t); len(got) != 1 || !got[a] {
		t.Fatalf("after failed cross-tenant replace unused = %v, want {a}", got)
	}
}

// Through the goAuth provider, the way the engine calls it.
func TestProviderReplaceBackupCodesIsTransactional(t *testing.T) {
	f := newBackupFixture(t)
	cipher, err := NewAESGCMCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	provider := NewStoreUserProvider(NewRelationalUserRepository(f.pg)).WithMFA(f.mfa, cipher).WithTx(f.pg)
	ctx := context.Background()

	record := func(s string) goauth.BackupCodeRecord { return goauth.BackupCodeRecord{Hash: hashOf(s)} }
	if err := provider.ReplaceBackupCodes(ctx, f.userID, []goauth.BackupCodeRecord{record("a"), record("b")}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := provider.ReplaceBackupCodes(ctx, f.userID, []goauth.BackupCodeRecord{record("b"), record("c")}); err != nil {
		t.Fatalf("overlapping replace through the provider: %v", err)
	}
	got, err := provider.GetBackupCodes(ctx, f.userID)
	if err != nil || len(got) != 2 {
		t.Fatalf("codes = %v err=%v", got, err)
	}

	// Unknown user: goAuth's not-found error, existing codes untouched.
	if err := provider.ReplaceBackupCodes(ctx, "00000000-0000-0000-0000-000000000000", []goauth.BackupCodeRecord{record("z")}); !errors.Is(err, goauth.ErrUserNotFound) {
		t.Fatalf("unknown user err = %v, want goauth.ErrUserNotFound", err)
	}
	if got, _ := provider.GetBackupCodes(ctx, f.userID); len(got) != 2 {
		t.Fatalf("codes changed by an unrelated failure: %v", got)
	}

	if ok, err := provider.ConsumeBackupCode(ctx, f.userID, hashOf("b")); err != nil || !ok {
		t.Fatalf("consume: ok=%v err=%v", ok, err)
	}
	if ok, _ := provider.ConsumeBackupCode(ctx, f.userID, hashOf("b")); ok {
		t.Fatal("a backup code was consumed twice")
	}
}

// Without a transaction runner the relational repository fails loudly rather
// than silently doing a non-atomic replacement.
func TestProviderWithoutATransactionRunnerFailsLoudly(t *testing.T) {
	f := newBackupFixture(t)
	cipher, err := NewAESGCMCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	provider := NewStoreUserProvider(NewRelationalUserRepository(f.pg)).WithMFA(f.mfa, cipher) // no WithTx

	err = provider.ReplaceBackupCodes(context.Background(), f.userID, []goauth.BackupCodeRecord{{Hash: hashOf("a")}})
	if !errors.Is(err, ErrTransactionRequired) {
		t.Fatalf("err = %v, want ErrTransactionRequired", err)
	}
}
