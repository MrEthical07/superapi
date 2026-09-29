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

// Migration 000008 collapses duplicate backup codes (keeping an unused row when
// there is one), then enforces uniqueness per user.
func TestMigration000008BackupCodesUnique(t *testing.T) {
	databaseURL, sourceURL := dbtest.NewDatabase(t)
	m := migrateTo(t, databaseURL, sourceURL, 7)
	conn := connect(t, databaseURL)
	ctx := context.Background()

	var userA, userB string
	for email, dst := range map[string]*string{"a@example.com": &userA, "b@example.com": &userB} {
		if err := conn.QueryRow(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, 'h') RETURNING id::text`, email).Scan(dst); err != nil {
			t.Fatalf("insert user: %v", err)
		}
	}
	hash := func(b byte) []byte {
		h := make([]byte, 32)
		for i := range h {
			h[i] = b
		}
		return h
	}
	insert := func(user string, code byte, used bool) {
		t.Helper()
		usedAt := "NULL"
		if used {
			usedAt = "NOW()"
		}
		if _, err := conn.Exec(ctx, `INSERT INTO user_backup_codes (user_id, code_hash, used_at) VALUES ($1, $2, `+usedAt+`)`, user, hash(code)); err != nil {
			t.Fatalf("insert code: %v", err)
		}
	}
	// Before 000008 duplicates are allowed:
	insert(userA, 1, true)  // duplicate of the unused row below; the used row has the lower id
	insert(userA, 1, false) // must be the survivor
	insert(userA, 1, true)
	insert(userA, 2, true) // all copies used: one of them survives
	insert(userA, 2, true)
	insert(userA, 3, false) // no duplicate: untouched
	insert(userB, 1, false) // same hash as user A's, different user: untouched
	insert(userB, 1, false) // duplicate for B: two unused, one survives

	if err := m.Migrate(8); err != nil {
		t.Fatalf("migrate to 8: %v", err)
	}

	type row struct {
		user string
		code byte
		used bool
	}
	rows, err := conn.Query(ctx, `SELECT user_id::text, get_byte(code_hash, 0), used_at IS NOT NULL FROM user_backup_codes ORDER BY user_id, get_byte(code_hash, 0), id`)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	var got []row
	for rows.Next() {
		var r row
		var code int
		if err := rows.Scan(&r.user, &code, &r.used); err != nil {
			t.Fatalf("scan: %v", err)
		}
		r.code = byte(code)
		got = append(got, r)
	}
	rows.Close()

	count := func(user string, code byte) (n int, anyUsed bool) {
		for _, r := range got {
			if r.user == user && r.code == code {
				n++
				anyUsed = anyUsed || r.used
			}
		}
		return n, anyUsed
	}
	if n, used := count(userA, 1); n != 1 || used {
		t.Fatalf("user A code 1: %d rows, used=%v; want one unused survivor", n, used)
	}
	if n, used := count(userA, 2); n != 1 || !used {
		t.Fatalf("user A code 2: %d rows, used=%v; want one (used) survivor", n, used)
	}
	if n, _ := count(userA, 3); n != 1 {
		t.Fatalf("user A code 3: %d rows, want it untouched", n)
	}
	if n, _ := count(userB, 1); n != 1 {
		t.Fatalf("user B code 1: %d rows, want the duplicate collapsed to one", n)
	}
	if len(got) != 4 {
		t.Fatalf("%d rows remain, want 4: %+v", len(got), got)
	}

	// The constraint now holds, per user.
	if _, err := conn.Exec(ctx, `INSERT INTO user_backup_codes (user_id, code_hash) VALUES ($1, $2)`, userA, hash(3)); err == nil {
		t.Fatal("a duplicate (user_id, code_hash) must be rejected")
	} else if !strings.Contains(err.Error(), "user_backup_codes_user_code_unique") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO user_backup_codes (user_id, code_hash) VALUES ($1, $2)`, userB, hash(3)); err != nil {
		t.Fatalf("the same hash for another user is allowed: %v", err)
	}

	// Down restores the old shape (duplicates allowed again).
	if err := m.Migrate(7); err != nil {
		t.Fatalf("down to 7: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO user_backup_codes (user_id, code_hash) VALUES ($1, $2)`, userA, hash(3)); err != nil {
		t.Fatalf("duplicates are allowed again after down: %v", err)
	}
}
