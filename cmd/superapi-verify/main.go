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
		if hint := hintForDiagnostic(diagnostic.Message); hint != "" {
			fmt.Fprintf(stdout, "hint: %s\n", hint)
		}
	}

	return 1
}

func hintForDiagnostic(message string) string {
	normalized := strings.ToLower(strings.TrimSpace(message))

	switch {
	case strings.Contains(normalized, "cannot appear after"):
		return "reorder route policies as auth -> tenant -> rbac -> rate limit -> cache. See docs/policies.md"
	case strings.Contains(normalized, "authrequired is required when rbac or tenant policies are configured"):
		return "add policy.AuthRequired(...) before RBAC/tenant policies. See docs/policies.md"
	case strings.Contains(normalized, "tenantmatchfrompath requires tenantrequired"):
		return "add policy.TenantRequired() before policy.TenantMatchFromPath(...). See docs/policies.md"
	case strings.Contains(normalized, "requires tenantrequired"):
		return "route path includes {tenant_id}; add policy.TenantRequired() and policy.TenantMatchFromPath(\"tenant_id\"). See docs/policies.md"
	case strings.Contains(normalized, "requires varyby.userid or varyby.tenantid"):
		return "CacheRead on authenticated routes must vary by identity. Add VaryBy.UserID or VaryBy.TenantID. See docs/cache-guide.md"
	case strings.Contains(normalized, "unsupported policy constructor"):
		return "use supported policy constructors from internal/core/policy or extend static validator support first"
	case strings.Contains(normalized, "variadic spread policies are not supported"):
		return "pass policies directly in r.Handle(...) so static verify can analyze ordering and dependencies"
	default:
		return "see docs/policies.md for policy rules and troubleshooting"
	}
}
