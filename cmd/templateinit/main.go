package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("templateinit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: templateinit --module <path> [--name <name>] [flags]")
		fmt.Fprintln(stderr, "\nInitializes a project created from the SuperAPI template (one-time, idempotent).")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}

	opts := Options{Root: ".", Out: stdout, Prune: map[string]bool{}}
	fs.StringVar(&opts.Root, "root", ".", "repository root")
	fs.StringVar(&opts.Module, "module", "", "new Go module path, e.g. github.com/acme/foo (required)")
	fs.StringVar(&opts.Name, "name", "", "project name for the README title, e.g. \"Foo API\"")
	fs.StringVar(&opts.Description, "description", "", "one-line README description")
	fs.StringVar(&opts.Copyright, "copyright", "", "LICENSE copyright holder (default: a TODO)")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "print the plan without changing anything")
	fs.BoolVar(&opts.KeepInit, "keep-init", false, "keep cmd/templateinit and the template markers so init can run again")
	all := fs.Bool("no-all", false, "prune every optional feature (all --no-* flags)")
	prune := make(map[string]*bool, len(features))
	for _, f := range features {
		prune[f.Marker] = fs.Bool(f.Flag, false, f.Help)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return 2
	}
	if strings.TrimSpace(opts.Module) == "" {
		fmt.Fprintln(stderr, "--module is required, e.g. --module github.com/acme/foo")
		return 2
	}
	for marker, on := range prune {
		opts.Prune[marker] = *on || *all
	}

	if opts.DryRun {
		fmt.Fprintln(stdout, "templateinit: dry run (no files are changed)")
	}
	res, err := Run(opts)
	if err != nil {
		fmt.Fprintln(stderr, "templateinit:", err)
		return 1
	}
	fmt.Fprintf(stdout, "templateinit: %d files deleted, %d files updated\n", len(res.Deleted), len(res.Modified))
	if !opts.DryRun {
		fmt.Fprintln(stdout, "\nNext: make dev-up && cp .env.example .env && make migrate-up && make user email=you@example.com role=admin && make run")
		fmt.Fprintln(stdout, "Then run the gate: go build ./... && go test ./... && go run ./cmd/superapi-verify ./...")
	}
	return 0
}
