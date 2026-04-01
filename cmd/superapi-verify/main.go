package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MrEthical07/superapi/internal/tools/validator"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("superapi-verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "output format: text|json")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	targets := fs.Args()
	if len(targets) == 0 {
		targets = []string{"./..."}
	}

	diagnostics, err := validator.AnalyzePaths(targets)
	if err != nil {
		fmt.Fprintf(stderr, "verify failed: %v\n", err)
		return 2
	}

	normalizedFormat := strings.ToLower(strings.TrimSpace(*format))
	switch normalizedFormat {
	case "text", "json":
	default:
		fmt.Fprintf(stderr, "unsupported format %q (valid: text, json)\n", *format)
		return 2
	}

	if len(diagnostics) == 0 {
		if normalizedFormat == "json" {
			enc := json.NewEncoder(stdout)
			enc.SetEscapeHTML(true)
			_ = enc.Encode(map[string]any{"ok": true, "diagnostics": []validator.Diagnostic{}})
		} else {
			fmt.Fprintln(stdout, "verify: ok")
		}
		return 0
	}

	if normalizedFormat == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(true)
		_ = enc.Encode(map[string]any{"ok": false, "diagnostics": diagnostics})
		return 1
	}

	for _, diagnostic := range diagnostics {
		fmt.Fprintf(stdout, "[ERROR] %s:%d\n%s\n", diagnostic.File, diagnostic.Line, diagnostic.Message)
	}

	return 1
}
