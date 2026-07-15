package main

import (
	"fmt"
	"strings"
)

// --- Migration templates ---

func renderMigrationUp(cfg AuthGenConfig) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", cfg.TableName))

	var columns []string

	// Primary key
	if cfg.IDType == IDTypeEmail {
		// email IS the primary key
		columns = append(columns, "    email TEXT PRIMARY KEY")
	} else {
		columns = append(columns, "    "+cfg.IDColumnDef())
	}

	// Login identifier column (if not also the PK)
	loginCol := cfg.LoginColumnName()
	if cfg.IDType != IDTypeEmail || loginCol != "email" {
		if loginCol != "email" || cfg.IDType != IDTypeEmail {
			columns = append(columns, fmt.Sprintf("    %s %s NOT NULL UNIQUE", loginCol, cfg.LoginColumnType()))
		}
	}
	// If email login but not email PK, the column was added above.
	// If email PK and email login, the PK above covers it.

	// Tenant
	if cfg.TenantEnabled {
		columns = append(columns, "    tenant_id UUID")
	}

	// Password hash
	if cfg.PasswordHash {
		columns = append(columns, "    password_hash TEXT NOT NULL")
	}

	// Role
	if cfg.RoleEnabled {
		columns = append(columns, "    role TEXT")
	}

	// Permissions
	if cfg.PermissionsEnabled {
		columns = append(columns, "    "+cfg.PermissionsColumnDef())
	}

	// Status
	if cfg.StatusEnabled {
		columns = append(columns, "    status TEXT NOT NULL DEFAULT 'active'")
	}

	// Verification
	if cfg.VerificationFlag {
		columns = append(columns, "    is_verified BOOLEAN NOT NULL DEFAULT FALSE")
	}

	// Timestamps
	if cfg.Timestamps {
		columns = append(columns, "    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()")
		columns = append(columns, "    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()")
	}

	// Soft delete
	if cfg.SoftDelete {
		columns = append(columns, "    deleted_at TIMESTAMPTZ")
	}

	// Last login
	if cfg.LastLogin {
		columns = append(columns, "    last_login_at TIMESTAMPTZ")
	}

	b.WriteString(strings.Join(columns, ",\n"))
	b.WriteString("\n);\n")

	// Indexes
	loginColName := cfg.LoginColumnName()
	if cfg.IDType != IDTypeEmail {
		b.WriteString(fmt.Sprintf("\nCREATE UNIQUE INDEX IF NOT EXISTS %s_%s_unique_idx ON %s (%s);\n",
			cfg.TableName, loginColName, cfg.TableName, loginColName))
	}

	if cfg.TenantEnabled {
		b.WriteString(fmt.Sprintf("\nCREATE INDEX IF NOT EXISTS %s_tenant_id_idx ON %s (tenant_id);\n",
			cfg.TableName, cfg.TableName))
	}

	if cfg.StatusEnabled {
		b.WriteString(fmt.Sprintf("\nCREATE INDEX IF NOT EXISTS %s_status_idx ON %s (status);\n",
			cfg.TableName, cfg.TableName))
	}

	if cfg.Timestamps {
		b.WriteString(fmt.Sprintf("\nCREATE INDEX IF NOT EXISTS %s_created_at_idx ON %s (created_at);\n",
			cfg.TableName, cfg.TableName))
	}

	return b.String()
}

func renderMigrationDown(cfg AuthGenConfig) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE;\n", cfg.TableName)
}

// --- Schema mirror template ---

func renderSchema(cfg AuthGenConfig) string {
	// Schema mirror matches the up migration (without indexes — sqlc uses schema for types).
	return renderMigrationUp(cfg)
}

// --- sqlc queries template ---

func renderQueries(cfg AuthGenConfig) string {
	var b strings.Builder
	table := cfg.TableName
	loginCol := cfg.LoginColumnName()

	// Determine the ID column and parameter references.
	idCol := "id"
	if cfg.IDType == IDTypeEmail {
		idCol = "email"
	}

	// Build column list for INSERT and SELECT.
	selectCols := buildSelectColumns(cfg)
	insertCols, insertParams := buildInsertColumns(cfg)

	// -- CreateAuthUser
	b.WriteString("-- name: CreateAuthUser :one\n")
	b.WriteString(fmt.Sprintf("INSERT INTO %s (%s)\nVALUES (%s)\nRETURNING %s;\n\n",
		table, strings.Join(insertCols, ", "), strings.Join(insertParams, ", "), strings.Join(selectCols, ", ")))

	// -- GetAuthUserByID
	b.WriteString("-- name: GetAuthUserByID :one\n")
	b.WriteString(fmt.Sprintf("SELECT %s\nFROM %s\nWHERE %s = $1",
		strings.Join(selectCols, ", "), table, idCol))
	if cfg.SoftDelete {
		b.WriteString(" AND deleted_at IS NULL")
	}
	b.WriteString(";\n\n")

	// -- GetAuthUserByLogin
	b.WriteString("-- name: GetAuthUserByLogin :one\n")
	b.WriteString(fmt.Sprintf("SELECT %s\nFROM %s\nWHERE %s = $1",
		strings.Join(selectCols, ", "), table, loginCol))
	if cfg.SoftDelete {
		b.WriteString(" AND deleted_at IS NULL")
	}
	b.WriteString(";\n\n")

	// -- UpdateAuthUserPasswordHash
	if cfg.PasswordHash {
		b.WriteString("-- name: UpdateAuthUserPasswordHash :exec\n")
		updateSet := "password_hash = $2"
		if cfg.Timestamps {
			updateSet += ", updated_at = NOW()"
		}
		b.WriteString(fmt.Sprintf("UPDATE %s SET %s WHERE %s = $1;\n\n", table, updateSet, idCol))
	}

	// -- UpdateAuthUserStatus
	if cfg.StatusEnabled {
		b.WriteString("-- name: UpdateAuthUserStatus :one\n")
		updateSet := "status = $2"
		if cfg.Timestamps {
			updateSet += ", updated_at = NOW()"
		}
		b.WriteString(fmt.Sprintf("UPDATE %s SET %s WHERE %s = $1 RETURNING %s;\n\n",
			table, updateSet, idCol, strings.Join(selectCols, ", ")))
	}

	// -- UpdateAuthUserVerification
	if cfg.VerificationFlag {
		b.WriteString("-- name: UpdateAuthUserVerification :one\n")
		updateSet := "is_verified = $2"
		if cfg.Timestamps {
			updateSet += ", updated_at = NOW()"
		}
		b.WriteString(fmt.Sprintf("UPDATE %s SET %s WHERE %s = $1 RETURNING %s;\n\n",
			table, updateSet, idCol, strings.Join(selectCols, ", ")))
	}

	// -- UpdateAuthUserLastLogin
	if cfg.LastLogin {
		b.WriteString("-- name: UpdateAuthUserLastLogin :exec\n")
		b.WriteString(fmt.Sprintf("UPDATE %s SET last_login_at = NOW() WHERE %s = $1;\n\n",
			table, idCol))
	}

	// -- GetAuthUserByTenant (if tenant enabled)
	if cfg.TenantEnabled {
		b.WriteString("-- name: GetAuthUserByIDAndTenant :one\n")
		b.WriteString(fmt.Sprintf("SELECT %s\nFROM %s\nWHERE %s = $1 AND tenant_id = $2",
			strings.Join(selectCols, ", "), table, idCol))
		if cfg.SoftDelete {
			b.WriteString(" AND deleted_at IS NULL")
		}
		b.WriteString(";\n\n")
	}

	return b.String()
}

func buildSelectColumns(cfg AuthGenConfig) []string {
	var cols []string

	if cfg.IDType == IDTypeEmail {
		cols = append(cols, "email")
	} else {
		cols = append(cols, "id")
		cols = append(cols, cfg.LoginColumnName())
	}

	if cfg.TenantEnabled {
		cols = append(cols, "tenant_id")
	}
	if cfg.PasswordHash {
		cols = append(cols, "password_hash")
	}
	if cfg.RoleEnabled {
		cols = append(cols, "role")
	}
	if cfg.PermissionsEnabled {
		cols = append(cols, "permissions")
	}
	if cfg.StatusEnabled {
		cols = append(cols, "status")
	}
	if cfg.VerificationFlag {
		cols = append(cols, "is_verified")
	}
	if cfg.Timestamps {
		cols = append(cols, "created_at", "updated_at")
	}
	if cfg.SoftDelete {
		cols = append(cols, "deleted_at")
	}
	if cfg.LastLogin {
		cols = append(cols, "last_login_at")
	}

	return cols
}

func buildInsertColumns(cfg AuthGenConfig) (cols []string, params []string) {
	paramIdx := 1

	switch cfg.IDType {
	case IDTypeEmail:
		cols = append(cols, "email")
		params = append(params, fmt.Sprintf("$%d", paramIdx))
		paramIdx++
	case IDTypeCustomStr:
		cols = append(cols, "id")
		params = append(params, fmt.Sprintf("$%d", paramIdx))
		paramIdx++
		cols = append(cols, cfg.LoginColumnName())
		params = append(params, fmt.Sprintf("$%d", paramIdx))
		paramIdx++
	case IDTypeBigint:
		// auto-generated ID, don't include in INSERT
		cols = append(cols, cfg.LoginColumnName())
		params = append(params, fmt.Sprintf("$%d", paramIdx))
		paramIdx++
	default:
		// UUID — can be auto-generated or provided
		cols = append(cols, cfg.LoginColumnName())
		params = append(params, fmt.Sprintf("$%d", paramIdx))
		paramIdx++
	}

	if cfg.TenantEnabled {
		cols = append(cols, "tenant_id")
		params = append(params, fmt.Sprintf("$%d", paramIdx))
		paramIdx++
	}
	if cfg.PasswordHash {
		cols = append(cols, "password_hash")
		params = append(params, fmt.Sprintf("$%d", paramIdx))
		paramIdx++
	}
	if cfg.RoleEnabled {
		cols = append(cols, "role")
		params = append(params, fmt.Sprintf("$%d", paramIdx))
		paramIdx++
	}
	if cfg.PermissionsEnabled {
		cols = append(cols, "permissions")
		params = append(params, fmt.Sprintf("$%d", paramIdx))
		paramIdx++
	}
	if cfg.StatusEnabled {
		cols = append(cols, "status")
		params = append(params, fmt.Sprintf("$%d", paramIdx))
		paramIdx++
	}
	if cfg.VerificationFlag {
		cols = append(cols, "is_verified")
		params = append(params, fmt.Sprintf("$%d", paramIdx))
	}

	return cols, params
}
