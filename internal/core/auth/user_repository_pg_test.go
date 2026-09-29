package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
)

// TestRelationalUserRepositoryTenantScoping runs against a real Postgres
// (SUPERAPI_TEST_DATABASE_URL) to prove tenant scoping is enforced in SQL.
func TestRelationalUserRepositoryTenantScoping(t *testing.T) {
	repo := NewRelationalUserRepository(dbtest.NewPostgres(t))
	ctx := context.Background()

	alice, err := repo.Create(ctx, CreateStoredUserInput{TenantID: "tenant-a", Identifier: "alice@example.com", PasswordHash: "h", Role: "user", Status: "active"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if alice.TenantID != "tenant-a" {
		t.Fatalf("tenant=%q want tenant-a", alice.TenantID)
	}
	legacy, err := repo.Create(ctx, CreateStoredUserInput{Identifier: "legacy@example.com", PasswordHash: "h", Status: "active"})
	if err != nil {
		t.Fatalf("create legacy: %v", err)
	}
	if legacy.TenantID != DefaultTenantID {
		t.Fatalf("empty tenant must default to %q, got %q", DefaultTenantID, legacy.TenantID)
	}

	if got, err := repo.GetByIdentifierInTenant(ctx, "tenant-a", "alice@example.com"); err != nil || got.ID != alice.ID {
		t.Fatalf("same-tenant identifier lookup: got=%+v err=%v", got, err)
	}
	if got, err := repo.GetByIDInTenant(ctx, "tenant-a", alice.ID); err != nil || got.ID != alice.ID {
		t.Fatalf("same-tenant id lookup: got=%+v err=%v", got, err)
	}

	notFound := []struct {
		name string
		call func() error
	}{
		{"identifier in other tenant", func() error { _, err := repo.GetByIdentifierInTenant(ctx, "tenant-b", "alice@example.com"); return err }},
		{"id in other tenant", func() error { _, err := repo.GetByIDInTenant(ctx, "tenant-b", alice.ID); return err }},
		{"identifier with empty tenant", func() error { _, err := repo.GetByIdentifierInTenant(ctx, "", "alice@example.com"); return err }},
		{"id with empty tenant", func() error { _, err := repo.GetByIDInTenant(ctx, "", alice.ID); return err }},
		{"default-tenant user from tenant-a", func() error { _, err := repo.GetByIDInTenant(ctx, "tenant-a", legacy.ID); return err }},
	}
	for _, tc := range notFound {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrAuthUserNotFound) {
				t.Fatalf("err=%v want ErrAuthUserNotFound", err)
			}
		})
	}

	// Tenant-blind lookups keep working (goAuth's contract with tenancy off).
	if got, err := repo.GetByIdentifier(ctx, "alice@example.com"); err != nil || got.ID != alice.ID {
		t.Fatalf("tenant-blind lookup: got=%+v err=%v", got, err)
	}

	// Identifier uniqueness is global by default (users_email_unique_idx).
	if _, err := repo.Create(ctx, CreateStoredUserInput{TenantID: "tenant-b", Identifier: "alice@example.com", PasswordHash: "h", Status: "active"}); err == nil {
		t.Fatal("expected global unique violation for duplicate identifier across tenants")
	}
}

// Identifiers are case-insensitive in SQL: lookups match any case, the stored
// value is lower-cased, and a case-variant duplicate is "already exists".
func TestRelationalUserRepositoryIdentifiersAreCaseInsensitive(t *testing.T) {
	repo := NewRelationalUserRepository(dbtest.NewPostgres(t))
	ctx := context.Background()

	created, err := repo.Create(ctx, CreateStoredUserInput{TenantID: "tenant-a", Identifier: "  Alice@Example.COM ", PasswordHash: "h", Status: "active"})
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
		if got, err := repo.GetByIdentifierInTenant(ctx, "tenant-a", id); err != nil || got.ID != created.ID {
			t.Fatalf("GetByIdentifierInTenant(%q): got=%+v err=%v", id, got, err)
		}
		if _, err := repo.GetByIdentifierInTenant(ctx, "tenant-b", id); !errors.Is(err, ErrAuthUserNotFound) {
			t.Fatalf("GetByIdentifierInTenant(other tenant, %q) err=%v, want not found", id, err)
		}
	}

	for _, dup := range []string{"alice@example.com", "ALICE@example.com", "Alice@Example.Com"} {
		if _, err := repo.Create(ctx, CreateStoredUserInput{TenantID: "tenant-b", Identifier: dup, PasswordHash: "h", Status: "active"}); !errors.Is(err, ErrAuthUserExists) {
			t.Fatalf("Create(%q) err=%v, want ErrAuthUserExists", dup, err)
		}
	}
}
