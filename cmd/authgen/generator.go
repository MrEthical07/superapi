package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GenerateAuth runs the full auth bootstrap generation process.
func GenerateAuth(workspaceRoot string, cfg AuthGenConfig, force bool) error {
	if err := cfg.Validate(); err != nil {
		return err
	}

	// --- Step 0: Check for existing auth bootstrap ---
	if err := checkExistingBootstrap(workspaceRoot, force); err != nil {
		return err
	}

	// --- Step 1: Generate migration files ---
	if err := generateMigrations(workspaceRoot, cfg); err != nil {
		return fmt.Errorf("generate migrations: %w", err)
	}

	// --- Step 2: Generate schema mirror ---
	if err := generateSchema(workspaceRoot, cfg); err != nil {
		return fmt.Errorf("generate schema: %w", err)
	}

	// --- Step 3: Generate sqlc query file ---
	if err := generateQueries(workspaceRoot, cfg); err != nil {
		return fmt.Errorf("generate queries: %w", err)
	}

	// --- Step 3b: Run sqlc generate (best-effort; warn if unavailable) ---
	if err := runSQLCGenerate(workspaceRoot); err != nil {
		fmt.Printf("  warning: %v\n", err)
		fmt.Println("  Please run 'sqlc generate' manually before building.")
	}

	// NOTE: authgen scaffolds the auth data layer (migration, schema, queries)
	// only. It intentionally does NOT generate a UserProvider or rewrite
	// deps.go / goauth_provider.go: the template already ships a working,
	// sqlc-backed StoreUserProvider (internal/core/auth) that is wired
	// automatically when AUTH_ENABLED=true. Generating a second, parallel
	// provider was the old data-layer duality and has been removed. Customize
	// auth behavior in internal/core/auth/config.go and roles.go.

	// --- Step 4: Generate documentation ---
	if err := generateDocs(workspaceRoot, cfg); err != nil {
		return fmt.Errorf("generate docs: %w", err)
	}

	// --- Step 5: Save config snapshot ---
	if err := saveConfigSnapshot(workspaceRoot, cfg); err != nil {
		return fmt.Errorf("save config snapshot: %w", err)
	}

	return nil
}

func checkExistingBootstrap(workspaceRoot string, force bool) error {
	queryPath := filepath.Join(workspaceRoot, "db", "queries", "auth_users.sql")
	schemaPath := filepath.Join(workspaceRoot, "db", "schema", "auth_users.sql")

	existing := []string{}
	for _, p := range []string{queryPath, schemaPath} {
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}

	if len(existing) > 0 {
		if !force {
			return fmt.Errorf("auth bootstrap already exists (found: %s)\nRe-run with --force to overwrite",
				strings.Join(existing, ", "))
		}
		fmt.Println("Warning: Overwriting existing auth bootstrap files (--force)")
	}
	return nil
}

func generateMigrations(workspaceRoot string, cfg AuthGenConfig) error {
	migrationsDir := filepath.Join(workspaceRoot, "db", "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		return err
	}

	// Check if auth migration files already exist (idempotent --force).
	existingNum := findExistingAuthMigration(migrationsDir, cfg.TableName)

	var migNum int
	if existingNum > 0 {
		// Reuse existing migration number; delete old files first.
		migNum = existingNum
		oldPrefix := fmt.Sprintf("%06d_auth_%s", migNum, cfg.TableName)
		_ = os.Remove(filepath.Join(migrationsDir, oldPrefix+".up.sql"))
		_ = os.Remove(filepath.Join(migrationsDir, oldPrefix+".down.sql"))
	} else {
		var err error
		migNum, err = NextMigrationNumber(migrationsDir)
		if err != nil {
			return err
		}
	}

	prefix := fmt.Sprintf("%06d_auth_%s", migNum, cfg.TableName)
	upPath := filepath.Join(migrationsDir, prefix+".up.sql")
	downPath := filepath.Join(migrationsDir, prefix+".down.sql")

	upContent := renderMigrationUp(cfg)
	downContent := renderMigrationDown(cfg)

	if err := os.WriteFile(upPath, []byte(upContent), 0o644); err != nil {
		return fmt.Errorf("write up migration: %w", err)
	}
	if err := os.WriteFile(downPath, []byte(downContent), 0o644); err != nil {
		return fmt.Errorf("write down migration: %w", err)
	}

	fmt.Printf("  created: %s\n", upPath)
	fmt.Printf("  created: %s\n", downPath)
	return nil
}

// findExistingAuthMigration looks for an existing auth migration with the
// pattern NNNNNN_auth_<table>.up.sql. Returns the migration number or 0.
func findExistingAuthMigration(migrationsDir, tableName string) int {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return 0
	}
	suffix := fmt.Sprintf("_auth_%s.up.sql", tableName)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			numStr := strings.SplitN(e.Name(), "_", 2)[0]
			var n int
			if _, err := fmt.Sscanf(numStr, "%d", &n); err == nil {
				return n
			}
		}
	}
	return 0
}

func generateSchema(workspaceRoot string, cfg AuthGenConfig) error {
	schemaDir := filepath.Join(workspaceRoot, "db", "schema")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		return err
	}

	schemaPath := filepath.Join(schemaDir, "auth_users.sql")
	content := renderSchema(cfg)

	if err := os.WriteFile(schemaPath, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Printf("  created: %s\n", schemaPath)
	return nil
}

func generateQueries(workspaceRoot string, cfg AuthGenConfig) error {
	queriesDir := filepath.Join(workspaceRoot, "db", "queries")
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		return err
	}

	queryPath := filepath.Join(queriesDir, "auth_users.sql")
	content := renderQueries(cfg)

	if err := os.WriteFile(queryPath, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Printf("  created: %s\n", queryPath)
	return nil
}

func runSQLCGenerate(workspaceRoot string) error {
	fmt.Println("  running: sqlc generate")

	// Try sqlc directly first, then fall back to go run.
	cmd := exec.Command("sqlc", "generate")
	cmd.Dir = workspaceRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Fall back to go run
		fmt.Println("  sqlc not found on PATH, trying go run...")
		cmd = exec.Command("go", "run", "github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0", "generate")
		cmd.Dir = workspaceRoot
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("sqlc generate failed: %w\n\nPlease run 'sqlc generate' manually, then 'go build ./...'", err)
		}
	}
	return nil
}

func generateDocs(workspaceRoot string, cfg AuthGenConfig) error {
	docsDir := filepath.Join(workspaceRoot, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return err
	}

	docPath := filepath.Join(docsDir, "auth-bootstrap.md")

	var b strings.Builder
	b.WriteString("# Auth Bootstrap Reference\n\n")
	b.WriteString("Generated by `make auth` / `go run ./cmd/authgen`.\n\n")
	b.WriteString("## Configuration Used\n\n")
	b.WriteString("```\n")
	b.WriteString(cfg.Summary())
	b.WriteString("```\n\n")

	b.WriteString("## Generated Files\n\n")
	b.WriteString("| File | Purpose |\n")
	b.WriteString("|---|---|\n")
	b.WriteString("| `db/migrations/NNNNNN_auth_" + cfg.TableName + ".up.sql` | Users table migration (up) |\n")
	b.WriteString("| `db/migrations/NNNNNN_auth_" + cfg.TableName + ".down.sql` | Users table migration (down) |\n")
	b.WriteString("| `db/schema/auth_users.sql` | Schema mirror for sqlc |\n")
	b.WriteString("| `db/queries/auth_users.sql` | sqlc query definitions |\n")
	b.WriteString("\n")

	b.WriteString("## Provider Wiring (no generation needed)\n\n")
	b.WriteString("This template already ships a working, sqlc-backed goAuth user provider\n")
	b.WriteString("(`StoreUserProvider` in `internal/core/auth`), wired automatically in\n")
	b.WriteString("`internal/core/app/deps.go` when `AUTH_ENABLED=true`. authgen does not\n")
	b.WriteString("generate a second provider or edit wiring files — it only scaffolds the\n")
	b.WriteString("auth data layer above. Customize auth behavior in\n")
	b.WriteString("`internal/core/auth/config.go` and roles in `internal/core/auth/roles.go`.\n")
	b.WriteString("\n")

	b.WriteString("## Schema\n\n")
	b.WriteString("```sql\n")
	b.WriteString(renderMigrationUp(cfg))
	b.WriteString("```\n\n")

	b.WriteString("## After Generation\n\n")
	b.WriteString("1. Run sqlc to generate typed Go code:\n")
	b.WriteString("   ```\n   sqlc generate\n   ```\n\n")
	b.WriteString("2. Run migrations against your database:\n")
	b.WriteString("   ```\n   make migrate-up DB_URL=\"your_postgres_url\"\n   ```\n\n")
	b.WriteString("3. Enable auth in your environment:\n")
	b.WriteString("   ```\n   AUTH_ENABLED=true\n   AUTH_MODE=hybrid\n   REDIS_ENABLED=true\n   POSTGRES_ENABLED=true\n   ```\n\n")
	b.WriteString("   In this template, startup auth configuration is currently controlled by `AUTH_ENABLED` and `AUTH_MODE`.\n\n")
	b.WriteString("4. Verify everything compiles:\n")
	b.WriteString("   ```\n   go build ./...\n   go test ./...\n   ```\n\n")

	b.WriteString("## Permissions Storage Mode\n\n")
	switch cfg.PermissionsMode {
	case PermsBitmask:
		b.WriteString("**Mode: bitmask** (BIGINT)\n\n")
		b.WriteString("Permissions are stored as a 64-bit integer where each bit represents a permission.\n")
		b.WriteString("This is the goAuth-aligned default. Use `goauth.PermissionMask` for bitwise checks.\n\n")
		b.WriteString("Example:\n")
		b.WriteString("```\n")
		b.WriteString("bit 0 = system.whoami\n")
		b.WriteString("bit 1 = project.read\n")
		b.WriteString("bit 2 = project.write\n")
		b.WriteString("```\n\n")
	case PermsTextArray:
		b.WriteString("**Mode: text_array** (TEXT[])\n\n")
		b.WriteString("Permissions are stored as a PostgreSQL text array.\n")
		b.WriteString("Each element is a permission name string (e.g., `system.whoami`).\n\n")
	case PermsJSONB:
		b.WriteString("**Mode: jsonb** (JSONB)\n\n")
		b.WriteString("Permissions are stored as a JSONB array of strings.\n")
		b.WriteString("Supports flexible querying with PostgreSQL JSONB operators.\n\n")
	}

	b.WriteString("## Regeneration\n\n")
	b.WriteString("To regenerate auth bootstrap:\n")
	b.WriteString("```\n")
	b.WriteString("go run ./cmd/authgen --force\n")
	b.WriteString("```\n\n")
	b.WriteString("Or with a config file:\n")
	b.WriteString("```\n")
	b.WriteString("go run ./cmd/authgen --config authgen.yaml --force\n")
	b.WriteString("```\n\n")
	b.WriteString("After regeneration, re-run `sqlc generate` and `go build ./...`.\n")

	if err := os.WriteFile(docPath, []byte(b.String()), 0o644); err != nil {
		return err
	}

	fmt.Printf("  created: %s\n", docPath)
	return nil
}

func saveConfigSnapshot(workspaceRoot string, cfg AuthGenConfig) error {
	data, err := cfg.MarshalYAML()
	if err != nil {
		return err
	}

	snapshotPath := filepath.Join(workspaceRoot, "authgen.yaml")
	header := "# Auth bootstrap configuration snapshot.\n# Generated by authgen — can be reused with: go run ./cmd/authgen --config authgen.yaml\n\n"

	if err := os.WriteFile(snapshotPath, []byte(header+string(data)), 0o644); err != nil {
		return err
	}

	fmt.Printf("  created: %s\n", snapshotPath)
	return nil
}
