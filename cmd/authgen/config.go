package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v2"
)

// PermissionsMode defines how permissions are stored in the DB.
type PermissionsMode string

const (
	// PermsBitmask stores permissions as a BIGINT bitmask.
	PermsBitmask PermissionsMode = "bitmask"
	// PermsTextArray stores permissions as a Postgres TEXT[] list.
	PermsTextArray PermissionsMode = "text_array"
	// PermsJSONB stores permissions as a Postgres JSONB array.
	PermsJSONB PermissionsMode = "jsonb"
)

// IDType defines the primary key format for the users table.
type IDType string

const (
	// IDTypeUUID uses UUID primary keys.
	IDTypeUUID IDType = "uuid"
	// IDTypeBigint uses BIGINT identity primary keys.
	IDTypeBigint IDType = "bigint"
	// IDTypeEmail uses the email column as the primary key.
	IDTypeEmail IDType = "email"
	// IDTypeCustomStr uses caller-managed string ids.
	IDTypeCustomStr IDType = "custom_string"
)

// LoginMethod defines how users authenticate.
type LoginMethod string

const (
	// LoginEmail authenticates with email and password.
	LoginEmail LoginMethod = "email"
	// LoginUsername authenticates with username and password.
	LoginUsername LoginMethod = "username"
	// LoginPhone authenticates with phone and password.
	LoginPhone LoginMethod = "phone"
	// LoginCustom authenticates with a custom identifier and password.
	LoginCustom LoginMethod = "custom"
)

// AuthGenConfig holds all wizard/config-driven choices.
type AuthGenConfig struct {
	// TenantEnabled adds optional tenant_id support.
	TenantEnabled bool `yaml:"tenant_enabled"`
	// RoleEnabled includes a role column.
	RoleEnabled bool `yaml:"role_enabled"`
	// PermissionsEnabled includes a permissions column.
	PermissionsEnabled bool `yaml:"permissions_enabled"`
	// PermissionsMode selects permission storage format.
	PermissionsMode PermissionsMode `yaml:"permissions_mode"`
	// IDType selects primary key strategy.
	IDType IDType `yaml:"id_type"`
	// LoginMethod selects login identifier column.
	LoginMethod LoginMethod `yaml:"login_method"`
	// TableName is the auth users table name.
	TableName string `yaml:"table_name"`
	// PasswordHash enables password hash persistence.
	PasswordHash bool `yaml:"password_hash"`
	// StatusEnabled adds account status column.
	StatusEnabled bool `yaml:"status_enabled"`
	// VerificationFlag adds is_verified column.
	VerificationFlag bool `yaml:"verification_flag"`
	// Timestamps adds created_at and updated_at columns.
	Timestamps bool `yaml:"timestamps"`
	// SoftDelete adds deleted_at column.
	SoftDelete bool `yaml:"soft_delete"`
	// LastLogin adds last_login_at column.
	LastLogin bool `yaml:"last_login"`
}

// DefaultConfig returns the goAuth-aligned default configuration.
func DefaultConfig() AuthGenConfig {
	return AuthGenConfig{
		TenantEnabled:      false,
		RoleEnabled:        true,
		PermissionsEnabled: true,
		PermissionsMode:    PermsBitmask,
		IDType:             IDTypeUUID,
		LoginMethod:        LoginEmail,
		TableName:          "users",
		PasswordHash:       true,
		StatusEnabled:      true,
		VerificationFlag:   false,
		Timestamps:         true,
		SoftDelete:         false,
		LastLogin:          false,
	}
}

var validTableName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Validate checks the config for invalid or conflicting combinations.
func (c AuthGenConfig) Validate() error {
	var errs []string

	if c.TableName == "" {
		errs = append(errs, "table_name is required")
	} else if !validTableName.MatchString(c.TableName) {
		errs = append(errs, fmt.Sprintf("table_name %q is not a valid SQL identifier (lowercase letters, digits, underscores; must start with letter)", c.TableName))
	}

	switch c.IDType {
	case IDTypeUUID, IDTypeBigint, IDTypeEmail, IDTypeCustomStr:
	default:
		errs = append(errs, fmt.Sprintf("invalid id_type %q", c.IDType))
	}

	switch c.LoginMethod {
	case LoginEmail, LoginUsername, LoginPhone, LoginCustom:
	default:
		errs = append(errs, fmt.Sprintf("invalid login_method %q", c.LoginMethod))
	}

	if c.PermissionsEnabled {
		switch c.PermissionsMode {
		case PermsBitmask, PermsTextArray, PermsJSONB:
		default:
			errs = append(errs, fmt.Sprintf("invalid permissions_mode %q (must be bitmask, text_array, or jsonb)", c.PermissionsMode))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// LoadConfigFile reads and parses a YAML config file.
func LoadConfigFile(path string) (AuthGenConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AuthGenConfig{}, fmt.Errorf("read config file: %w", err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return AuthGenConfig{}, fmt.Errorf("parse config file: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return AuthGenConfig{}, err
	}
	return cfg, nil
}

// Summary returns a human-readable summary of the configuration.
func (c AuthGenConfig) Summary() string {
	var b strings.Builder
	b.WriteString("Auth Bootstrap Configuration:\n")
	b.WriteString(fmt.Sprintf("  tenant support:     %s\n", boolLabel(c.TenantEnabled)))
	b.WriteString(fmt.Sprintf("  role:               %s\n", boolLabel(c.RoleEnabled)))
	b.WriteString(fmt.Sprintf("  permissions:        %s\n", boolLabel(c.PermissionsEnabled)))
	if c.PermissionsEnabled {
		b.WriteString(fmt.Sprintf("  permissions mode:   %s\n", c.PermissionsMode))
	}
	b.WriteString(fmt.Sprintf("  id type:            %s\n", c.IDType))
	b.WriteString(fmt.Sprintf("  login method:       %s + password\n", c.LoginMethod))
	b.WriteString(fmt.Sprintf("  users table:        %s\n", c.TableName))
	b.WriteString(fmt.Sprintf("  password hash:      %s\n", boolLabel(c.PasswordHash)))
	b.WriteString(fmt.Sprintf("  status:             %s\n", boolLabel(c.StatusEnabled)))
	b.WriteString(fmt.Sprintf("  verification:       %s\n", boolLabel(c.VerificationFlag)))
	b.WriteString(fmt.Sprintf("  timestamps:         %s\n", boolLabel(c.Timestamps)))
	b.WriteString(fmt.Sprintf("  soft delete:        %s\n", boolLabel(c.SoftDelete)))
	b.WriteString(fmt.Sprintf("  last login:         %s\n", boolLabel(c.LastLogin)))
	return b.String()
}

func boolLabel(v bool) string {
	if v {
		return "enabled"
	}
	return "disabled"
}

// MarshalYAML writes the config as YAML for config-driven mode export.
func (c AuthGenConfig) MarshalYAML() ([]byte, error) {
	return yaml.Marshal(c)
}

// LoginColumnName returns the SQL column name for the login identifier.
func (c AuthGenConfig) LoginColumnName() string {
	switch c.LoginMethod {
	case LoginEmail:
		return "email"
	case LoginUsername:
		return "username"
	case LoginPhone:
		return "phone"
	case LoginCustom:
		return "identifier"
	default:
		return "email"
	}
}

// LoginColumnType returns the SQL type for the login column.
func (c AuthGenConfig) LoginColumnType() string {
	return "TEXT"
}

// IDColumnDef returns the SQL column definition for the primary key.
func (c AuthGenConfig) IDColumnDef() string {
	switch c.IDType {
	case IDTypeUUID:
		return "id UUID PRIMARY KEY DEFAULT gen_random_uuid()"
	case IDTypeBigint:
		return "id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY"
	case IDTypeEmail:
		return "" // email column IS the PK
	case IDTypeCustomStr:
		return "id TEXT PRIMARY KEY"
	default:
		return "id UUID PRIMARY KEY DEFAULT gen_random_uuid()"
	}
}

// IDColumnGoType returns the Go type for scanner output.
func (c AuthGenConfig) IDColumnGoType() string {
	switch c.IDType {
	case IDTypeUUID:
		return "pgtype.UUID"
	case IDTypeBigint:
		return "int64"
	case IDTypeEmail:
		return "string" // email is the PK
	case IDTypeCustomStr:
		return "string"
	default:
		return "pgtype.UUID"
	}
}

// PermissionsColumnDef returns the SQL column for permissions storage.
func (c AuthGenConfig) PermissionsColumnDef() string {
	switch c.PermissionsMode {
	case PermsBitmask:
		return "permissions BIGINT NOT NULL DEFAULT 0"
	case PermsTextArray:
		return "permissions TEXT[] NOT NULL DEFAULT '{}'"
	case PermsJSONB:
		return "permissions JSONB NOT NULL DEFAULT '[]'"
	default:
		return "permissions BIGINT NOT NULL DEFAULT 0"
	}
}

// NextMigrationNumber scans db/migrations/ and returns the next sequence number.
func NextMigrationNumber(migrationsDir string) (int, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 1, nil
		}
		return 0, fmt.Errorf("read migrations dir: %w", err)
	}
	max := 0
	for _, e := range entries {
		name := e.Name()
		if len(name) < 6 {
			continue
		}
		numStr := name[:6]
		n := 0
		for _, ch := range numStr {
			if ch >= '0' && ch <= '9' {
				n = n*10 + int(ch-'0')
			} else {
				break
			}
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}
