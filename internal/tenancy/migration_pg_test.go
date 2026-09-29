package tenancy_test

import (
	"context"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"

	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
)

// 000002_tenancy adds the tenants table and users.tenant_id on top of the
// baseline, and its down migration removes exactly that.
func TestTenancyMigrationUpAndDown(t *testing.T) {
	databaseURL, sourceURL := dbtest.NewDatabase(t)
	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })

	conn, err := pgx.Connect(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	ctx := context.Background()

	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}
	hasColumn := func() bool {
		return count(`SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'users' AND column_name = 'tenant_id'`) == 1
	}
	hasTable := func() bool {
		return count(`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'tenants'`) == 1
	}
	hasIndex := func() bool {
		return count(`SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'users_tenant_email_lower_idx'`) == 1
	}

	if err := m.Migrate(1); err != nil {
		t.Fatalf("migrate to 1: %v", err)
	}
	if hasColumn() || hasTable() || hasIndex() {
		t.Fatal("the baseline must not contain tenancy")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO users (email, password_hash) VALUES ('early@example.com', 'h')`); err != nil {
		t.Fatalf("insert before tenancy: %v", err)
	}

	if err := m.Migrate(2); err != nil {
		t.Fatalf("migrate to 2: %v", err)
	}
	if !hasColumn() || !hasTable() || !hasIndex() {
		t.Fatal("000002_tenancy must add the tenants table, users.tenant_id and its index")
	}
	// Existing rows land in goAuth's default tenant, and the column defaults to it.
	if got := count(`SELECT count(*) FROM users WHERE email = 'early@example.com' AND tenant_id = '0'`); got != 1 {
		t.Fatal("existing users must be backfilled into the default tenant")
	}
	// Uniqueness stays global across tenants.
	if _, err := conn.Exec(ctx, `INSERT INTO users (email, password_hash, tenant_id) VALUES ('early@example.com', 'h', 'other')`); err == nil || !strings.Contains(err.Error(), "users_email_lower_unique_idx") {
		t.Fatalf("email uniqueness must stay global, got: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO tenants (id, slug, name, status) VALUES ('acme', 'acme', 'Acme', 'bogus')`); err == nil {
		t.Fatal("tenants.status must be constrained to active/inactive")
	}

	if err := m.Migrate(1); err != nil {
		t.Fatalf("down to 1: %v", err)
	}
	if hasColumn() || hasTable() || hasIndex() {
		t.Fatal("the down migration must remove the tenants table, the column and the index")
	}
	if got := count(`SELECT count(*) FROM users WHERE email = 'early@example.com'`); got != 1 {
		t.Fatal("the down migration must keep the users")
	}
}
