package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	"golang.org/x/term"

	"github.com/MrEthical07/superapi/internal/core/app"
	coreauth "github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/features"
)

type options struct {
	email               string
	role                string
	passwordStdin       bool
	requireVerification bool
	// steps are the optional features' parts of account creation; a feature
	// registers its flags through app.UserCLI.
	steps []app.UserStep
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	registered := features.All()
	opts, err := parseFlags(args, stderr, registered)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "error: load config:", err)
		return 1
	}
	if err := cfg.Lint(); err != nil {
		fmt.Fprintln(stderr, "error: config:", err)
		return 1
	}
	if !cfg.Auth.Enabled {
		fmt.Fprintln(stderr, "error: AUTH_ENABLED=true is required (with POSTGRES_ENABLED and REDIS_ENABLED); see .env.example")
		return 1
	}
	for _, step := range opts.steps {
		if err := step.Validate(cfg); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 2
		}
	}

	password, err := readPassword(opts.passwordStdin, stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	deps, err := app.NewDependencies(ctx, cfg, registered...)
	if err != nil {
		fmt.Fprintln(stderr, "error: init dependencies:", err)
		return 1
	}
	defer deps.Close()

	for _, step := range opts.steps {
		ctx, err = step.Prepare(ctx, deps)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	}

	result, err := deps.AuthEngine.CreateAccount(ctx, goauth.CreateAccountRequest{
		Identifier: opts.email,
		Password:   password,
		Role:       opts.role,
	})
	if err != nil {
		switch {
		case errors.Is(err, goauth.ErrAccountExists):
			fmt.Fprintf(stderr, "error: an account for %s already exists\n", opts.email)
		case errors.Is(err, goauth.ErrAccountRoleInvalid):
			fmt.Fprintf(stderr, "error: unknown role %q (see internal/core/auth/roles.go)\n", opts.role)
		case errors.Is(err, goauth.ErrPasswordPolicy):
			fmt.Fprintln(stderr, "error: password does not meet the password policy")
		default:
			fmt.Fprintln(stderr, "error: create account:", err)
		}
		return 1
	}

	status := "active"
	if cfg.Auth.EmailVerificationEnabled {
		if opts.requireVerification {
			status = "pending_verification"
		} else if err := deps.AuthEngine.EnableAccount(ctx, result.UserID); err != nil {
			// The operator vouches for this address: skip email verification.
			fmt.Fprintln(stderr, "error: activate account:", err)
			return 1
		}
	}

	fmt.Fprintf(stdout, "created user\n  id:     %s\n  email:  %s\n  role:   %s\n  status: %s\n", result.UserID, opts.email, result.Role, status)
	for _, step := range opts.steps {
		for _, line := range step.Summary() {
			fmt.Fprintln(stdout, line)
		}
	}
	return 0
}

func parseFlags(args []string, stderr io.Writer, registered []app.Feature) (options, error) {
	fs := flag.NewFlagSet("createuser", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts options
	fs.StringVar(&opts.email, "email", "", "login identifier (email) for the new account (required)")
	fs.StringVar(&opts.role, "role", coreauth.RoleUser, "role to assign (must exist in internal/core/auth/roles.go)")
	fs.BoolVar(&opts.passwordStdin, "password-stdin", false, "read the password from stdin instead of prompting")
	fs.BoolVar(&opts.requireVerification, "require-verification", false, "leave the account pending email verification (only with AUTH_EMAIL_VERIFICATION_ENABLED)")
	for _, f := range registered {
		if cli, ok := f.(app.UserCLI); ok {
			opts.steps = append(opts.steps, cli.UserFlags(fs))
		}
	}
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v (the password is never passed as an argument)", fs.Args())
	}
	opts.email = coreauth.NormalizeIdentifier(opts.email)
	opts.role = strings.TrimSpace(opts.role)
	if opts.email == "" {
		return options{}, errors.New("--email is required")
	}
	return opts, nil
}

// readPassword reads the password from stdin (--password-stdin, first line) or
// prompts twice without echo on a terminal.
func readPassword(fromStdin bool, stdin io.Reader, stderr io.Writer) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		password := strings.TrimRight(line, "\r\n")
		if password == "" {
			return "", errors.New("empty password on stdin")
		}
		return password, nil
	}

	f, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return "", errors.New("stdin is not a terminal; pipe the password with --password-stdin")
	}
	fmt.Fprint(stderr, "Password: ")
	first, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Fprint(stderr, "Confirm password: ")
	second, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	if len(first) == 0 {
		return "", errors.New("empty password")
	}
	return string(first), nil
}
