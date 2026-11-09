package auth

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
)

// The compare-and-swap writes run against a real Postgres
// (SUPERAPI_TEST_DATABASE_URL): the guard has to hold in SQL, not in Go.

func TestRelationalCompareAndSwapPasswordHash(t *testing.T) {
	pg := dbtest.NewPostgres(t)
	repo := NewRelationalUserRepository(pg)
	ctx := context.Background()

	u, err := repo.Create(ctx, CreateStoredUserInput{Identifier: "cas-pw@example.com", PasswordHash: "old", Role: "user", Status: "active"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	hash := func() string {
		t.Helper()
		got, err := repo.GetByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		return got.PasswordHash
	}

	t.Run("applied when the hash matches", func(t *testing.T) {
		applied, err := repo.CompareAndSwapPasswordHash(ctx, u.ID, "old", "new")
		if err != nil || !applied {
			t.Fatalf("applied=%v err=%v, want applied", applied, err)
		}
		if got := hash(); got != "new" {
			t.Fatalf("hash = %q, want new", got)
		}
	})

	t.Run("not applied when the hash differs", func(t *testing.T) {
		applied, err := repo.CompareAndSwapPasswordHash(ctx, u.ID, "old", "forged")
		if err != nil || applied {
			t.Fatalf("applied=%v err=%v, want (false, nil)", applied, err)
		}
		if got := hash(); got != "new" {
			t.Fatalf("hash = %q after a rejected swap, want it untouched", got)
		}
	})

	t.Run("a second swap with the same expected hash loses", func(t *testing.T) {
		first, err1 := repo.CompareAndSwapPasswordHash(ctx, u.ID, "new", "winner")
		second, err2 := repo.CompareAndSwapPasswordHash(ctx, u.ID, "new", "loser")
		if err1 != nil || err2 != nil || !first || second {
			t.Fatalf("first=%v (%v) second=%v (%v), want only the first to apply", first, err1, second, err2)
		}
		if got := hash(); got != "winner" {
			t.Fatalf("hash = %q, want winner", got)
		}
	})

	t.Run("concurrent swaps from one hash have exactly one winner", func(t *testing.T) {
		const workers = 8
		wins := make(chan bool, workers)
		for i := 0; i < workers; i++ {
			go func() {
				applied, err := repo.CompareAndSwapPasswordHash(ctx, u.ID, "winner", "raced")
				if err != nil {
					t.Errorf("swap: %v", err)
				}
				wins <- applied
			}()
		}
		won := 0
		for i := 0; i < workers; i++ {
			if <-wins {
				won++
			}
		}
		if won != 1 {
			t.Fatalf("%d of %d concurrent swaps applied, want exactly 1", won, workers)
		}
		if got := hash(); got != "raced" {
			t.Fatalf("hash = %q, want raced", got)
		}
	})

	for _, tc := range []struct{ name, id string }{
		{"unknown user", "00000000-0000-0000-0000-000000000000"},
		{"malformed id", "not-a-uuid"},
		{"empty id", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			applied, err := repo.CompareAndSwapPasswordHash(ctx, tc.id, "raced", "x")
			if err != nil || applied {
				t.Fatalf("applied=%v err=%v, want (false, nil)", applied, err)
			}
		})
	}
}

func TestRelationalUpdateStatusIf(t *testing.T) {
	pg := dbtest.NewPostgres(t)
	repo := NewRelationalUserRepository(pg)
	ctx := context.Background()

	create := func(email string) StoredUser {
		t.Helper()
		u, err := repo.Create(ctx, CreateStoredUserInput{Identifier: email, PasswordHash: "h", Role: "user", Status: "pending_verification"})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		return u
	}
	u := create("cas-status@example.com")

	t.Run("applied from the expected status and advances the version", func(t *testing.T) {
		applied, err := repo.UpdateStatusIf(ctx, u.ID, "pending_verification", "active")
		if err != nil || !applied {
			t.Fatalf("applied=%v err=%v, want applied", applied, err)
		}
		got, err := repo.GetByID(ctx, u.ID)
		if err != nil || got.Status != "active" || got.AccountVersion != u.AccountVersion+1 {
			t.Fatalf("got status=%q version=%d (err=%v), want active and version %d", got.Status, got.AccountVersion, err, u.AccountVersion+1)
		}
	})

	t.Run("not applied from another status, nothing changes", func(t *testing.T) {
		before, _ := repo.GetByID(ctx, u.ID)
		applied, err := repo.UpdateStatusIf(ctx, u.ID, "pending_verification", "locked")
		if err != nil || applied {
			t.Fatalf("applied=%v err=%v, want (false, nil)", applied, err)
		}
		after, err := repo.GetByID(ctx, u.ID)
		if err != nil || after.Status != "active" || after.AccountVersion != before.AccountVersion {
			t.Fatalf("after a rejected swap: status=%q version=%d (err=%v), want active and version %d", after.Status, after.AccountVersion, err, before.AccountVersion)
		}
	})

	for _, tc := range []struct{ name, id string }{
		{"unknown user", "00000000-0000-0000-0000-000000000000"},
		{"malformed id", "not-a-uuid"},
		{"empty id", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			applied, err := repo.UpdateStatusIf(ctx, tc.id, "active", "locked")
			if err != nil || applied {
				t.Fatalf("applied=%v err=%v, want (false, nil)", applied, err)
			}
		})
	}

	// UpdateStatusIf must have the same side effects as UpdateStatus: the
	// status, the account version bump and updated_at, and nothing else.
	t.Run("same side effects as UpdateStatus", func(t *testing.T) {
		plain, viaCAS := create("cas-parity-a@example.com"), create("cas-parity-b@example.com")
		if _, err := repo.UpdateStatus(ctx, plain.ID, "active"); err != nil {
			t.Fatalf("UpdateStatus: %v", err)
		}
		if applied, err := repo.UpdateStatusIf(ctx, viaCAS.ID, "pending_verification", "active"); err != nil || !applied {
			t.Fatalf("UpdateStatusIf: applied=%v err=%v", applied, err)
		}
		a, _ := repo.GetByID(ctx, plain.ID)
		b, _ := repo.GetByID(ctx, viaCAS.ID)
		if a.Status != b.Status || a.AccountVersion != b.AccountVersion || a.Role != b.Role || a.TOTPEnabled != b.TOTPEnabled || a.PasswordHash != b.PasswordHash {
			t.Fatalf("UpdateStatus gave %+v, UpdateStatusIf gave %+v; they must change the same columns", a, b)
		}

		// updated_at moves forward on both (read through the sqlc queries the
		// repository uses, since StoredUser does not carry it).
		rowA, errA := pg.Queries(ctx).GetAuthUserByID(ctx, mustParseUserID(t, plain.ID))
		rowB, errB := pg.Queries(ctx).GetAuthUserByID(ctx, mustParseUserID(t, viaCAS.ID))
		if errA != nil || errB != nil {
			t.Fatalf("read rows: %v / %v", errA, errB)
		}
		if !rowA.UpdatedAt.Time.After(rowA.CreatedAt.Time) || !rowB.UpdatedAt.Time.After(rowB.CreatedAt.Time) {
			t.Fatalf("updated_at did not advance past created_at: plain %v/%v cas %v/%v", rowA.UpdatedAt.Time, rowA.CreatedAt.Time, rowB.UpdatedAt.Time, rowB.CreatedAt.Time)
		}
	})
}

func mustParseUserID(t *testing.T, id string) pgtype.UUID {
	t.Helper()
	parsed, err := parseUserID(id)
	if err != nil {
		t.Fatalf("parse user id %q: %v", id, err)
	}
	return parsed
}
