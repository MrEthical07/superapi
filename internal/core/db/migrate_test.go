package db

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

func TestMigrationSourceURL(t *testing.T) {
	u, err := MigrationSourceURL("db/migrations")
	if err != nil {
		t.Fatalf("MigrationSourceURL() error = %v", err)
	}
	if !strings.HasPrefix(u, "file://") {
		t.Fatalf("url = %q, want file:// prefix", u)
	}
	if runtime.GOOS == "windows" && !strings.Contains(u, ":/") {
		t.Fatalf("windows url = %q, expected drive path", u)
	}
}

func TestMigrationSourceURLEmptyPath(t *testing.T) {
	_, err := MigrationSourceURL("")
	if err == nil {
		t.Fatalf("expected error for empty path")
	}
}

type migrateOpsStub struct {
	upErr    error
	downErr  error
	ver      uint
	dirty    bool
	verErr   error
	forceErr error
}

func (s *migrateOpsStub) Up() error                    { return s.upErr }
func (s *migrateOpsStub) Steps(int) error              { return s.downErr }
func (s *migrateOpsStub) Version() (uint, bool, error) { return s.ver, s.dirty, s.verErr }
func (s *migrateOpsStub) Force(int) error              { return s.forceErr }

func TestNoChangeNormalization(t *testing.T) {
	r := &migrationRunnerForTest{ops: &migrateOpsStub{upErr: migrate.ErrNoChange}}
	noChange, err := r.Up()
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if !noChange {
		t.Fatalf("expected noChange=true")
	}
}

func TestVersionNilVersion(t *testing.T) {
	r := &migrationRunnerForTest{ops: &migrateOpsStub{verErr: migrate.ErrNilVersion}}
	v, err := r.Version()
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if v.HasVersion {
		t.Fatalf("expected HasVersion=false")
	}
}

func TestForceNegativeVersion(t *testing.T) {
	r := &migrationRunnerForTest{ops: &migrateOpsStub{}}
	err := r.Force(-1)
	if err == nil {
		t.Fatalf("expected error for negative version")
	}
}

func TestDownInvalidSteps(t *testing.T) {
	r := &migrationRunnerForTest{ops: &migrateOpsStub{}}
	_, err := r.Down(0)
	if err == nil {
		t.Fatalf("expected error for invalid steps")
	}
}

func TestWrappedError(t *testing.T) {
	root := errors.New("boom")
	r := &migrationRunnerForTest{ops: &migrateOpsStub{upErr: root}}
	_, err := r.Up()
	if !errors.Is(err, root) {
		t.Fatalf("expected wrapped root error, got %v", err)
	}
}

type migrateOps interface {
	Up() error
	Steps(int) error
	Version() (uint, bool, error)
	Force(int) error
}

type migrationRunnerForTest struct {
	ops migrateOps
}

func (r *migrationRunnerForTest) Up() (bool, error) {
	err := r.ops.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func (r *migrationRunnerForTest) Down(steps int) (bool, error) {
	if steps <= 0 {
		return false, errors.New("invalid steps")
	}
	err := r.ops.Steps(-steps)
	if errors.Is(err, migrate.ErrNoChange) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func (r *migrationRunnerForTest) Version() (MigrationVersion, error) {
	v, dirty, err := r.ops.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return MigrationVersion{HasVersion: false}, nil
	}
	if err != nil {
		return MigrationVersion{}, err
	}
	return MigrationVersion{Version: v, Dirty: dirty, HasVersion: true}, nil
}

func (r *migrationRunnerForTest) Force(version int) error {
	if version < 0 {
		return errors.New("negative version")
	}
	return r.ops.Force(version)
}
