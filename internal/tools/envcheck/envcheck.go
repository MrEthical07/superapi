// Package envcheck verifies that every environment variable the code reads is
// documented in .env.example and docs/environment-variables.md.
//
// It parses Go source (not text, so commented-out examples do not count) and
// collects the string-literal first argument of known env readers such as
// getenv/getBool (config), envBool/envCSV (auth), the exported config.Env*
// readers optional features use for their own settings, and
// os.Getenv/os.LookupEnv. superapi-verify runs it so an undocumented variable
// fails CI.
package envcheck

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// readers are call names whose first string-literal argument is an env key.
var readers = map[string]bool{
	"getenv": true, "getBool": true, "getInt": true, "getInt32": true, "getInt64": true,
	"getDuration": true, "getFloat64": true, "getCSV": true,
	"envBool": true, "envCSV": true, "envDuration": true,
	"EnvString": true, "EnvBool": true, "EnvInt": true, "EnvDuration": true, "EnvCSV": true,
	"Getenv": true, "LookupEnv": true,
}

// deprecatedReaders are call names for keys that are deprecated and ignored:
// they must be documented in docs/environment-variables.md but are
// intentionally absent from .env.example.
var deprecatedReaders = map[string]bool{"EnvDeprecated": true}

var keyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)

// Result lists variables read by code but missing from each document.
type Result struct {
	Keys []string
	// Deprecated lists the keys read through config.EnvDeprecated.
	Deprecated     []string
	MissingExample []string
	MissingDocs    []string
}

// OK reports whether every key is documented.
func (r Result) OK() bool { return len(r.MissingExample) == 0 && len(r.MissingDocs) == 0 }

// Problems renders human-readable findings.
func (r Result) Problems() []string {
	out := make([]string, 0, len(r.MissingExample)+len(r.MissingDocs))
	for _, k := range r.MissingExample {
		out = append(out, fmt.Sprintf("%s is read by the code but missing from .env.example", k))
	}
	for _, k := range r.MissingDocs {
		out = append(out, fmt.Sprintf("%s is read by the code but missing from docs/environment-variables.md", k))
	}
	return out
}

// Check scans root (the repository root) and compares against its docs.
func Check(root string) (Result, error) {
	keys, deprecated, err := collect(root)
	if err != nil {
		return Result{}, err
	}
	example, err := documentedKeys(filepath.Join(root, ".env.example"))
	if err != nil {
		return Result{}, err
	}
	docs, err := documentedKeys(filepath.Join(root, "docs", "environment-variables.md"))
	if err != nil {
		return Result{}, err
	}

	res := Result{Keys: keys}
	for k := range deprecated {
		res.Deprecated = append(res.Deprecated, k)
	}
	sort.Strings(res.Deprecated)
	for _, k := range keys {
		if !deprecated[k] && !example[k] {
			res.MissingExample = append(res.MissingExample, k)
		}
		if !docs[k] {
			res.MissingDocs = append(res.MissingDocs, k)
		}
	}
	return res, nil
}

// CollectKeys returns every env key read by non-test Go files under root.
func CollectKeys(root string) ([]string, error) {
	keys, _, err := collect(root)
	return keys, err
}

// collect returns every key read and the subset that is only ever read as
// deprecated (never as a live setting).
func collect(root string) ([]string, map[string]bool, error) {
	live := map[string]bool{}
	dead := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return collectFile(path, live, dead)
	})
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	for k := range live {
		seen[k] = true
	}
	deprecated := map[string]bool{}
	for k := range dead {
		seen[k] = true
		if !live[k] {
			deprecated[k] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, deprecated, nil
}

func collectFile(path string, live, dead map[string]bool) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		var name string
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			name = fn.Sel.Name
		}
		into := live
		switch {
		case readers[name]:
		case deprecatedReaders[name]:
			into = dead
		default:
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if key, err := strconv.Unquote(lit.Value); err == nil && keyPattern.MatchString(key) {
			into[key] = true
		}
		return true
	})
	return nil
}

var docKeyPattern = regexp.MustCompile(`[A-Z][A-Z0-9_]{2,}`)

// documentedKeys extracts every KEY-looking token from a file: assignments
// (commented or not) in .env.example, table cells and code spans in docs.
func documentedKeys(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		for _, m := range docKeyPattern.FindAllString(sc.Text(), -1) {
			out[m] = true
		}
	}
	return out, sc.Err()
}
