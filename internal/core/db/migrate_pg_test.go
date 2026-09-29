package db_test

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

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

// The baseline (000001_init) is the whole core schema: the accounts table and
// its second-factor tables, with case-insensitive unique emails enforced by the
// database.
func TestBaselineSchema(t *testing.T) {
	databaseURL, sourceURL := dbtest.NewDatabase(t)
	migrateTo(t, databaseURL, sourceURL, 1)
	conn := connect(t, databaseURL)
	ctx := context.Background()

	columns := func(table string) []string {
		rows, err := conn.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1`, table)
		if err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				t.Fatalf("scan: %v", err)
			}
			out = append(out, c)
		}
		sort.Strings(out)
		return out
	}
	wantUsers := []string{"account_version", "created_at", "email", "id", "password_hash", "permissions", "role", "status", "totp_enabled", "updated_at"}
	if got := columns("users"); strings.Join(got, ",") != strings.Join(wantUsers, ",") {
		t.Fatalf("users columns = %v, want %v", got, wantUsers)
	}
	for _, table := range []string{"user_totp", "user_backup_codes",
		// template:begin webauthn
		"webauthn_credentials",
		// template:end webauthn
	} {
		if len(columns(table)) == 0 {
			t.Fatalf("table %s missing from the baseline", table)
		}
	}

	var indexes []string
	rows, err := conn.Query(ctx, `SELECT indexname FROM pg_indexes WHERE schemaname = 'public' AND tablename = 'users'`)
	if err != nil {
		t.Fatalf("indexes: %v", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		indexes = append(indexes, name)
	}
	rows.Close()
	sort.Strings(indexes)
	wantIndexes := []string{"users_created_at_idx", "users_email_lower_unique_idx", "users_pkey", "users_status_idx"}
	if strings.Join(indexes, ",") != strings.Join(wantIndexes, ",") {
		t.Fatalf("users indexes = %v, want %v", indexes, wantIndexes)
	}

	insert := func(email string) error {
		_, err := conn.Exec(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, 'h')`, email)
		return err
	}
	if err := insert("alice@example.com"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := insert("alice@example.com"); err == nil || !strings.Contains(err.Error(), "users_email_lower_unique_idx") {
		t.Fatalf("a duplicate email must violate users_email_lower_unique_idx, got: %v", err)
	}
	if err := insert("Bob@Example.com"); err == nil || !strings.Contains(err.Error(), "users_email_lowercase_check") {
		t.Fatalf("a mixed-case email must violate users_email_lowercase_check, got: %v", err)
	}

	// The unique constraint on backup codes holds per user.
	var userID string
	if err := conn.QueryRow(ctx, `SELECT id::text FROM users WHERE email = 'alice@example.com'`).Scan(&userID); err != nil {
		t.Fatalf("select user: %v", err)
	}
	hash := make([]byte, 32)
	if _, err := conn.Exec(ctx, `INSERT INTO user_backup_codes (user_id, code_hash) VALUES ($1, $2)`, userID, hash); err != nil {
		t.Fatalf("insert backup code: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO user_backup_codes (user_id, code_hash) VALUES ($1, $2)`, userID, hash); err == nil || !strings.Contains(err.Error(), "user_backup_codes_user_code_unique") {
		t.Fatalf("a duplicate (user_id, code_hash) must be rejected, got: %v", err)
	}
}

// The template ships the baseline and at most one optional second migration;
// projects add their own after their first deployment.
func TestShippedMigrationsAreTheBaseline(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", "..", "db", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) < 1 || len(matches) > 2 {
		t.Fatalf("db/migrations ships %d up migrations (%v); the template baseline is at most two", len(matches), matches)
	}
	if filepath.Base(matches[0]) != "000001_init.up.sql" {
		t.Fatalf("first migration = %s, want 000001_init.up.sql", matches[0])
	}
}
