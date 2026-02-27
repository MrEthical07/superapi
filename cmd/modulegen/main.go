package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MrEthical07/superapi/internal/devx/modulegen"
)

func main() {
	name := flag.String("name", "", "module name (required)")
	forceRaw := flag.String("force", "", "overwrite existing module when set to 1/true")
	flag.Parse()

	spec, err := modulegen.NormalizeName(*name)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	force, err := parseBoolFlag(*forceRaw)
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

	if err := modulegen.GenerateModule(workspaceRoot, spec, force); err != nil {
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
