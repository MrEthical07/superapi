package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MrEthical07/superapi/internal/devx/modulegen"
)

type wizardConfig struct {
	Name            string
	UseDB           bool
	UseAuth         bool
	UseTenant       bool
	UseRateLimit    bool
	UseCache        bool
	CreateMigration bool
}

func main() {
	name := flag.String("name", "", "module name")
	forceRaw := flag.String("force", "", "overwrite existing module when set to 1/true")
	useDB := flag.Bool("db", false, "create module-local schema/query stubs")
	useAuth := flag.Bool("auth", false, "attach auth policy to generated route")
	useTenant := flag.Bool("tenant", false, "attach tenant policy to generated route")
	useRateLimit := flag.Bool("ratelimit", false, "attach rate limit policy to generated route")
	useCache := flag.Bool("cache", false, "attach cache policy to generated route")
	createMigration := flag.Bool("migration", false, "create a global migration scaffold for the module")
	flag.Parse()

	force, err := parseBoolFlag(*forceRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	cfg := wizardConfig{
		Name:            strings.TrimSpace(*name),
		UseDB:           *useDB,
		UseAuth:         *useAuth,
		UseTenant:       *useTenant,
		UseRateLimit:    *useRateLimit,
		UseCache:        *useCache,
		CreateMigration: *createMigration,
	}

	if cfg.Name == "" {
		cfg, err = runWizard(bufio.NewScanner(os.Stdin))
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}

	if cfg.UseTenant && !cfg.UseAuth {
		fmt.Fprintln(os.Stderr, "error: tenant policy requires auth policy because tenant scope depends on AuthContext (rerun with --auth)")
		os.Exit(1)
	}
	if cfg.CreateMigration && !cfg.UseDB {
		fmt.Fprintln(os.Stderr, "error: migration scaffold requires --db")
		os.Exit(1)
	}

	spec, err := modulegen.NormalizeName(cfg.Name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: determine current working directory:", err)
		os.Exit(1)
	}
	workspaceRoot := cwd
	if _, statErr := os.Stat(filepath.Join(workspaceRoot, "go.mod")); statErr != nil {
		fmt.Fprintln(os.Stderr, "error: run this command from repository root")
		os.Exit(1)
	}

	template := modulegen.TemplateConfig{
		Spec: spec,
		Options: modulegen.TemplateOptions{
			UseDB:           cfg.UseDB,
			UseAuth:         cfg.UseAuth,
			UseTenant:       cfg.UseTenant,
			UseRateLimit:    cfg.UseRateLimit,
			UseCache:        cfg.UseCache,
			CreateMigration: cfg.CreateMigration,
		},
	}

	if err := modulegen.GenerateModule(workspaceRoot, template, force); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Printf("generated module %q (package=%q route=/api/v1/%s)\n", spec.RawName, spec.Package, spec.RoutePath)
}

func parseBoolFlag(raw string) (bool, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return false, nil
	}
	switch value {
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, errors.New("invalid force value: use 1/0 or true/false")
	}
}

func runWizard(scanner *bufio.Scanner) (wizardConfig, error) {
	fmt.Println("SuperAPI Module Generator")
	fmt.Println()

	name := promptString(scanner, "Module name", "")
	if name == "" {
		return wizardConfig{}, fmt.Errorf("module name is required")
	}

	useDB := promptYesNo(scanner, "Create module-local db/schema.sql and db/queries.sql?", false)
	createMigration := false
	if useDB {
		createMigration = promptYesNo(scanner, "Create a global migration scaffold too?", true)
	}

	useAuth := promptYesNo(scanner, "Protect generated route with auth?", false)
	useTenant := false
	if useAuth {
		useTenant = promptYesNo(scanner, "Require tenant scope on the generated route?", false)
	}

	useRateLimit := promptYesNo(scanner, "Attach a rate limit policy?", false)
	useCache := promptYesNo(scanner, "Attach a cache policy?", false)

	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  module: %s\n", name)
	fmt.Printf("  db stubs: %t\n", useDB)
	fmt.Printf("  migration scaffold: %t\n", createMigration)
	fmt.Printf("  auth: %t\n", useAuth)
	fmt.Printf("  tenant: %t\n", useTenant)
	fmt.Printf("  rate limit: %t\n", useRateLimit)
	fmt.Printf("  cache: %t\n", useCache)

	if !promptYesNo(scanner, "Proceed with generation?", true) {
		return wizardConfig{}, fmt.Errorf("cancelled by user")
	}

	return wizardConfig{
		Name:            name,
		UseDB:           useDB,
		UseAuth:         useAuth,
		UseTenant:       useTenant,
		UseRateLimit:    useRateLimit,
		UseCache:        useCache,
		CreateMigration: createMigration,
	}, nil
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

func promptString(scanner *bufio.Scanner, question, defaultVal string) string {
	if defaultVal == "" {
		fmt.Printf("%s: ", question)
	} else {
		fmt.Printf("%s [%s]: ", question, defaultVal)
	}
	if !scanner.Scan() {
		return defaultVal
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return defaultVal
	}
	return input
}
