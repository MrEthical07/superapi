package main

import (
	"strings"
	"testing"
)

func TestRenderMigrationUp_Default(t *testing.T) {
	cfg := DefaultConfig()
	sql := renderMigrationUp(cfg)

	mustContain := []string{
		"CREATE TABLE IF NOT EXISTS users",
		"id UUID PRIMARY KEY",
		"email TEXT NOT NULL UNIQUE",
		"password_hash TEXT NOT NULL",
		"role TEXT",
		"permissions BIGINT NOT NULL DEFAULT 0",
		"status TEXT NOT NULL DEFAULT 'active'",
		"created_at TIMESTAMPTZ",
		"updated_at TIMESTAMPTZ",
	}

	for _, s := range mustContain {
		if !strings.Contains(sql, s) {
			t.Errorf("migration UP should contain %q\n\nGot:\n%s", s, sql)
		}
	}

	mustNotContain := []string{
		"tenant_id",
		"is_verified",
		"deleted_at",
		"last_login_at",
	}
	for _, s := range mustNotContain {
		if strings.Contains(sql, s) {
			t.Errorf("default migration UP should NOT contain %q", s)
		}
	}
}

func TestRenderMigrationUp_WithAllOptions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TenantEnabled = true
	cfg.VerificationFlag = true
	cfg.SoftDelete = true
	cfg.LastLogin = true

	sql := renderMigrationUp(cfg)

	for _, s := range []string{"tenant_id", "is_verified", "deleted_at", "last_login_at"} {
		if !strings.Contains(sql, s) {
			t.Errorf("migration UP should contain %q", s)
		}
	}
}

func TestRenderMigrationUp_EmailPK(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IDType = IDTypeEmail
	sql := renderMigrationUp(cfg)

	if !strings.Contains(sql, "email TEXT PRIMARY KEY") {
		t.Errorf("expected email as primary key\n\nGot:\n%s", sql)
	}
	if strings.Contains(sql, "id UUID") {
		t.Error("should not contain uuid id when email is PK")
	}
}

func TestRenderMigrationUp_BigintID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IDType = IDTypeBigint
	sql := renderMigrationUp(cfg)

	if !strings.Contains(sql, "BIGINT GENERATED ALWAYS AS IDENTITY") {
		t.Errorf("expected bigint auto-increment\n\nGot:\n%s", sql)
	}
}

func TestRenderMigrationUp_TextArrayPerms(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PermissionsMode = PermsTextArray
	sql := renderMigrationUp(cfg)

	if !strings.Contains(sql, "TEXT[]") {
		t.Errorf("expected TEXT[] permissions\n\nGot:\n%s", sql)
	}
}

func TestRenderMigrationUp_JSONBPerms(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PermissionsMode = PermsJSONB
	sql := renderMigrationUp(cfg)

	if !strings.Contains(sql, "JSONB") {
		t.Errorf("expected JSONB permissions\n\nGot:\n%s", sql)
	}
}

func TestRenderMigrationDown(t *testing.T) {
	cfg := DefaultConfig()
	sql := renderMigrationDown(cfg)
	if !strings.Contains(sql, "DROP TABLE IF EXISTS users") {
		t.Errorf("migration DOWN should drop users table\n\nGot:\n%s", sql)
	}
}

func TestRenderQueries_Default(t *testing.T) {
	cfg := DefaultConfig()
	sql := renderQueries(cfg)

	mustContain := []string{
		"-- name: CreateAuthUser :one",
		"-- name: GetAuthUserByID :one",
		"-- name: GetAuthUserByLogin :one",
		"-- name: UpdateAuthUserPasswordHash :exec",
		"-- name: UpdateAuthUserStatus :one",
	}

	for _, s := range mustContain {
		if !strings.Contains(sql, s) {
			t.Errorf("queries should contain %q\n\nGot:\n%s", s, sql)
		}
	}
}

func TestRenderQueries_WithTenant(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TenantEnabled = true
	sql := renderQueries(cfg)

	if !strings.Contains(sql, "GetAuthUserByIDAndTenant") {
		t.Errorf("queries with tenant should contain GetAuthUserByIDAndTenant\n\nGot:\n%s", sql)
	}
}

func TestRenderQueries_WithVerification(t *testing.T) {
	cfg := DefaultConfig()
	cfg.VerificationFlag = true
	sql := renderQueries(cfg)

	if !strings.Contains(sql, "UpdateAuthUserVerification") {
		t.Errorf("queries with verification should contain UpdateAuthUserVerification\n\nGot:\n%s", sql)
	}
}

func TestRenderQueries_WithSoftDelete(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SoftDelete = true
	sql := renderQueries(cfg)

	if !strings.Contains(sql, "deleted_at IS NULL") {
		t.Errorf("queries with soft delete should filter by deleted_at IS NULL\n\nGot:\n%s", sql)
	}
}

func TestRenderQueries_WithLastLogin(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LastLogin = true
	sql := renderQueries(cfg)

	if !strings.Contains(sql, "UpdateAuthUserLastLogin") {
		t.Errorf("queries with last login should contain UpdateAuthUserLastLogin\n\nGot:\n%s", sql)
	}
}

func TestRenderProvider_Default(t *testing.T) {
	cfg := DefaultConfig()
	provider, err := renderProvider(cfg)
	if err != nil {
		t.Fatalf("render provider: %v", err)
	}

	mustContain := []string{
		"SQLCUserProvider",
		"NewSQLCUserProvider",
		"GetUserByIdentifier",
		"GetUserByID",
		"UpdatePasswordHash",
		"CreateUser",
		"UpdateAccountStatus",
		"mapAccountStatusToString",
		"parseAccountStatus",
	}

	for _, s := range mustContain {
		if !strings.Contains(provider, s) {
			t.Errorf("provider should contain %q", s)
		}
	}
}

func TestRenderProvider_WithTenant(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TenantEnabled = true
	provider, err := renderProvider(cfg)
	if err != nil {
		t.Fatalf("render provider: %v", err)
	}

	if !strings.Contains(provider, "parseTenantUUID") {
		t.Error("provider with tenant should contain parseTenantUUID")
	}
}

func TestBuildSelectColumns_Default(t *testing.T) {
	cfg := DefaultConfig()
	cols := buildSelectColumns(cfg)

	expected := []string{"id", "email", "password_hash", "role", "permissions", "status", "created_at", "updated_at"}
	if len(cols) != len(expected) {
		t.Fatalf("expected %d columns, got %d: %v", len(expected), len(cols), cols)
	}
	for i, e := range expected {
		if cols[i] != e {
			t.Errorf("column[%d] = %q, want %q", i, cols[i], e)
		}
	}
}

func TestBuildInsertColumns_Default(t *testing.T) {
	cfg := DefaultConfig()
	cols, params := buildInsertColumns(cfg)

	// UUID auto-generates, so INSERT should NOT include id
	for _, c := range cols {
		if c == "id" {
			t.Error("UUID insert should not include id column (auto-generated)")
		}
	}

	if len(cols) != len(params) {
		t.Fatalf("columns (%d) and params (%d) count mismatch", len(cols), len(params))
	}
}

func TestBuildInsertColumns_BigintID(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IDType = IDTypeBigint
	cols, _ := buildInsertColumns(cfg)

	for _, c := range cols {
		if c == "id" {
			t.Error("bigint ID should not be in INSERT (auto-increment)")
		}
	}
}

func TestBuildInsertColumns_EmailPK(t *testing.T) {
	cfg := DefaultConfig()
	cfg.IDType = IDTypeEmail
	cols, _ := buildInsertColumns(cfg)

	if cols[0] != "email" {
		t.Errorf("email PK first insert col should be email, got %q", cols[0])
	}
}

func TestToPascal(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"email", "Email"},
		{"password_hash", "PasswordHash"},
		{"created_at", "CreatedAt"},
		{"identifier", "Identifier"},
	}
	for _, tc := range cases {
		got := toPascal(tc.in)
		if got != tc.want {
			t.Errorf("toPascal(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseNumberList(t *testing.T) {
	cases := []struct {
		input string
		max   int
		want  []int
	}{
		{"1,2,3", 5, []int{0, 1, 2}},
		{"1, 3, 5", 5, []int{0, 2, 4}},
		{"1,1,2", 5, []int{0, 1}},
		{"", 5, nil},
		{"99", 5, nil},
	}
	for _, tc := range cases {
		got := parseNumberList(tc.input, tc.max)
		if len(got) != len(tc.want) {
			t.Errorf("parseNumberList(%q, %d) = %v, want %v", tc.input, tc.max, got, tc.want)
			continue
		}
		for i, v := range got {
			if v != tc.want[i] {
				t.Errorf("parseNumberList(%q, %d)[%d] = %d, want %d", tc.input, tc.max, i, v, tc.want[i])
			}
		}
	}
}
