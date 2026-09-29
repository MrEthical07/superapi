package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"

	"github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/db/dbtest"
)

// migrateTo runs migrations up to version on a fresh empty database.
func migrateTo(t *testing.T, databaseURL, sourceURL string, version uint) *migrate.Migrate {
	t.Helper()
	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })
	if err := m.Migrate(version); err != nil {
		t.Fatalf("migrate to %d: %v", version, err)
	}
	return m
}

func connect(t *testing.T, databaseURL string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func TestMigrationsUpDownUp(t *testing.T) {
	databaseURL, sourceURL := dbtest.NewDatabase(t)
	runner, err := db.NewMigrationRunner(databaseURL, sourceURL)
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	defer runner.Close()

	if _, err := runner.Up(); err != nil {
		t.Fatalf("first up: %v", err)
	}
	head, err := runner.Version()
	if err != nil || !head.HasVersion || head.Dirty {
		t.Fatalf("version after up: %+v err=%v", head, err)
	}
	// Step down one migration at a time until nothing is applied. (Version
	// numbers can have gaps in a pruned project, so head.Version is not the
	// number of steps.)
	for i := 0; ; i++ {
		v, err := runner.Version()
		if err != nil {
			t.Fatalf("version while stepping down: %v", err)
		}
		if !v.HasVersion {
			break
		}
		if i >= 100 {
			t.Fatal("still migrated after 100 down steps")
		}
		if _, err := runner.Down(1); err != nil {
			t.Fatalf("down from version %d: %v", v.Version, err)
		}
	}
	if _, err := runner.Up(); err != nil {
		t.Fatalf("second up: %v", err)
	}
	again, err := runner.Version()
	if err != nil || again.Version != head.Version || again.Dirty {
		t.Fatalf("version after second up: %+v err=%v, want %d", again, err, head.Version)
	}
}

// Migration 000007 must refuse to run while two accounts differ only by email
// case, name them, and change nothing. Once they are resolved it lower-cases
// the stored emails and enforces case-insensitive uniqueness.
func TestMigration000007EmailCaseInsensitive(t *testing.T) {
	databaseURL, sourceURL := dbtest.NewDatabase(t)
	m := migrateTo(t, databaseURL, sourceURL, 6)
	conn := connect(t, databaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	insert := func(email, tenant string) string {
		var id string
		if err := conn.QueryRow(ctx,
			`INSERT INTO users (email, password_hash, tenant_id) VALUES ($1, 'h', $2) RETURNING id::text`, email, tenant).Scan(&id); err != nil {
			t.Fatalf("insert %q: %v", email, err)
		}
		return id
	}
	insert("Alice@Example.com", "0")
	dupID := insert("alice@example.com", "0")
	insert("Bob@Example.com", "0")

	err := m.Migrate(7)
	if err == nil {
		t.Fatal("migration 7 must fail while case-insensitive duplicates exist")
	}
	for _, want := range []string{"differ only by email case", "alice@example.com", "Alice@Example.com", dupID} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should mention %q, got: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "bob@example.com") {
		t.Fatalf("a non-duplicate account must not be listed: %v", err)
	}

	// Nothing was merged, deleted or rewritten.
	var total int
	var mixed int
	if err := conn.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE email <> lower(email)) FROM users`).Scan(&total, &mixed); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 3 || mixed != 2 {
		t.Fatalf("failed migration changed data: total=%d mixed-case=%d, want 3 and 2", total, mixed)
	}

	// Resolve the duplicate by hand, mark the version clean, and re-run: the
	// steps an operator follows from the migration's error text.
	if _, err := conn.Exec(ctx, `DELETE FROM users WHERE id = $1`, dupID); err != nil {
		t.Fatalf("resolve duplicate: %v", err)
	}
	if err := m.Force(6); err != nil {
		t.Fatalf("force 6: %v", err)
	}
	if err := m.Migrate(7); err != nil {
		t.Fatalf("migrate to 7 after resolving duplicates: %v", err)
	}

	var stored []string
	rows, err := conn.Query(ctx, `SELECT email FROM users ORDER BY email`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			t.Fatalf("scan: %v", err)
		}
		stored = append(stored, e)
	}
	rows.Close()
	if strings.Join(stored, ",") != "alice@example.com,bob@example.com" {
		t.Fatalf("stored emails = %v, want lower-cased", stored)
	}

	// Uniqueness is now case-insensitive and global (across tenants).
	if _, err := conn.Exec(ctx, `INSERT INTO users (email, password_hash, tenant_id) VALUES ('ALICE@example.com', 'h', 'other')`); err == nil {
		t.Fatal("a case-variant duplicate must violate users_email_lower_unique_idx")
	} else if !strings.Contains(err.Error(), "users_email_lower_unique_idx") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A clean database migrates straight through, and 000007 can be reverted.
func TestMigration000007DownRestoresIndexes(t *testing.T) {
	databaseURL, sourceURL := dbtest.NewDatabase(t)
	m := migrateTo(t, databaseURL, sourceURL, 7)
	conn := connect(t, databaseURL)
	ctx := context.Background()

	indexExists := func(name string) bool {
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`, name).Scan(&n); err != nil {
			t.Fatalf("pg_indexes: %v", err)
		}
		return n == 1
	}
	if !indexExists("users_email_lower_unique_idx") || !indexExists("users_tenant_email_lower_idx") || indexExists("users_tenant_email_idx") {
		t.Fatal("unexpected indexes after 000007 up")
	}
	if err := m.Migrate(6); err != nil {
		t.Fatalf("down to 6: %v", err)
	}
	if indexExists("users_email_lower_unique_idx") || indexExists("users_tenant_email_lower_idx") || !indexExists("users_tenant_email_idx") {
		t.Fatal("unexpected indexes after 000007 down")
	}
}
