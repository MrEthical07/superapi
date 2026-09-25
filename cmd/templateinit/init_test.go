package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripMarkers(t *testing.T) {
	in := strings.Join([]string{
		"keep 1",
		"// template:begin perf",
		"perf only",
		"// template:begin devx",
		"nested devx",
		"// template:end devx",
		"// template:end perf",
		"# template:begin devx",
		"devx only",
		"# template:end devx",
		"keep 2",
		"",
	}, "\n")

	got, err := stripMarkers([]byte(in), map[string]bool{"perf": true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := "keep 1\ndevx only\nkeep 2\n"; string(got) != want {
		t.Fatalf("prune perf:\n%q\nwant\n%q", got, want)
	}

	kept, err := stripMarkers([]byte(in), map[string]bool{"perf": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kept), "# template:begin devx") || strings.Contains(string(kept), "perf only") {
		t.Fatalf("keep-init must keep markers of kept blocks and drop pruned ones:\n%s", kept)
	}

	for _, bad := range []string{
		"// template:begin perf\nx\n",
		"x\n// template:end perf\n",
		"// template:begin perf\n// template:end devx\n",
	} {
		if _, err := stripMarkers([]byte(bad), nil, true); err == nil {
			t.Errorf("expected unbalanced marker error for %q", bad)
		}
	}
}

func TestReplaceModule(t *testing.T) {
	old := "github.com/MrEthical07/superapi"
	in := strings.Join([]string{
		`import "github.com/MrEthical07/superapi/internal/core/app"`,
		`module github.com/MrEthical07/superapi`,
		`see https://github.com/MrEthical07/superapi.`,
		`clone https://github.com/MrEthical07/superapi.git`,
		`unrelated github.com/MrEthical07/superapi-extra and github.com/MrEthical07/goAuth`,
	}, "\n")
	got := string(replaceModule([]byte(in), old, "github.com/acme/foo"))
	want := strings.Join([]string{
		`import "github.com/acme/foo/internal/core/app"`,
		`module github.com/acme/foo`,
		`see https://github.com/acme/foo.`,
		`clone https://github.com/acme/foo.git`,
		`unrelated github.com/MrEthical07/superapi-extra and github.com/MrEthical07/goAuth`,
	}, "\n")
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRewriteMetadata(t *testing.T) {
	readme := rewriteReadme([]byte("badge\n\n# SuperAPI\n\n<!-- template:description -->\n"), Options{Name: "Foo API"})
	if !bytes.Contains(readme, []byte("# Foo API\n")) || !bytes.Contains(readme, []byte("Foo API is a Go API built on the SuperAPI template")) {
		t.Fatalf("readme: %s", readme)
	}
	license := rewriteLicense([]byte("Copyright 2026 SuperAPI contributors\n"), "")
	if !bytes.Contains(license, []byte("TODO: copyright holder")) {
		t.Fatalf("license: %s", license)
	}
}

// newFixture builds a tiny repository with markers and module references.
func newFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":                   "module github.com/MrEthical07/superapi\n\ngo 1.26\n",
		"main.go":                  "package main\n\nimport _ \"github.com/MrEthical07/superapi/internal/x\"\n\nfunc main() {}\n",
		"internal/x/x.go":          "package x\n\n// template:begin perf\nconst Perf = 1\n\n// template:end perf\nconst Keep = 2\n",
		"performance/README.md":    "perf docs\n",
		"cmd/templateinit/doc.go":  "package main\n",
		"README.md":                "<!-- template:begin maintainer -->\nbadge\n<!-- template:end maintainer -->\n# SuperAPI\n\n<!-- template:description -->\n<!-- template:begin init -->\nmake init\n<!-- template:end init -->\n",
		"CHANGELOG.md":             "# Changelog\n\n## v0.9.0\n",
		"LICENSE":                  "Copyright 2026 SuperAPI contributors\n",
		"SECURITY.md":              "- security@projectbook.dev\n",
		".github/ISSUE_TEMPLATE/c": "url: https://github.com/MrEthical07/superapi/security\n",
	}
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRunDryRunChangesNothing(t *testing.T) {
	dir := newFixture(t)
	before := snapshot(t, dir)
	var out bytes.Buffer
	res, err := Run(Options{Root: dir, Module: "github.com/acme/foo", Name: "Foo", DryRun: true, Prune: map[string]bool{"perf": true}, Out: &out, SkipPostSteps: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Deleted) == 0 || len(res.Modified) == 0 {
		t.Fatalf("dry run should still report a plan: %+v", res)
	}
	if after := snapshot(t, dir); after != before {
		t.Fatal("dry run modified the tree")
	}
}

func TestRunIsIdempotent(t *testing.T) {
	dir := newFixture(t)
	opts := Options{Root: dir, Module: "github.com/acme/foo", Name: "Foo", Prune: map[string]bool{"perf": true}, SkipPostSteps: true}
	if _, err := Run(opts); err != nil {
		t.Fatal(err)
	}
	first := snapshot(t, dir)

	for _, want := range []string{"module github.com/acme/foo", "github.com/acme/foo/internal/x", "const Keep = 2", "# Foo", "https://github.com/acme/foo/security", "TODO: add your security contact", "## Unreleased"} {
		if !strings.Contains(first, want) {
			t.Errorf("missing %q after init", want)
		}
	}
	for _, gone := range []string{"MrEthical07/superapi", "const Perf", "performance/README.md", "template:", "badge", "make init", "cmd/templateinit", "v0.9.0"} {
		if strings.Contains(first, gone) {
			t.Errorf("%q still present after init", gone)
		}
	}

	// cmd/templateinit is gone, but running the logic again must be a no-op.
	res, err := Run(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Deleted) != 0 || len(res.Modified) != 0 {
		t.Fatalf("second run changed files: %+v", res)
	}
	if snapshot(t, dir) != first {
		t.Fatal("second run changed the tree")
	}
}

func TestRunKeepInit(t *testing.T) {
	dir := newFixture(t)
	if _, err := Run(Options{Root: dir, Module: "github.com/acme/foo", KeepInit: true, SkipPostSteps: true}); err != nil {
		t.Fatal(err)
	}
	snap := snapshot(t, dir)
	if !strings.Contains(snap, "cmd/templateinit/doc.go") || !strings.Contains(snap, "template:begin perf") {
		t.Fatal("--keep-init must keep the tool and the markers")
	}
	if strings.Contains(snap, "badge") {
		t.Fatal("maintainer content is removed even with --keep-init")
	}
}

func TestRunRejectsBadModule(t *testing.T) {
	if _, err := Run(Options{Root: newFixture(t), Module: "not a module", SkipPostSteps: true}); err == nil {
		t.Fatal("expected invalid module error")
	}
}

// snapshot renders every file (path + content) for comparisons.
func snapshot(t *testing.T, dir string) string {
	t.Helper()
	files, err := textFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, rel := range files {
		data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		b.WriteString("== " + rel + "\n")
		b.Write(data)
	}
	return b.String()
}

// TestTemplateInitEndToEnd runs init against a copy of this repository and then
// the project's quality gate. It is slow, so it only runs when
// SUPERAPI_TEMPLATEINIT_E2E is set (CI runs it; see .github/workflows/ci.yml).
func TestTemplateInitEndToEnd(t *testing.T) {
	if os.Getenv("SUPERAPI_TEMPLATEINIT_E2E") == "" {
		t.Skip("set SUPERAPI_TEMPLATEINIT_E2E=1 to run")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	combos := map[string]map[string]bool{
		"default": {},
		"all":     {"tenancy": true, "webauthn": true, "document-store": true, "devx": true, "perf": true, "demo": true},
	}
	for name, prune := range combos {
		t.Run(name, func(t *testing.T) {
			dir := copyRepo(t, repoRoot)
			var log bytes.Buffer
			if _, err := Run(Options{Root: dir, Module: "github.com/acme/foo", Name: "Foo API", Prune: prune, Out: &log}); err != nil {
				t.Fatalf("init: %v\n%s", err, log.String())
			}
			for _, args := range [][]string{
				{"go", "build", "./..."},
				{"go", "vet", "./..."},
				{"go", "run", "./cmd/superapi-verify", "./..."},
				{"go", "test", "./..."},
			} {
				cmd := exec.Command(args[0], args[1:]...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
				}
			}
			grep := exec.Command("grep", "-rl", "MrEthical07/superapi", ".")
			grep.Dir = dir
			if out, _ := grep.Output(); len(bytes.TrimSpace(out)) > 0 {
				t.Fatalf("old module path still referenced in:\n%s", out)
			}
		})
	}
}

// copyRepo copies the tracked and untracked (non-ignored) files of root.
func copyRepo(t *testing.T, root string) string {
	t.Helper()
	dst := t.TempDir()
	list := exec.Command("git", "ls-files", "-co", "--exclude-standard", "-z")
	list.Dir = root
	out, err := list.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		src := filepath.Join(root, rel)
		info, err := os.Stat(src)
		if err != nil || info.IsDir() {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}
