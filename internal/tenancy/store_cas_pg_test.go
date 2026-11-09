package tenancy_test

import (
	"context"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
	"github.com/MrEthical07/superapi/internal/tenancy"
)

// The compare-and-swap writes keep the tenant predicate in SQL
// (db/queries/tenancy.sql). These tests run against a real Postgres
// (SUPERAPI_TEST_DATABASE_URL).
func TestUserStoreCompareAndSwapIsTenantScoped(t *testing.T) {
	pg := dbtest.NewPostgres(t)
	store := tenancy.NewUserStore(pg)
	ctx := context.Background()

	newUser := func(email, status string) tenancy.TenantUser {
		t.Helper()
		u, err := store.CreateInTenant(ctx, "tenant-a", auth.CreateStoredUserInput{Identifier: email, PasswordHash: "old", Role: "user", Status: status})
		if err != nil {
			t.Fatalf("create %s: %v", email, err)
		}
		return u
	}
	get := func(id string) tenancy.TenantUser {
		t.Helper()
		u, err := store.GetByIDInTenant(ctx, "tenant-a", id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		return u
	}

	t.Run("password hash", func(t *testing.T) {
		u := newUser("cas-pw@example.com", "active")

		for _, tc := range []struct {
			name                  string
			tenantID, userID, old string
		}{
			{"other tenant", "tenant-b", u.ID, "old"},
			{"empty tenant", "", u.ID, "old"},
			{"stale expected hash", "tenant-a", u.ID, "not-the-hash"},
			{"unknown user", "tenant-a", "00000000-0000-0000-0000-000000000000", "old"},
			{"malformed id", "tenant-a", "not-a-uuid", "old"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				applied, err := store.CompareAndSwapPasswordHashInTenant(ctx, tc.tenantID, tc.userID, tc.old, "forged")
				if err != nil || applied {
					t.Fatalf("applied=%v err=%v, want (false, nil)", applied, err)
				}
				if got := get(u.ID).PasswordHash; got != "old" {
					t.Fatalf("hash = %q after a rejected swap, want it untouched", got)
				}
			})
		}

		applied, err := store.CompareAndSwapPasswordHashInTenant(ctx, "tenant-a", u.ID, "old", "new")
		if err != nil || !applied {
			t.Fatalf("own-tenant swap: applied=%v err=%v", applied, err)
		}
		if got := get(u.ID).PasswordHash; got != "new" {
			t.Fatalf("hash = %q, want new", got)
		}
	})

	t.Run("account status", func(t *testing.T) {
		u := newUser("cas-status@example.com", "pending_verification")

		for _, tc := range []struct {
			name                      string
			tenantID, userID, current string
		}{
			{"other tenant", "tenant-b", u.ID, "pending_verification"},
			{"empty tenant", "", u.ID, "pending_verification"},
			{"stale expected status", "tenant-a", u.ID, "active"},
			{"unknown user", "tenant-a", "00000000-0000-0000-0000-000000000000", "pending_verification"},
			{"malformed id", "tenant-a", "not-a-uuid", "pending_verification"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				applied, err := store.UpdateStatusIfInTenant(ctx, tc.tenantID, tc.userID, tc.current, "active")
				if err != nil || applied {
					t.Fatalf("applied=%v err=%v, want (false, nil)", applied, err)
				}
				got := get(u.ID)
				if got.Status != "pending_verification" || got.AccountVersion != u.AccountVersion {
					t.Fatalf("after a rejected swap: status=%q version=%d, want pending_verification and %d", got.Status, got.AccountVersion, u.AccountVersion)
				}
			})
		}

		applied, err := store.UpdateStatusIfInTenant(ctx, "tenant-a", u.ID, "pending_verification", "active")
		if err != nil || !applied {
			t.Fatalf("own-tenant swap: applied=%v err=%v", applied, err)
		}
		if got := get(u.ID); got.Status != "active" || got.AccountVersion != u.AccountVersion+1 {
			t.Fatalf("status=%q version=%d, want active and %d", got.Status, got.AccountVersion, u.AccountVersion+1)
		}
	})
}
