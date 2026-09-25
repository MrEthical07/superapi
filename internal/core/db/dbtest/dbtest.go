// Package dbtest provides a throwaway, fully migrated Postgres database for
// integration tests.
//
// Tests using it are skipped unless SUPERAPI_TEST_DATABASE_URL points at a
// Postgres server whose role may CREATE DATABASE, for example:
//
//	SUPERAPI_TEST_DATABASE_URL=postgres://superapi:superapi@127.0.0.1:5432/superapi?sslmode=disable go test ./...
//
// `make dev-up` starts a suitable server; CI sets the variable against its
// Postgres service container.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

// EnvDatabaseURL names the env var holding the admin connection URL.
const EnvDatabaseURL = "SUPERAPI_TEST_DATABASE_URL"

// NewPostgres creates a fresh database, applies every migration under
// db/migrations, and returns the storage boundary over it. The database is
// dropped when the test finishes.
func NewPostgres(t testing.TB) *storage.Postgres {
	t.Helper()

	adminURL := strings.TrimSpace(os.Getenv(EnvDatabaseURL))
	if adminURL == "" {
		t.Skipf("%s not set; skipping Postgres integration test", EnvDatabaseURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin database: %v", err)
	}
	defer admin.Close(context.Background())

	name := "superapi_test_" + randomSuffix(t)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()
		conn, err := pgx.Connect(dropCtx, adminURL)
		if err != nil {
			t.Logf("drop test database: connect: %v", err)
			return
		}
		defer conn.Close(context.Background())
		if _, err := conn.Exec(dropCtx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database: %v", err)
		}
	})

	testURL, err := withDatabase(adminURL, name)
	if err != nil {
		t.Fatalf("build test database url: %v", err)
	}

	sourceURL, err := db.MigrationSourceURL(migrationsDir(t))
	if err != nil {
		t.Fatalf("migration source: %v", err)
	}
	runner, err := db.NewMigrationRunner(testURL, sourceURL)
	if err != nil {
		t.Fatalf("migration runner: %v", err)
	}
	if _, err := runner.Up(); err != nil {
		_ = runner.Close()
		t.Fatalf("migrate up: %v", err)
	}
	_ = runner.Close()

	pool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)

	pg, err := storage.NewPostgres(pool)
	if err != nil {
		t.Fatalf("storage boundary: %v", err)
	}
	return pg
}

func withDatabase(rawURL, name string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

func randomSuffix(t testing.TB) string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("random suffix: %v", err)
	}
	return hex.EncodeToString(b[:])
}

// migrationsDir locates db/migrations relative to this source file so tests
// work from any package directory.
func migrationsDir(t testing.TB) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate dbtest source file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "db", "migrations")
}
