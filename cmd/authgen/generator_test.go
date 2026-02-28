package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAuth_Default(t *testing.T) {
	root := setupTestWorkspace(t)
	cfg := DefaultConfig()

	if err := GenerateAuth(root, cfg, false); err != nil {
		t.Fatalf("GenerateAuth: %v", err)
	}

	// Verify migration files
	migrations := filepath.Join(root, "db", "migrations")
	entries, err := os.ReadDir(migrations)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	foundUp, foundDown := false, false
	for _, e := range entries {
		if strings.Contains(e.Name(), "auth_users") && strings.HasSuffix(e.Name(), ".up.sql") {
			foundUp = true
		}
		if strings.Contains(e.Name(), "auth_users") && strings.HasSuffix(e.Name(), ".down.sql") {
			foundDown = true
		}
	}
	if !foundUp || !foundDown {
		t.Fatal("expected both up and down migration files")
	}

	// Verify schema
	schemaPath := filepath.Join(root, "db", "schema", "auth_users.sql")
	if _, err := os.Stat(schemaPath); err != nil {
		t.Fatalf("schema file not found: %v", err)
	}

	// Verify queries
	queryPath := filepath.Join(root, "db", "queries", "auth_users.sql")
	if _, err := os.Stat(queryPath); err != nil {
		t.Fatalf("query file not found: %v", err)
	}

	// Verify provider
	providerPath := filepath.Join(root, "internal", "core", "auth", "provider_sqlc.go")
	if _, err := os.Stat(providerPath); err != nil {
		t.Fatalf("provider file not found: %v", err)
	}

	// Verify docs
	docsPath := filepath.Join(root, "docs", "auth-bootstrap.md")
	if _, err := os.Stat(docsPath); err != nil {
		t.Fatalf("docs file not found: %v", err)
	}

	// Verify config snapshot
	snapshotPath := filepath.Join(root, "authgen.yaml")
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatalf("config snapshot not found: %v", err)
	}

	// Verify goauth_provider.go was updated
	providerContent, err := os.ReadFile(filepath.Join(root, "internal", "core", "auth", "goauth_provider.go"))
	if err != nil {
		t.Fatalf("read goauth_provider: %v", err)
	}
	if !strings.Contains(string(providerContent), "userProvider goauth.UserProvider") {
		t.Error("goauth_provider.go should have updated function signature")
	}

	// Verify deps.go was updated
	depsContent, err := os.ReadFile(filepath.Join(root, "internal", "core", "app", "deps.go"))
	if err != nil {
		t.Fatalf("read deps.go: %v", err)
	}
	if !strings.Contains(string(depsContent), "NewSQLCUserProvider") {
		t.Error("deps.go should wire SQLCUserProvider")
	}
}

func TestGenerateAuth_RejectsExisting(t *testing.T) {
	root := setupTestWorkspace(t)
	cfg := DefaultConfig()

	// First run: should succeed
	if err := GenerateAuth(root, cfg, false); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run without force: should fail
	if err := GenerateAuth(root, cfg, false); err == nil {
		t.Fatal("expected error on second run without --force")
	}
}

func TestGenerateAuth_ForceOverwrite(t *testing.T) {
	root := setupTestWorkspace(t)
	cfg := DefaultConfig()

	// First run
	if err := GenerateAuth(root, cfg, false); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run with force: should succeed
	if err := GenerateAuth(root, cfg, true); err != nil {
		t.Fatalf("force run: %v", err)
	}
}

func TestGenerateAuth_WithTenant(t *testing.T) {
	root := setupTestWorkspace(t)
	cfg := DefaultConfig()
	cfg.TenantEnabled = true

	if err := GenerateAuth(root, cfg, false); err != nil {
		t.Fatalf("GenerateAuth: %v", err)
	}

	// Check migration contains tenant_id
	migrations := filepath.Join(root, "db", "migrations")
	entries, _ := os.ReadDir(migrations)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") && strings.Contains(e.Name(), "auth_users") {
			content, _ := os.ReadFile(filepath.Join(migrations, e.Name()))
			if !strings.Contains(string(content), "tenant_id") {
				t.Error("migration should contain tenant_id")
			}
		}
	}

	// Check queries contain tenant query
	queryContent, _ := os.ReadFile(filepath.Join(root, "db", "queries", "auth_users.sql"))
	if !strings.Contains(string(queryContent), "GetAuthUserByIDAndTenant") {
		t.Error("queries should contain tenant-aware query")
	}
}

func TestGenerateAuth_InvalidConfig(t *testing.T) {
	root := setupTestWorkspace(t)
	cfg := AuthGenConfig{
		TableName: "123invalid",
	}

	err := GenerateAuth(root, cfg, false)
	if err == nil {
		t.Fatal("expected validation error for invalid config")
	}
}

// setupTestWorkspace creates a minimal workspace with the files the generator needs to update.
func setupTestWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// go.mod
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/MrEthical07/superapi\n\ngo 1.26.0\n"), 0o644)

	// db directories
	os.MkdirAll(filepath.Join(root, "db", "migrations"), 0o755)
	os.MkdirAll(filepath.Join(root, "db", "schema"), 0o755)
	os.MkdirAll(filepath.Join(root, "db", "queries"), 0o755)

	// Add existing migrations to test numbering
	os.WriteFile(filepath.Join(root, "db", "migrations", "000001_system_settings.up.sql"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(root, "db", "migrations", "000001_system_settings.down.sql"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(root, "db", "migrations", "000002_tenants.up.sql"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(root, "db", "migrations", "000002_tenants.down.sql"), []byte(""), 0o644)

	// auth provider (simulated)
	authDir := filepath.Join(root, "internal", "core", "auth")
	os.MkdirAll(authDir, 0o755)
	goauthProvider := `package auth

import (
	"context"
	"errors"
	"fmt"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/redis/go-redis/v9"
)

type GoAuthProvider struct {
	validator goAuthValidator
}

func NewGoAuthEngineProvider(redisClient redis.UniversalClient, mode Mode) (Provider, func(), error) {
	if redisClient == nil {
		return nil, nil, fmt.Errorf("goAuth provider requires redis client")
	}

	cfg := goauth.DefaultConfig()

	engine, err := goauth.New().
		WithConfig(cfg).
		WithRedis(redisClient).
		WithPermissions([]string{"system.whoami"}).
		WithRoles(map[string][]string{
			"user":  {"system.whoami"},
			"admin": {"system.whoami"},
		}).
		WithUserProvider(noopUserProvider{}).
		Build()
	if err != nil {
		return nil, nil, fmt.Errorf("build goAuth engine: %w", err)
	}

	_ = engine
	_ = errors.New("")
	_ = context.Background()
	return nil, nil, nil
}

type noopUserProvider struct{}
func (noopUserProvider) GetUserByIdentifier(string) (goauth.UserRecord, error) { return goauth.UserRecord{}, nil }
func (noopUserProvider) GetUserByID(string) (goauth.UserRecord, error) { return goauth.UserRecord{}, nil }
func (noopUserProvider) UpdatePasswordHash(string, string) error { return nil }
func (noopUserProvider) CreateUser(context.Context, goauth.CreateUserInput) (goauth.UserRecord, error) { return goauth.UserRecord{}, nil }
func (noopUserProvider) UpdateAccountStatus(context.Context, string, goauth.AccountStatus) (goauth.UserRecord, error) { return goauth.UserRecord{}, nil }
func (noopUserProvider) GetTOTPSecret(context.Context, string) (*goauth.TOTPRecord, error) { return nil, nil }
func (noopUserProvider) EnableTOTP(context.Context, string, []byte) error { return nil }
func (noopUserProvider) DisableTOTP(context.Context, string) error { return nil }
func (noopUserProvider) MarkTOTPVerified(context.Context, string) error { return nil }
func (noopUserProvider) UpdateTOTPLastUsedCounter(context.Context, string, int64) error { return nil }
func (noopUserProvider) GetBackupCodes(context.Context, string) ([]goauth.BackupCodeRecord, error) { return nil, nil }
func (noopUserProvider) ReplaceBackupCodes(context.Context, string, []goauth.BackupCodeRecord) error { return nil }
func (noopUserProvider) ConsumeBackupCode(context.Context, string, [32]byte) (bool, error) { return false, nil }
`
	os.WriteFile(filepath.Join(authDir, "goauth_provider.go"), []byte(goauthProvider), 0o644)

	// deps.go (simulated)
	depsDir := filepath.Join(root, "internal", "core", "app")
	os.MkdirAll(depsDir, 0o755)
	depsFile := `package app

import (
	"context"
	"fmt"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

func initDependencies(ctx context.Context) error {
	_ = fmt.Sprintf("")
	provider, closeFn, err := auth.NewGoAuthEngineProvider(deps.Redis, authMode)
	if err != nil {
		return err
	}
	_ = provider
	_ = closeFn
	return nil
}
`
	os.WriteFile(filepath.Join(depsDir, "deps.go"), []byte(depsFile), 0o644)

	// docs dir
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)

	return root
}
