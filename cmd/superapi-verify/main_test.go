package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunReturnsFailureForInvalidRoutes(t *testing.T) {
	tempDir := t.TempDir()
	source := `package sample
import (
	"net/http"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
)
func register(r httpx.Router, h http.Handler) {
	r.Handle(http.MethodGet, "/api/v1/projects/{id}", h,
		policy.RequirePerm("project.read"),
	)
}
`
	filePath := filepath.Join(tempDir, "routes.go")
	if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{tempDir}, &stdout, &stderr)
	if exitCode != 1 {
		t.Fatalf("exitCode=%d want=1 stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "[ERROR]") {
		t.Fatalf("expected error output, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "hint:") {
		t.Fatalf("expected hint output, got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "docs/policies.md") {
		t.Fatalf("expected docs hint output, got: %s", stdout.String())
	}
}

func TestRunReturnsSuccessForValidRoutes(t *testing.T) {
	tempDir := t.TempDir()
	source := `package sample
import (
	"net/http"
	"time"
	goauth "github.com/MrEthical07/goAuth"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)
func register(r httpx.Router, h http.Handler, engine *goauth.Engine, limiter ratelimit.Limiter) {
	r.Handle(http.MethodGet, "/api/v1/projects/{id}", h,
		policy.AuthRequired(engine, "hybrid"),
		policy.RequirePerm("project.read"),
		policy.RateLimit(limiter, ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ratelimit.ScopeUser}),
	)
}
`
	filePath := filepath.Join(tempDir, "routes.go")
	if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := run([]string{tempDir}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exitCode=%d want=0 stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "verify: ok") {
		t.Fatalf("expected success output, got: %s", stdout.String())
	}
}

func TestHintForDiagnosticFallback(t *testing.T) {
	hint := hintForDiagnostic("some unknown validator output")
	if !strings.Contains(hint, "docs/policies.md") {
		t.Fatalf("fallback hint must reference docs/policies.md, got: %q", hint)
	}
}
