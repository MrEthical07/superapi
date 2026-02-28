package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigValidation(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
}

func TestConfigValidation_EmptyTableName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TableName = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty table name")
	}
}

func TestConfigValidation_InvalidTableName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TableName = "123invalid"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid table name")
	}
}

func TestConfigValidation_InvalidIDType(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IDType = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid id type")
	}
}

func TestConfigValidation_InvalidLoginMethod(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LoginMethod = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid login method")
	}
}

func TestConfigValidation_InvalidPermissionsMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PermissionsEnabled = true
	cfg.PermissionsMode = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid permissions mode")
	}
}

func TestConfigSummary(t *testing.T) {
	cfg := DefaultConfig()
	summary := cfg.Summary()
	if !strings.Contains(summary, "bitmask") {
		t.Fatal("summary should contain permissions mode")
	}
	if !strings.Contains(summary, "uuid") {
		t.Fatal("summary should contain id type")
	}
	if !strings.Contains(summary, "email") {
		t.Fatal("summary should contain login method")
	}
}

func TestLoginColumnName(t *testing.T) {
	cases := []struct {
		method LoginMethod
		want   string
	}{
		{LoginEmail, "email"},
		{LoginUsername, "username"},
		{LoginPhone, "phone"},
		{LoginCustom, "identifier"},
	}
	for _, tc := range cases {
		cfg := DefaultConfig()
		cfg.LoginMethod = tc.method
		got := cfg.LoginColumnName()
		if got != tc.want {
			t.Errorf("LoginColumnName() for %s = %q, want %q", tc.method, got, tc.want)
		}
	}
}

func TestIDColumnDef(t *testing.T) {
	cases := []struct {
		idType IDType
		expect string
	}{
		{IDTypeUUID, "UUID PRIMARY KEY"},
		{IDTypeBigint, "BIGINT GENERATED ALWAYS"},
		{IDTypeEmail, ""},
		{IDTypeCustomStr, "TEXT PRIMARY KEY"},
	}
	for _, tc := range cases {
		cfg := DefaultConfig()
		cfg.IDType = tc.idType
		got := cfg.IDColumnDef()
		if tc.expect != "" && !strings.Contains(got, tc.expect) {
			t.Errorf("IDColumnDef() for %s = %q, expected to contain %q", tc.idType, got, tc.expect)
		}
		if tc.expect == "" && got != "" {
			t.Errorf("IDColumnDef() for %s = %q, expected empty", tc.idType, got)
		}
	}
}

func TestPermissionsColumnDef(t *testing.T) {
	cases := []struct {
		mode   PermissionsMode
		expect string
	}{
		{PermsBitmask, "BIGINT"},
		{PermsTextArray, "TEXT[]"},
		{PermsJSONB, "JSONB"},
	}
	for _, tc := range cases {
		cfg := DefaultConfig()
		cfg.PermissionsMode = tc.mode
		got := cfg.PermissionsColumnDef()
		if !strings.Contains(got, tc.expect) {
			t.Errorf("PermissionsColumnDef() for %s = %q, expected to contain %q", tc.mode, got, tc.expect)
		}
	}
}

func TestNextMigrationNumber(t *testing.T) {
	dir := t.TempDir()

	num, err := NextMigrationNumber(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != 1 {
		t.Fatalf("expected 1 for empty dir, got %d", num)
	}

	os.WriteFile(filepath.Join(dir, "000003_test.up.sql"), []byte(""), 0o644)
	num, err = NextMigrationNumber(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != 4 {
		t.Fatalf("expected 4, got %d", num)
	}
}

func TestNextMigrationNumber_NonExistentDir(t *testing.T) {
	num, err := NextMigrationNumber("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != 1 {
		t.Fatalf("expected 1, got %d", num)
	}
}

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")

	content := `tenant_enabled: true
role_enabled: true
permissions_enabled: true
permissions_mode: bitmask
id_type: uuid
login_method: email
table_name: auth_users
password_hash: true
status_enabled: true
verification_flag: false
timestamps: true
soft_delete: false
last_login: false
`
	os.WriteFile(cfgPath, []byte(content), 0o644)

	cfg, err := LoadConfigFile(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.TenantEnabled {
		t.Fatal("expected tenant enabled")
	}
	if cfg.TableName != "auth_users" {
		t.Fatalf("expected table_name=auth_users, got %q", cfg.TableName)
	}
}

func TestLoadConfigFile_Invalid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "bad.yaml")
	os.WriteFile(cfgPath, []byte("table_name: 123bad"), 0o644)

	_, err := LoadConfigFile(cfgPath)
	if err == nil {
		t.Fatal("expected validation error")
	}
}
