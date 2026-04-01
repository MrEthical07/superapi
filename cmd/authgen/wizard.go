package main

import (
	"bufio"
	"fmt"
	"strings"
)

// RunWizard runs the interactive auth bootstrap wizard.
// It returns the final AuthGenConfig or an error if the user cancels.
func RunWizard(scanner *bufio.Scanner) (AuthGenConfig, error) {
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║   SuperAPI Auth Bootstrap Generator      ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println()

	choice := promptChoice(scanner, "How would you like to configure auth?", []choiceOption{
		{key: "1", label: "Use recommended defaults (fast setup)"},
		{key: "2", label: "Customize everything"},
	}, "1")

	var cfg AuthGenConfig

	if choice == "1" {
		cfg = DefaultConfig()
		fmt.Println()
		fmt.Println(cfg.Summary())
		fmt.Println()

		action := promptChoice(scanner, "What would you like to do?", []choiceOption{
			{key: "1", label: "Generate with these defaults"},
			{key: "2", label: "Edit selected fields"},
			{key: "3", label: "Cancel"},
		}, "1")

		switch action {
		case "1":
			// proceed with defaults
		case "2":
			cfg = editSelectedFields(scanner, cfg)
		case "3":
			return AuthGenConfig{}, fmt.Errorf("cancelled by user")
		}
	} else {
		cfg = runFullCustomFlow(scanner)
	}

	if err := cfg.Validate(); err != nil {
		return AuthGenConfig{}, err
	}

	fmt.Println()
	fmt.Println("Final configuration:")
	fmt.Println(cfg.Summary())

	confirm := promptChoice(scanner, "Proceed with generation?", []choiceOption{
		{key: "y", label: "Yes, generate"},
		{key: "n", label: "No, cancel"},
	}, "y")
	if confirm != "y" {
		return AuthGenConfig{}, fmt.Errorf("cancelled by user")
	}

	return cfg, nil
}

func runFullCustomFlow(scanner *bufio.Scanner) AuthGenConfig {
	cfg := AuthGenConfig{}

	// 1. Multi-tenancy
	cfg.TenantEnabled = promptYesNo(scanner, "Include tenant support?", false)

	// 2. Roles / permissions
	cfg.RoleEnabled = promptYesNo(scanner, "Include role field?", true)
	cfg.PermissionsEnabled = promptYesNo(scanner, "Include permissions field?", true)
	if cfg.PermissionsEnabled {
		cfg.PermissionsMode = promptPermissionsMode(scanner)
	}

	// 3. User identity
	cfg.IDType = promptIDType(scanner)

	// 4. Login identifier
	cfg.LoginMethod = promptLoginMethod(scanner)

	// 5. Table naming
	cfg.TableName = promptString(scanner, "Users table name", "users")

	// 6. Optional columns
	cfg.PasswordHash = promptYesNo(scanner, "Store password hash in user table?", true)
	cfg.StatusEnabled = promptYesNo(scanner, "Include status column?", true)
	cfg.VerificationFlag = promptYesNo(scanner, "Include verification flag (is_verified)?", false)
	cfg.Timestamps = promptYesNo(scanner, "Include timestamps (created_at, updated_at)?", true)
	cfg.SoftDelete = promptYesNo(scanner, "Include soft delete (deleted_at)?", false)
	cfg.LastLogin = promptYesNo(scanner, "Include last login timestamp?", false)

	return cfg
}

func editSelectedFields(scanner *bufio.Scanner, cfg AuthGenConfig) AuthGenConfig {
	fields := []struct {
		key   string
		label string
	}{
		{"tenant", "Tenant support"},
		{"role", "Role field"},
		{"permissions", "Permissions"},
		{"id_type", "ID type"},
		{"login", "Login method"},
		{"table", "Table name"},
		{"password", "Password hash"},
		{"status", "Status column"},
		{"verification", "Verification flag"},
		{"timestamps", "Timestamps"},
		{"soft_delete", "Soft delete"},
		{"last_login", "Last login"},
	}

	fmt.Println("Which fields would you like to change? (comma-separated numbers, or 'done' to finish)")
	for i, f := range fields {
		fmt.Printf("  %d) %s\n", i+1, f.label)
	}

	input := promptString(scanner, "Fields to edit", "done")
	if input == "done" || input == "" {
		return cfg
	}

	selected := parseNumberList(input, len(fields))

	for _, idx := range selected {
		switch fields[idx].key {
		case "tenant":
			cfg.TenantEnabled = promptYesNo(scanner, "Include tenant support?", cfg.TenantEnabled)
		case "role":
			cfg.RoleEnabled = promptYesNo(scanner, "Include role field?", cfg.RoleEnabled)
		case "permissions":
			cfg.PermissionsEnabled = promptYesNo(scanner, "Include permissions field?", cfg.PermissionsEnabled)
			if cfg.PermissionsEnabled {
				cfg.PermissionsMode = promptPermissionsMode(scanner)
			}
		case "id_type":
			cfg.IDType = promptIDType(scanner)
		case "login":
			cfg.LoginMethod = promptLoginMethod(scanner)
		case "table":
			cfg.TableName = promptString(scanner, "Users table name", cfg.TableName)
		case "password":
			cfg.PasswordHash = promptYesNo(scanner, "Store password hash?", cfg.PasswordHash)
		case "status":
			cfg.StatusEnabled = promptYesNo(scanner, "Include status column?", cfg.StatusEnabled)
		case "verification":
			cfg.VerificationFlag = promptYesNo(scanner, "Include verification flag?", cfg.VerificationFlag)
		case "timestamps":
			cfg.Timestamps = promptYesNo(scanner, "Include timestamps?", cfg.Timestamps)
		case "soft_delete":
			cfg.SoftDelete = promptYesNo(scanner, "Include soft delete?", cfg.SoftDelete)
		case "last_login":
			cfg.LastLogin = promptYesNo(scanner, "Include last login?", cfg.LastLogin)
		}
	}

	return cfg
}

// --- Prompt helpers ---

type choiceOption struct {
	key   string
	label string
}

func promptChoice(scanner *bufio.Scanner, question string, options []choiceOption, defaultKey string) string {
	fmt.Println(question)
	for _, o := range options {
		marker := "  "
		if o.key == defaultKey {
			marker = "* "
		}
		fmt.Printf("  %s[%s] %s\n", marker, o.key, o.label)
	}
	fmt.Printf("Choice [%s]: ", defaultKey)

	if !scanner.Scan() {
		return defaultKey
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return defaultKey
	}
	for _, o := range options {
		if strings.EqualFold(input, o.key) {
			return o.key
		}
	}
	return defaultKey
}

func promptYesNo(scanner *bufio.Scanner, question string, defaultVal bool) bool {
	def := "n"
	if defaultVal {
		def = "y"
	}
	fmt.Printf("%s [%s]: ", question, def)
	if !scanner.Scan() {
		return defaultVal
	}
	input := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if input == "" {
		return defaultVal
	}
	switch input {
	case "y", "yes", "true", "1":
		return true
	case "n", "no", "false", "0":
		return false
	default:
		return defaultVal
	}
}

func promptString(scanner *bufio.Scanner, question string, defaultVal string) string {
	fmt.Printf("%s [%s]: ", question, defaultVal)
	if !scanner.Scan() {
		return defaultVal
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return defaultVal
	}
	return input
}

func promptPermissionsMode(scanner *bufio.Scanner) PermissionsMode {
	choice := promptChoice(scanner, "Permissions storage mode:", []choiceOption{
		{key: "1", label: "bitmask (recommended, goAuth-aligned)"},
		{key: "2", label: "text_array"},
		{key: "3", label: "jsonb"},
	}, "1")
	switch choice {
	case "2":
		return PermsTextArray
	case "3":
		return PermsJSONB
	default:
		return PermsBitmask
	}
}

func promptIDType(scanner *bufio.Scanner) IDType {
	choice := promptChoice(scanner, "Primary user ID format:", []choiceOption{
		{key: "1", label: "UUID (recommended)"},
		{key: "2", label: "BIGINT auto-increment"},
		{key: "3", label: "Email as primary key"},
		{key: "4", label: "Custom string ID"},
	}, "1")
	switch choice {
	case "2":
		return IDTypeBigint
	case "3":
		return IDTypeEmail
	case "4":
		return IDTypeCustomStr
	default:
		return IDTypeUUID
	}
}

func promptLoginMethod(scanner *bufio.Scanner) LoginMethod {
	choice := promptChoice(scanner, "Login method:", []choiceOption{
		{key: "1", label: "Email + password"},
		{key: "2", label: "Username + password"},
		{key: "3", label: "Phone + password"},
		{key: "4", label: "Custom identifier + password"},
	}, "1")
	switch choice {
	case "2":
		return LoginUsername
	case "3":
		return LoginPhone
	case "4":
		return LoginCustom
	default:
		return LoginEmail
	}
}

func parseNumberList(input string, max int) []int {
	parts := strings.Split(input, ",")
	var result []int
	seen := make(map[int]bool)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n := 0
		valid := false
		for _, ch := range p {
			if ch >= '0' && ch <= '9' {
				n = n*10 + int(ch-'0')
				valid = true
			}
		}
		if valid && n >= 1 && n <= max && !seen[n-1] {
			result = append(result, n-1)
			seen[n-1] = true
		}
	}
	return result
}
