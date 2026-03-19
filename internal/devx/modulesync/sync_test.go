package modulesync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncCopiesModuleSQLIntoGlobalFolders(t *testing.T) {
	root := t.TempDir()

	moduleDB := filepath.Join(root, "internal", "modules", "projects", "db")
	if err := os.MkdirAll(moduleDB, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDB, "schema.sql"), []byte("CREATE TABLE projects ();"), 0o644); err != nil {
		t.Fatalf("write schema: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDB, "queries.sql"), []byte("-- name: ListProjects :many"), 0o644); err != nil {
		t.Fatalf("write queries: %v", err)
	}

	if err := Sync(root); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	schemaOut, err := os.ReadFile(filepath.Join(root, "db", "schema", "projects.sql"))
	if err != nil {
		t.Fatalf("read synced schema: %v", err)
	}
	if !strings.Contains(string(schemaOut), "internal/modules/projects/db/schema.sql") {
		t.Fatalf("schema output missing source header: %s", string(schemaOut))
	}

	queriesOut, err := os.ReadFile(filepath.Join(root, "db", "queries", "projects.sql"))
	if err != nil {
		t.Fatalf("read synced queries: %v", err)
	}
	if !strings.Contains(string(queriesOut), "ListProjects") {
		t.Fatalf("queries output missing content: %s", string(queriesOut))
	}
}

func TestSyncRemovesStaleGeneratedFiles(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, "db", "schema")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	staleFile := filepath.Join(schemaDir, "old.sql")
	if err := os.WriteFile(staleFile, []byte(generatedHeaderPrefix+"internal/modules/old/db/schema.sql\n\n"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	modulesDir := filepath.Join(root, "internal", "modules")
	if err := os.MkdirAll(modulesDir, 0o755); err != nil {
		t.Fatalf("mkdir modules: %v", err)
	}

	if err := Sync(root); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatalf("expected stale generated file to be removed")
	}
}
