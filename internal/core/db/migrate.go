package db

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// MigrationVersion describes the current migration state.
type MigrationVersion struct {
	// Version is the applied migration version when HasVersion is true.
	Version uint
	// Dirty indicates the database is marked dirty by migrate.
	Dirty bool
	// HasVersion reports whether any migration version is set.
	HasVersion bool
}

// MigrationRunner wraps golang-migrate operations for CLI and app tooling.
type MigrationRunner struct {
	m *migrate.Migrate
}

// NewMigrationRunner creates a migrate runner for a source and database URL.
func NewMigrationRunner(databaseURL, sourceURL string) (*MigrationRunner, error) {
	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("init migrate runner: %w", err)
	}
	return &MigrationRunner{m: m}, nil
}

// Close closes migrate source and database handles.
func (r *MigrationRunner) Close() error {
	if r == nil || r.m == nil {
		return nil
	}
	srcErr, dbErr := r.m.Close()
	if srcErr == nil && dbErr == nil {
		return nil
	}
	return errors.Join(srcErr, dbErr)
}

// Up applies all pending migrations.
func (r *MigrationRunner) Up() (bool, error) {
	err := r.m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("migrate up: %w", err)
	}
	return false, nil
}

// Down rolls back the requested number of migration steps.
func (r *MigrationRunner) Down(steps int) (bool, error) {
	if steps <= 0 {
		return false, fmt.Errorf("steps must be > 0")
	}
	err := r.m.Steps(-steps)
	if errors.Is(err, migrate.ErrNoChange) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("migrate down: %w", err)
	}
	return false, nil
}

// Version returns current migration version and dirty state.
func (r *MigrationRunner) Version() (MigrationVersion, error) {
	v, dirty, err := r.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return MigrationVersion{HasVersion: false}, nil
	}
	if err != nil {
		return MigrationVersion{}, fmt.Errorf("migrate version: %w", err)
	}
	return MigrationVersion{Version: v, Dirty: dirty, HasVersion: true}, nil
}

// Force sets migrate state to a specific version.
func (r *MigrationRunner) Force(version int) error {
	if version < 0 {
		return fmt.Errorf("version must be >= 0")
	}
	if err := r.m.Force(version); err != nil {
		return fmt.Errorf("migrate force: %w", err)
	}
	return nil
}

// MigrationSourceURL converts a filesystem path to a migrate file:// URL.
func MigrationSourceURL(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("migration path cannot be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve migration path: %w", err)
	}
	abs = filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	u := url.URL{Scheme: "file", Path: abs}
	return u.String(), nil
}
