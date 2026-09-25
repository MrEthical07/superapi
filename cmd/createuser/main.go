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
	"github.com/MrEthical07/superapi/internal/core/tenant"
)

type options struct {
	email               string
	role                string
	tenantID            string
	createTenant        bool
	passwordStdin       bool
	requireVerification bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stderr)
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
	if cfg.Tenancy.Enabled && opts.tenantID == "" {
		fmt.Fprintln(stderr, "error: TENANCY_ENABLED=true requires --tenant")
		return 2
	}
	if !cfg.Tenancy.Enabled && opts.tenantID != "" {
		fmt.Fprintln(stderr, "error: --tenant requires TENANCY_ENABLED=true")
		return 2
	}

	password, err := readPassword(opts.passwordStdin, stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	deps, err := app.NewDependencies(ctx, cfg)
	if err != nil {
		fmt.Fprintln(stderr, "error: init dependencies:", err)
		return 1
	}
	defer deps.Close()

	if opts.tenantID != "" {
		if err := ensureTenant(ctx, deps, cfg, opts); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		ctx = coreauth.WithRequestTenant(ctx, opts.tenantID)
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
	if opts.tenantID != "" {
		fmt.Fprintf(stdout, "  tenant: %s\n", opts.tenantID)
	}
	return 0
}

func parseFlags(args []string, stderr io.Writer) (options, error) {
	fs := flag.NewFlagSet("createuser", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts options
	fs.StringVar(&opts.email, "email", "", "login identifier (email) for the new account (required)")
	fs.StringVar(&opts.role, "role", coreauth.RoleUser, "role to assign (must exist in internal/core/auth/roles.go)")
	fs.StringVar(&opts.tenantID, "tenant", "", "tenant id (required when TENANCY_ENABLED=true)")
	fs.BoolVar(&opts.createTenant, "create-tenant", false, "create the tenant (active) if it does not exist")
	fs.BoolVar(&opts.passwordStdin, "password-stdin", false, "read the password from stdin instead of prompting")
	fs.BoolVar(&opts.requireVerification, "require-verification", false, "leave the account pending email verification (only with AUTH_EMAIL_VERIFICATION_ENABLED)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v (the password is never passed as an argument)", fs.Args())
	}
	opts.email = strings.TrimSpace(opts.email)
	opts.role = strings.TrimSpace(opts.role)
	opts.tenantID = strings.TrimSpace(opts.tenantID)
	if opts.email == "" {
		return options{}, errors.New("--email is required")
	}
	if opts.tenantID != "" && !tenant.ValidTenantID(opts.tenantID) {
		return options{}, fmt.Errorf("invalid --tenant %q", opts.tenantID)
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

// ensureTenant makes sure the tenant exists (creating it with --create-tenant)
// so the new user can actually pass tenant validation at the HTTP edge.
func ensureTenant(ctx context.Context, deps *app.Dependencies, cfg *config.Config, opts options) error {
	repo := tenant.NewRepository(deps.DB)
	if repo == nil {
		return errors.New("tenant repository unavailable (POSTGRES_ENABLED=false?)")
	}
	record, err := repo.Get(ctx, opts.tenantID)
	switch {
	case errors.Is(err, tenant.ErrTenantNotFound):
		if !opts.createTenant {
			if cfg.Tenancy.Validate {
				return fmt.Errorf("tenant %q does not exist; pass --create-tenant to create it", opts.tenantID)
			}
			return nil
		}
		if _, err := repo.Create(ctx, tenant.Record{ID: opts.tenantID, Slug: opts.tenantID, Name: opts.tenantID, Status: tenant.StatusActive}); err != nil {
			return err
		}
		return nil
	case err != nil:
		return err
	case !record.Active():
		return fmt.Errorf("tenant %q is not active", opts.tenantID)
	}
	return nil
}
