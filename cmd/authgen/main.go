package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	configPath := flag.String("config", "", "path to authgen YAML config file (non-interactive mode)")
	force := flag.Bool("force", false, "overwrite existing auth bootstrap files")
	flag.Parse()

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: determine current working directory:", err)
		os.Exit(1)
	}
	workspaceRoot := cwd
	if _, statErr := os.Stat(filepath.Join(workspaceRoot, "go.mod")); statErr != nil {
		fmt.Fprintln(os.Stderr, "error: run this command from the repository root (go.mod not found)")
		os.Exit(1)
	}

	var cfg AuthGenConfig

	if *configPath != "" {
		// Config-driven mode
		fmt.Printf("Loading config from: %s\n", *configPath)
		cfg, err = LoadConfigFile(*configPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Println(cfg.Summary())
	} else {
		// Interactive wizard mode
		scanner := bufio.NewScanner(os.Stdin)
		cfg, err = RunWizard(scanner)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}

	fmt.Println()
	fmt.Println("Generating auth bootstrap...")
	fmt.Println()

	if err := GenerateAuth(workspaceRoot, cfg, *force); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("Auth bootstrap complete!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Run: sqlc generate")
	fmt.Println("  2. Run: go build ./...")
	fmt.Println("  3. Run migrations: make migrate-up DB_URL=\"your_postgres_url\"")
	fmt.Println("  4. Enable auth: AUTH_ENABLED=true REDIS_ENABLED=true POSTGRES_ENABLED=true")
	fmt.Println("  5. See docs/auth-bootstrap.md for full details")
}
