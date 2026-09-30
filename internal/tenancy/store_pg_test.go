package tenancy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
	"github.com/MrEthical07/superapi/internal/tenancy"
)

// The tenant predicate lives in SQL (db/queries/tenancy.sql). These tests run
// against a real Postgres (SUPERAPI_TEST_DATABASE_URL).
func TestUserStoreTenantScoping(t *testing.T) {
	pg := dbtest.NewPostgres(t)
	store := tenancy.NewUserStore(pg)
	ctx := context.Background()

	alice, err := store.CreateInTenant(ctx, "tenant-a", auth.CreateStoredUserInput{Identifier: "Alice@Example.com", PasswordHash: "h", Role: "user", Status: "active"})
	if err != nil || alice.TenantID != "tenant-a" || alice.Email != "alice@example.com" {
		t.Fatalf("create: %+v err=%v", alice, err)
	}

	// Same-tenant lookups match; identifiers are case-insensitive.
	if got, err := store.GetByIDInTenant(ctx, "tenant-a", alice.ID); err != nil || got.ID != alice.ID || got.TenantID != "tenant-a" {
		t.Fatalf("id in tenant: %+v err=%v", got, err)
	}
	if got, err := store.GetByIdentifierInTenant(ctx, "tenant-a", "ALICE@example.com"); err != nil || got.ID != alice.ID {
		t.Fatalf("identifier in tenant: %+v err=%v", got, err)
	}

	// A record that exists only in another tenant, or under an empty tenant, is
	// not found.
	notFound := []struct {
		name string
		call func() error
	}{
		{"id, other tenant", func() error { _, err := store.GetByIDInTenant(ctx, "tenant-b", alice.ID); return err }},
		{"id, empty tenant", func() error { _, err := store.GetByIDInTenant(ctx, "", alice.ID); return err }},
		{"identifier, other tenant", func() error {
			_, err := store.GetByIdentifierInTenant(ctx, "tenant-b", "alice@example.com")
			return err
		}},
		{"identifier, empty tenant", func() error { _, err := store.GetByIdentifierInTenant(ctx, "", "alice@example.com"); return err }},
		{"malformed id", func() error { _, err := store.GetByIDInTenant(ctx, "tenant-a", "not-a-uuid"); return err }},
	}
	for _, tc := range notFound {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, auth.ErrAuthUserNotFound) {
				t.Fatalf("err=%v want ErrAuthUserNotFound", err)
			}
		})
	}

	// TenantOf.
	if tenantID, err := store.TenantOf(ctx, alice.ID); err != nil || tenantID != "tenant-a" {
		t.Fatalf("TenantOf = %q, %v", tenantID, err)
	}
	if _, err := store.TenantOf(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, auth.ErrAuthUserNotFound) {
		t.Fatalf("TenantOf unknown user err=%v", err)
	}

	// Identifier uniqueness is global across tenants (the users_email_lower
	// unique index); tenant-scoped uniqueness is a schema decision, see
	// docs/multi-tenancy.md.
	if _, err := store.CreateInTenant(ctx, "tenant-b", auth.CreateStoredUserInput{Identifier: "alice@example.com", PasswordHash: "h", Status: "active"}); !errors.Is(err, auth.ErrAuthUserExists) {
		t.Fatalf("duplicate across tenants err=%v, want ErrAuthUserExists", err)
	}

	// Tenant-blind lookups report the tenant; users created through the core
	// repository (no tenant) land in goAuth's default tenant via the column default.
	core := auth.NewRelationalUserRepository(pg)
	plain, err := core.Create(ctx, auth.CreateStoredUserInput{Identifier: "plain@example.com", PasswordHash: "h", Status: "active"})
	if err != nil {
		t.Fatalf("core create: %v", err)
	}
	if got, err := store.GetByID(ctx, plain.ID); err != nil || got.TenantID != tenancy.DefaultTenantID {
		t.Fatalf("core-created user tenant = %+v err=%v, want %q", got, err, tenancy.DefaultTenantID)
	}
	if got, err := store.GetByIdentifier(ctx, "alice@example.com"); err != nil || got.TenantID != "tenant-a" {
		t.Fatalf("tenant-blind identifier lookup: %+v err=%v", got, err)
	}
}
