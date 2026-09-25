package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Options configures one init run.
type Options struct {
	// Root is the repository root (contains go.mod).
	Root string
	// Module is the new Go module path, e.g. github.com/acme/foo.
	Module string
	// Name is the human project name used for the README title.
	Name string
	// Description replaces the README intro.
	Description string
	// Copyright is the LICENSE copyright holder; empty leaves a TODO.
	Copyright string
	// Prune holds the feature markers to remove (see features).
	Prune map[string]bool
	// DryRun prints the plan without writing.
	DryRun bool
	// KeepInit keeps cmd/templateinit and every marker so init can run again.
	KeepInit bool
	// SkipPostSteps skips go mod tidy and sqlc generate (tests).
	SkipPostSteps bool
	// Out receives the plan/progress log.
	Out io.Writer
}

// Result summarizes what a run did (or would do, in dry-run mode).
type Result struct {
	Deleted  []string
	Modified []string
	Notes    []string
}

var modulePathPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~\-]*(/[A-Za-z0-9._~\-]+)*$`)

// Run performs (or plans) the initialization.
func Run(opts Options) (Result, error) {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Prune == nil {
		opts.Prune = map[string]bool{}
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return Result{}, err
	}
	opts.Root = root

	oldModule, err := readModulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		return Result{}, err
	}
	newModule := strings.TrimSpace(opts.Module)
	if newModule == "" {
		newModule = oldModule
	}
	if !modulePathPattern.MatchString(newModule) {
		return Result{}, fmt.Errorf("invalid module path %q", newModule)
	}

	var res Result

	// 1. Delete pruned features (and the init tool itself).
	var toDelete []string
	sqlTouched := false
	for _, f := range features {
		if opts.Prune[f.Marker] {
			toDelete = append(toDelete, f.Paths...)
			sqlTouched = sqlTouched || f.TouchesSQL
		}
	}
	if !opts.KeepInit {
		toDelete = append(toDelete, initPaths...)
	}
	for _, rel := range toDelete {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Lstat(abs); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		res.Deleted = append(res.Deleted, rel)
		fmt.Fprintf(opts.Out, "delete  %s\n", rel)
		if !opts.DryRun {
			if err := os.RemoveAll(abs); err != nil {
				return res, fmt.Errorf("delete %s: %w", rel, err)
			}
		}
	}

	// 2. Rewrite every remaining text file.
	remove := map[string]bool{markerMaintainer: true}
	for m, on := range opts.Prune {
		remove[m] = on
	}
	if !opts.KeepInit {
		remove[markerInit] = true
	}
	deleted := map[string]bool{}
	for _, rel := range res.Deleted {
		deleted[rel] = true
	}

	files, err := textFiles(root)
	if err != nil {
		return res, err
	}
	for _, rel := range files {
		if underAny(rel, deleted) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		original, err := os.ReadFile(abs)
		if err != nil {
			return res, err
		}
		updated, err := transform(rel, original, transformContext{
			opts:      opts,
			oldModule: oldModule,
			newModule: newModule,
			remove:    remove,
		})
		if err != nil {
			return res, fmt.Errorf("%s: %w", rel, err)
		}
		if bytes.Equal(original, updated) {
			continue
		}
		res.Modified = append(res.Modified, rel)
		fmt.Fprintf(opts.Out, "update  %s\n", rel)
		if !opts.DryRun {
			info, err := os.Stat(abs)
			if err != nil {
				return res, err
			}
			if err := os.WriteFile(abs, updated, info.Mode().Perm()); err != nil {
				return res, err
			}
		}
	}

	// 3. Post steps.
	if !strings.HasPrefix(newModule, "github.com/") && newModule != oldModule {
		res.Notes = append(res.Notes, "module is not on github.com: review .github/ISSUE_TEMPLATE/config.yml links")
	}
	if strings.TrimSpace(opts.Copyright) == "" {
		res.Notes = append(res.Notes, "LICENSE copyright holder left as a TODO (pass --copyright)")
	}
	res.Notes = append(res.Notes, "SECURITY.md contact left as a TODO: add your security contact")

	if !opts.DryRun && !opts.SkipPostSteps {
		if newModule != oldModule || len(res.Deleted) > 0 {
			if err := runCmd(opts, root, "go", "mod", "tidy"); err != nil {
				res.Notes = append(res.Notes, "go mod tidy failed: "+err.Error()+" (run it manually)")
			}
		}
		if sqlTouched {
			if sqlc := findSQLC(); sqlc != "" {
				if err := runCmd(opts, root, sqlc, "generate"); err != nil {
					res.Notes = append(res.Notes, "sqlc generate failed: "+err.Error()+" (run make sqlc-generate)")
				}
			} else {
				res.Notes = append(res.Notes, "sqlc not found: run `make sqlc-generate` to drop generated models for pruned tables")
			}
		}
	}

	for _, n := range res.Notes {
		fmt.Fprintf(opts.Out, "note    %s\n", n)
	}
	sort.Strings(res.Modified)
	return res, nil
}

type transformContext struct {
	opts      Options
	oldModule string
	newModule string
	remove    map[string]bool
}

// transform applies every content rewrite to one file.
func transform(rel string, content []byte, tc transformContext) ([]byte, error) {
	out := content
	var err error

	out, err = stripMarkers(out, tc.remove, !tc.opts.KeepInit)
	if err != nil {
		return nil, err
	}
	if tc.newModule != tc.oldModule {
		out = replaceModule(out, tc.oldModule, tc.newModule)
	}

	switch rel {
	case "README.md":
		out = rewriteReadme(out, tc.opts)
	case "CHANGELOG.md":
		out = []byte("# Changelog\n\nAll notable changes to this project are documented in this file.\n\n## Unreleased\n\n- Project created from the SuperAPI template.\n")
	case "LICENSE":
		out = rewriteLicense(out, tc.opts.Copyright)
	case "SECURITY.md":
		out = bytes.ReplaceAll(out, []byte("security@projectbook.dev"), []byte("TODO: add your security contact address"))
	}

	if strings.HasSuffix(rel, ".go") && !bytes.Equal(out, content) {
		formatted, err := format.Source(out)
		if err != nil {
			return nil, fmt.Errorf("gofmt after rewrite: %w", err)
		}
		out = formatted
	}
	return out, nil
}

var markerPattern = regexp.MustCompile(`template:(begin|end)\s+([a-z][a-z0-9-]*)`)

// stripMarkers removes blocks whose feature is in remove. When dropMarkers is
// set, the marker lines of kept blocks are removed too; otherwise they stay so
// init can run again (--keep-init).
func stripMarkers(content []byte, remove map[string]bool, dropMarkers bool) ([]byte, error) {
	if !bytes.Contains(content, []byte("template:")) {
		return content, nil
	}
	lines := strings.SplitAfter(string(content), "\n")
	var out strings.Builder
	var stack []string
	skipDepth := 0 // number of open removed blocks

	for i, line := range lines {
		m := markerPattern.FindStringSubmatch(line)
		if m == nil {
			if skipDepth == 0 {
				out.WriteString(line)
			}
			continue
		}
		kind, name := m[1], m[2]
		switch kind {
		case "begin":
			stack = append(stack, name)
			if remove[name] || skipDepth > 0 {
				skipDepth++
				continue
			}
		case "end":
			if len(stack) == 0 || stack[len(stack)-1] != name {
				return nil, fmt.Errorf("line %d: unbalanced template:end %s", i+1, name)
			}
			stack = stack[:len(stack)-1]
			if skipDepth > 0 {
				skipDepth--
				continue
			}
		}
		if !dropMarkers {
			out.WriteString(line)
		}
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("unterminated template:begin %s", stack[len(stack)-1])
	}
	return []byte(out.String()), nil
}

// replaceModule replaces whole occurrences of oldModule (not prefixes of a
// longer path element such as oldModule+"-extra").
func replaceModule(content []byte, oldModule, newModule string) []byte {
	old := []byte(oldModule)
	if !bytes.Contains(content, old) {
		return content
	}
	var out bytes.Buffer
	rest := content
	for {
		idx := bytes.Index(rest, old)
		if idx < 0 {
			out.Write(rest)
			break
		}
		end := idx + len(old)
		out.Write(rest[:idx])
		if end < len(rest) && isPathChar(rest[end]) {
			out.Write(old)
		} else {
			out.WriteString(newModule)
		}
		rest = rest[end:]
	}
	return out.Bytes()
}

func isPathChar(c byte) bool {
	// '.' is not a continuation: ".../superapi.git" and a sentence-final
	// ".../superapi." both refer to the module and are rewritten.
	return c == '-' || c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

var readmeTitle = regexp.MustCompile(`(?m)^# .*$`)

func rewriteReadme(content []byte, opts Options) []byte {
	name := strings.TrimSpace(opts.Name)
	if name != "" {
		if loc := readmeTitle.FindIndex(content); loc != nil {
			content = append(append(append([]byte(nil), content[:loc[0]]...), []byte("# "+name)...), content[loc[1]:]...)
		}
	}
	desc := strings.TrimSpace(opts.Description)
	if desc == "" {
		label := name
		if label == "" {
			label = "This service"
		}
		desc = label + " is a Go API built on the SuperAPI template. TODO: describe what it does."
	}
	return bytes.ReplaceAll(content, []byte("<!-- template:description -->"), []byte(desc))
}

var licenseHolder = regexp.MustCompile(`Copyright (\d{4}) SuperAPI contributors`)

func rewriteLicense(content []byte, holder string) []byte {
	holder = strings.TrimSpace(holder)
	if holder == "" {
		holder = "TODO: copyright holder"
	}
	year := fmt.Sprint(time.Now().Year())
	return licenseHolder.ReplaceAll(content, []byte("Copyright "+year+" "+holder))
}

func readModulePath(goMod string) (string, error) {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return "", fmt.Errorf("read go.mod (run from the repository root): %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", errors.New("go.mod has no module directive")
}

// textFiles lists repository files eligible for rewriting (relative, slash
// separated), skipping VCS data, build output and binary files.
func textFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "bin", "dist", "build":
				if rel != "." {
					return filepath.SkipDir
				}
			}
			if rel == "performance/results" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || rel == "go.sum" {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 2<<20 {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil // binary
		}
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out, err
}

func underAny(rel string, prefixes map[string]bool) bool {
	for p := range prefixes {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

func findSQLC() string {
	if p, err := exec.LookPath("sqlc"); err == nil {
		return p
	}
	if out, err := exec.Command("go", "env", "GOPATH").Output(); err == nil {
		candidate := filepath.Join(strings.TrimSpace(string(out)), "bin", "sqlc")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func runCmd(opts Options, dir, name string, args ...string) error {
	fmt.Fprintf(opts.Out, "run     %s %s\n", filepath.Base(name), strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stdout = opts.Out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
