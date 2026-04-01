package main

import (
	"fmt"
	"strings"
	"text/template"
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

// --- Provider template ---

var providerTemplate = template.Must(template.New("provider").Parse(`package auth

import (
	"context"
	"errors"
	"fmt"
{{- if .IDIsBigint}}
	"strconv"
{{- end}}

	goauth "github.com/MrEthical07/goAuth"
	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// SQLCUserProvider is the DB-backed UserProvider for goAuth.
// Generated by authgen — do not edit manually.
type SQLCUserProvider struct {
	queries *sqlcgen.Queries
}

// NewSQLCUserProvider creates a DB-backed user provider.
func NewSQLCUserProvider(queries *sqlcgen.Queries) *SQLCUserProvider {
	return &SQLCUserProvider{queries: queries}
}

func (p *SQLCUserProvider) GetUserByIdentifier(identifier string) (goauth.UserRecord, error) {
	row, err := p.queries.GetAuthUserByLogin(context.Background(), identifier)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return goauth.UserRecord{}, goauth.ErrUserNotFound
		}
		return goauth.UserRecord{}, fmt.Errorf("get user by identifier: %w", err)
	}
	return mapUserToRecord(row), nil
}

func (p *SQLCUserProvider) GetUserByID(userID string) (goauth.UserRecord, error) {
{{- if .IDIsUUID}}
	var pgID pgtype.UUID
	if err := pgID.Scan(userID); err != nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}
	row, err := p.queries.GetAuthUserByID(context.Background(), pgID)
{{- else if .IDIsBigint}}
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}
	row, err := p.queries.GetAuthUserByID(context.Background(), id)
{{- else}}
	row, err := p.queries.GetAuthUserByID(context.Background(), userID)
{{- end}}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return goauth.UserRecord{}, goauth.ErrUserNotFound
		}
		return goauth.UserRecord{}, fmt.Errorf("get user by id: %w", err)
	}
	return mapUserToRecord(row), nil
}

func (p *SQLCUserProvider) UpdatePasswordHash(userID string, newHash string) error {
{{- if not .HasPasswordHash}}
	_ = userID
	_ = newHash
	return goauth.ErrUnauthorized
{{- else}}
{{- if .IDIsUUID}}
	var pgID pgtype.UUID
	if err := pgID.Scan(userID); err != nil {
		return goauth.ErrUserNotFound
	}
	return p.queries.UpdateAuthUserPasswordHash(context.Background(), sqlcgen.UpdateAuthUserPasswordHashParams{
		{{.IDFieldName}}: pgID,
		PasswordHash: newHash,
	})
{{- else if .IDIsBigint}}
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return goauth.ErrUserNotFound
	}
	return p.queries.UpdateAuthUserPasswordHash(context.Background(), sqlcgen.UpdateAuthUserPasswordHashParams{
		{{.IDFieldName}}: id,
		PasswordHash: newHash,
	})
{{- else}}
	return p.queries.UpdateAuthUserPasswordHash(context.Background(), sqlcgen.UpdateAuthUserPasswordHashParams{
		{{.IDFieldName}}: userID,
		PasswordHash: newHash,
	})
{{- end}}
{{- end}}
}

func (p *SQLCUserProvider) CreateUser(ctx context.Context, input goauth.CreateUserInput) (goauth.UserRecord, error) {
	row, err := p.queries.CreateAuthUser(ctx, sqlcgen.CreateAuthUserParams{
{{- range .CreateParams}}
		{{.}},
{{- end}}
	})
	if err != nil {
		return goauth.UserRecord{}, fmt.Errorf("create user: %w", err)
	}
	return mapUserToRecord(row), nil
}

func (p *SQLCUserProvider) UpdateAccountStatus(ctx context.Context, userID string, status goauth.AccountStatus) (goauth.UserRecord, error) {
{{- if not .HasStatus}}
	_ = ctx
	_ = userID
	_ = status
	return goauth.UserRecord{}, goauth.ErrUnauthorized
{{- else}}
{{- if .IDIsUUID}}
	var pgID pgtype.UUID
	if err := pgID.Scan(userID); err != nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}
	row, err := p.queries.UpdateAuthUserStatus(ctx, sqlcgen.UpdateAuthUserStatusParams{
		{{.IDFieldName}}: pgID,
		Status: mapAccountStatusToString(status),
	})
{{- else if .IDIsBigint}}
	id, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}
	row, err := p.queries.UpdateAuthUserStatus(ctx, sqlcgen.UpdateAuthUserStatusParams{
		{{.IDFieldName}}: id,
		Status: mapAccountStatusToString(status),
	})
{{- else}}
	row, err := p.queries.UpdateAuthUserStatus(ctx, sqlcgen.UpdateAuthUserStatusParams{
		{{.IDFieldName}}: userID,
		Status: mapAccountStatusToString(status),
	})
{{- end}}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return goauth.UserRecord{}, goauth.ErrUserNotFound
		}
		return goauth.UserRecord{}, fmt.Errorf("update account status: %w", err)
	}
	return mapUserToRecord(row), nil
{{- end}}
}

// TOTP stubs — implement when MFA is needed.
func (p *SQLCUserProvider) GetTOTPSecret(_ context.Context, _ string) (*goauth.TOTPRecord, error) {
	return nil, goauth.ErrUnauthorized
}
func (p *SQLCUserProvider) EnableTOTP(_ context.Context, _ string, _ []byte) error {
	return goauth.ErrUnauthorized
}
func (p *SQLCUserProvider) DisableTOTP(_ context.Context, _ string) error {
	return goauth.ErrUnauthorized
}
func (p *SQLCUserProvider) MarkTOTPVerified(_ context.Context, _ string) error {
	return goauth.ErrUnauthorized
}
func (p *SQLCUserProvider) UpdateTOTPLastUsedCounter(_ context.Context, _ string, _ int64) error {
	return goauth.ErrUnauthorized
}

// Backup code stubs — implement when MFA is needed.
func (p *SQLCUserProvider) GetBackupCodes(_ context.Context, _ string) ([]goauth.BackupCodeRecord, error) {
	return nil, goauth.ErrUnauthorized
}
func (p *SQLCUserProvider) ReplaceBackupCodes(_ context.Context, _ string, _ []goauth.BackupCodeRecord) error {
	return goauth.ErrUnauthorized
}
func (p *SQLCUserProvider) ConsumeBackupCode(_ context.Context, _ string, _ [32]byte) (bool, error) {
	return false, goauth.ErrUnauthorized
}

// --- Mapping helpers ---

{{.MapFunc}}

func mapAccountStatusToString(s goauth.AccountStatus) string {
	switch s {
	case goauth.AccountActive:
		return "active"
	case goauth.AccountPendingVerification:
		return "pending_verification"
	case goauth.AccountDisabled:
		return "disabled"
	case goauth.AccountLocked:
		return "locked"
	case goauth.AccountDeleted:
		return "deleted"
	default:
		return "active"
	}
}

func parseAccountStatus(s string) goauth.AccountStatus {
	switch s {
	case "active":
		return goauth.AccountActive
	case "pending_verification":
		return goauth.AccountPendingVerification
	case "disabled":
		return goauth.AccountDisabled
	case "locked":
		return goauth.AccountLocked
	case "deleted":
		return goauth.AccountDeleted
	default:
		return goauth.AccountActive
	}
}
`))

// ProviderTemplateData holds the data for the provider template.
type ProviderTemplateData struct {
	// IDIsUUID indicates UUID primary key mode.
	IDIsUUID bool
	// IDIsBigint indicates BIGINT primary key mode.
	IDIsBigint bool
	// IDIsEmail indicates email-as-primary-key mode.
	IDIsEmail bool
	// IDFieldName is the generated sqlc struct field used as logical id.
	IDFieldName string
	// HasPasswordHash indicates password hash persistence is enabled.
	HasPasswordHash bool
	// HasStatus indicates account status column is enabled.
	HasStatus bool
	// HasRole indicates role column is enabled.
	HasRole bool
	// HasPermissions indicates permissions column is enabled.
	HasPermissions bool
	// PermsBitmask indicates BIGINT permission storage mode.
	PermsBitmask bool
	// PermsTextArray indicates TEXT[] permission storage mode.
	PermsTextArray bool
	// PermsJSONB indicates JSONB permission storage mode.
	PermsJSONB bool
	// HasTenant indicates tenant_id support is enabled.
	HasTenant bool
	// MapFunc is generated mapper source from sqlc row to goAuth record.
	MapFunc string
	// CreateParams is generated CreateAuthUser parameter mapping source.
	CreateParams []string
	// ModelName is the sqlc model type name for the users table.
	ModelName string
}

func buildProviderTemplateData(cfg AuthGenConfig) ProviderTemplateData {
	data := ProviderTemplateData{
		IDIsUUID:        cfg.IDType == IDTypeUUID,
		IDIsBigint:      cfg.IDType == IDTypeBigint,
		IDIsEmail:       cfg.IDType == IDTypeEmail,
		HasPasswordHash: cfg.PasswordHash,
		HasStatus:       cfg.StatusEnabled,
		HasRole:         cfg.RoleEnabled,
		HasPermissions:  cfg.PermissionsEnabled,
		PermsBitmask:    cfg.PermissionsMode == PermsBitmask,
		PermsTextArray:  cfg.PermissionsMode == PermsTextArray,
		PermsJSONB:      cfg.PermissionsMode == PermsJSONB,
		HasTenant:       cfg.TenantEnabled,
		ModelName:       toPascal(cfg.TableName),
	}

	// sqlcgen generates a model struct from the table name.
	// For table "users" -> struct "User".
	// For custom table names, sqlc uses PascalCase of the singular form.

	// ID field name for sqlcgen struct.
	if cfg.IDType == IDTypeEmail {
		data.IDFieldName = "Email"
	} else {
		data.IDFieldName = "ID"
	}

	data.MapFunc = buildMapFunc(cfg)
	data.CreateParams = buildCreateParams(cfg)

	return data
}

func buildMapFunc(cfg AuthGenConfig) string {
	var b strings.Builder

	// sqlcgen uses the table name (singular PascalCase) as the model name.
	// For "users" -> "User". We use sqlcgen.User directly.
	modelName := "sqlcgen." + toPascal(cfg.TableName)
	// Handle common plural tables — sqlc singularizes "users" -> "User"
	if cfg.TableName == "users" {
		modelName = "sqlcgen.User"
	}

	b.WriteString(fmt.Sprintf("func mapUserToRecord(row %s) goauth.UserRecord {\n", modelName))
	b.WriteString("\treturn goauth.UserRecord{\n")

	// UserID
	switch cfg.IDType {
	case IDTypeEmail:
		b.WriteString("\t\tUserID:     row.Email,\n")
		b.WriteString("\t\tIdentifier: row.Email,\n")
	case IDTypeUUID:
		b.WriteString("\t\tUserID:     formatUUID(row.ID),\n")
		b.WriteString(fmt.Sprintf("\t\tIdentifier: row.%s,\n", toPascal(cfg.LoginColumnName())))
	case IDTypeBigint:
		b.WriteString("\t\tUserID:     strconv.FormatInt(row.ID, 10),\n")
		b.WriteString(fmt.Sprintf("\t\tIdentifier: row.%s,\n", toPascal(cfg.LoginColumnName())))
	default:
		b.WriteString("\t\tUserID:     row.ID,\n")
		b.WriteString(fmt.Sprintf("\t\tIdentifier: row.%s,\n", toPascal(cfg.LoginColumnName())))
	}

	if cfg.TenantEnabled {
		b.WriteString("\t\tTenantID:     formatUUID(row.TenantID),\n")
	}
	if cfg.PasswordHash {
		b.WriteString("\t\tPasswordHash: row.PasswordHash,\n")
	}
	if cfg.RoleEnabled {
		b.WriteString("\t\tRole:         row.Role.String,\n")
	}
	if cfg.StatusEnabled {
		b.WriteString("\t\tStatus:       parseAccountStatus(row.Status),\n")
	}

	b.WriteString("\t}\n")
	b.WriteString("}\n\n")

	// UUID formatter helper
	if cfg.IDType == IDTypeUUID || cfg.TenantEnabled {
		b.WriteString("func formatUUID(u pgtype.UUID) string {\n")
		b.WriteString("\tif !u.Valid {\n")
		b.WriteString("\t\treturn \"\"\n")
		b.WriteString("\t}\n")
		b.WriteString("\tb := u.Bytes\n")
		b.WriteString("\treturn fmt.Sprintf(\"%08x-%04x-%04x-%04x-%012x\",\n")
		b.WriteString("\t\tb[0:4], b[4:6], b[6:8], b[8:10], b[10:16])\n")
		b.WriteString("}\n")
	}

	return b.String()
}

func buildCreateParams(cfg AuthGenConfig) []string {
	var params []string

	switch cfg.IDType {
	case IDTypeEmail:
		params = append(params, "Email: input.Identifier")
	case IDTypeCustomStr:
		params = append(params, "ID: input.Identifier")
		loginField := toPascal(cfg.LoginColumnName())
		params = append(params, fmt.Sprintf("%s: input.Identifier", loginField))
	case IDTypeBigint:
		loginField := toPascal(cfg.LoginColumnName())
		params = append(params, fmt.Sprintf("%s: input.Identifier", loginField))
	default:
		// UUID — auto-generated by DB
		loginField := toPascal(cfg.LoginColumnName())
		params = append(params, fmt.Sprintf("%s: input.Identifier", loginField))
	}

	if cfg.TenantEnabled {
		params = append(params, "TenantID: parseTenantUUID(input.TenantID)")
	}
	if cfg.PasswordHash {
		params = append(params, "PasswordHash: input.PasswordHash")
	}
	if cfg.RoleEnabled {
		params = append(params, "Role: pgtype.Text{String: input.Role, Valid: input.Role != \"\"}")
	}
	if cfg.PermissionsEnabled {
		switch cfg.PermissionsMode {
		case PermsBitmask:
			params = append(params, "Permissions: 0")
		case PermsTextArray:
			params = append(params, "Permissions: []string{}")
		case PermsJSONB:
			params = append(params, "Permissions: []byte(\"[]\")")
		}
	}
	if cfg.StatusEnabled {
		params = append(params, "Status: mapAccountStatusToString(input.Status)")
	}
	if cfg.VerificationFlag {
		params = append(params, "IsVerified: false")
	}

	return params
}

func toPascal(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "_")
	var result strings.Builder
	for _, p := range parts {
		if len(p) > 0 {
			result.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return result.String()
}

func renderProvider(cfg AuthGenConfig) (string, error) {
	data := buildProviderTemplateData(cfg)
	var b strings.Builder
	if err := providerTemplate.Execute(&b, data); err != nil {
		return "", fmt.Errorf("render provider template: %w", err)
	}

	// Add parseTenantUUID helper if tenant is enabled
	if cfg.TenantEnabled {
		b.WriteString("\nfunc parseTenantUUID(s string) pgtype.UUID {\n")
		b.WriteString("\tvar u pgtype.UUID\n")
		b.WriteString("\t_ = u.Scan(s)\n")
		b.WriteString("\treturn u\n")
		b.WriteString("}\n")
	}

	return b.String(), nil
}
