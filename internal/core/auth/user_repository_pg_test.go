package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
)

// TestRelationalUserRepositoryLookups runs against a real Postgres
// (SUPERAPI_TEST_DATABASE_URL) to prove lookups and identifier uniqueness are
// enforced in SQL.
func TestRelationalUserRepositoryLookups(t *testing.T) {
	repo := NewRelationalUserRepository(dbtest.NewPostgres(t))
	ctx := context.Background()

	alice, err := repo.Create(ctx, CreateStoredUserInput{Identifier: "alice@example.com", PasswordHash: "h", Role: "user", Status: "active"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Create(ctx, CreateStoredUserInput{Identifier: "bob@example.com", PasswordHash: "h", Status: "active"}); err != nil {
		t.Fatalf("create second user: %v", err)
	}

	if got, err := repo.GetByIdentifier(ctx, "alice@example.com"); err != nil || got.ID != alice.ID {
		t.Fatalf("identifier lookup: got=%+v err=%v", got, err)
	}
	if got, err := repo.GetByID(ctx, alice.ID); err != nil || got.ID != alice.ID {
		t.Fatalf("id lookup: got=%+v err=%v", got, err)
	}

	notFound := []struct {
		name string
		call func() error
	}{
		{"unknown identifier", func() error { _, err := repo.GetByIdentifier(ctx, "nobody@example.com"); return err }},
		{"unknown id", func() error { _, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000"); return err }},
		{"empty identifier", func() error { _, err := repo.GetByIdentifier(ctx, ""); return err }},
	}
	for _, tc := range notFound {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrAuthUserNotFound) {
				t.Fatalf("err=%v want ErrAuthUserNotFound", err)
			}
		})
	}

	// Identifier uniqueness is global (users_email_unique_idx).
	if _, err := repo.Create(ctx, CreateStoredUserInput{Identifier: "alice@example.com", PasswordHash: "h", Status: "active"}); !errors.Is(err, ErrAuthUserExists) {
		t.Fatalf("duplicate identifier err=%v, want ErrAuthUserExists", err)
	}
}

// Identifiers are case-insensitive in SQL: lookups match any case, the stored
// value is lower-cased, and a case-variant duplicate is "already exists".
func TestRelationalUserRepositoryIdentifiersAreCaseInsensitive(t *testing.T) {
	repo := NewRelationalUserRepository(dbtest.NewPostgres(t))
	ctx := context.Background()

	created, err := repo.Create(ctx, CreateStoredUserInput{Identifier: "  Alice@Example.COM ", PasswordHash: "h", Status: "active"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Email != "alice@example.com" {
		t.Fatalf("stored email = %q, want lower-cased and trimmed", created.Email)
	}

	for _, id := range []string{"alice@example.com", "ALICE@EXAMPLE.COM", "Alice@Example.com", "  aLiCe@example.com  "} {
		if got, err := repo.GetByIdentifier(ctx, id); err != nil || got.ID != created.ID {
			t.Fatalf("GetByIdentifier(%q): got=%+v err=%v", id, got, err)
		}
	}

	for _, dup := range []string{"alice@example.com", "ALICE@example.com", "Alice@Example.Com"} {
		if _, err := repo.Create(ctx, CreateStoredUserInput{Identifier: dup, PasswordHash: "h", Status: "active"}); !errors.Is(err, ErrAuthUserExists) {
			t.Fatalf("Create(%q) err=%v, want ErrAuthUserExists", dup, err)
		}
	}
}
