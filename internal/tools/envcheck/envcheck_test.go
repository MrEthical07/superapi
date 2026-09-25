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
