package envcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepositoryEnvIsDocumented is the guard: a new env var must be added to
// .env.example and docs/environment-variables.md.
func TestRepositoryEnvIsDocumented(t *testing.T) {
	res, err := Check(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(res.Keys) < 50 {
		t.Fatalf("suspiciously few env keys collected (%d); is the scanner broken?", len(res.Keys))
	}
	for _, p := range res.Problems() {
		t.Error(p)
	}
}

func TestCollectKeysIgnoresCommentsAndTests(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", `package a
import "os"
// cfg.X = getenv("COMMENTED_OUT", "")
func f() { _ = os.Getenv("REAL_KEY"); _ = getBool("OTHER_KEY", false); _ = os.Getenv(dynamic) }
func getBool(string, bool) bool { return false }
var dynamic = "x"
`)
	write("a_test.go", `package a
import "os"
func g() { _ = os.Getenv("TEST_ONLY") }
`)
	keys, err := CollectKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(keys, ","); got != "OTHER_KEY,REAL_KEY" {
		t.Fatalf("keys=%s", got)
	}
}

// A feature keeps its settings in its own package and reads them with the
// exported config.Env* helpers; the scanner must find those keys wherever the
// package lives, and flag one missing from either document.
func TestCheckFindsFeatureOwnedKeys(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/widget/config.go", `package widget
import "example.com/x/config"
func load() {
	_ = config.EnvBool("WIDGET_ENABLED", false)
	_ = config.EnvString("WIDGET_NAME", "")
	_ = config.EnvDeprecated("WIDGET_OLD_KNOB")
}
`)
	write(".env.example", "WIDGET_ENABLED=false\n")
	write("docs/environment-variables.md", "| WIDGET_ENABLED | false |\n| WIDGET_OLD_KNOB | deprecated |\n")

	res, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.Keys, ","); got != "WIDGET_ENABLED,WIDGET_NAME,WIDGET_OLD_KNOB" {
		t.Fatalf("keys=%s", got)
	}
	// WIDGET_NAME is undocumented everywhere; the deprecated key is exempt from
	// .env.example but is documented.
	if got := strings.Join(res.MissingExample, ","); got != "WIDGET_NAME" {
		t.Fatalf("missing from .env.example: %s", got)
	}
	if got := strings.Join(res.MissingDocs, ","); got != "WIDGET_NAME" {
		t.Fatalf("missing from docs: %s", got)
	}
	if got := strings.Join(res.Deprecated, ","); got != "WIDGET_OLD_KNOB" {
		t.Fatalf("deprecated: %s", got)
	}
}
