package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/config"
)

// maxReportedFailures bounds how many failing users are listed at the end.
const maxReportedFailures = 20

type options struct {
	batchSize int
	dryRun    bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func parseFlags(args []string, stderr io.Writer) (options, error) {
	fs := flag.NewFlagSet("rotatetotpkey", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts options
	fs.IntVar(&opts.batchSize, "batch-size", 200, "rows to read and re-encrypt per batch (1-10000)")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "report what would be re-encrypted without writing anything")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if opts.batchSize < 1 || opts.batchSize > 10_000 {
		return options{}, fmt.Errorf("--batch-size must be between 1 and 10000, got %d", opts.batchSize)
	}
	return opts, nil
}

func run(args []string, stdout, stderr io.Writer) int {
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
	if !cfg.Auth.Enabled || !cfg.Auth.TOTPEnabled {
		fmt.Fprintln(stderr, "error: AUTH_ENABLED=true and AUTH_TOTP_ENABLED=true are required (with the same key settings the API uses)")
		return 1
	}
	cipher, err := app.NewTOTPCipher(cfg.Auth)
	if err != nil {
		fmt.Fprintln(stderr, "error: totp keys:", err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps, err := app.NewDependencies(ctx, cfg)
	if err != nil {
		fmt.Fprintln(stderr, "error: init dependencies:", err)
		return 1
	}
	defer deps.Close()

	res, err := rotate(ctx, auth.NewMFARepository(deps.DB), cipher, opts, stdout)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	res.print(stdout, opts.dryRun)
	if res.Failed > 0 {
		fmt.Fprintf(stderr, "error: %d secret(s) could not be re-encrypted; they are unchanged. Fix the cause (usually a key missing from AUTH_TOTP_ENCRYPTION_KEYS / AUTH_TOTP_ENCRYPTION_KEY) and run again.\n", res.Failed)
		return 1
	}
	return 0
}

// failure is one secret that could not be re-encrypted.
type failure struct {
	UserID string
	Reason string
}

// result summarizes a rotation run.
type result struct {
	// Scanned is every stored secret looked at.
	Scanned int
	// Current secrets were already sealed under the active key (format v2).
	Current int
	// Rotated secrets were re-encrypted under the active key (or, in a dry
	// run, would be).
	Rotated int
	// Skipped secrets changed while the run was in progress (a user
	// re-enrolled, or a login re-encrypted them first), so the stale write was
	// not applied.
	Skipped int
	// Failed secrets could not be decrypted or written; they are unchanged.
	Failed   int
	Failures []failure
}

func (r result) print(w io.Writer, dryRun bool) {
	verb := "re-encrypted"
	if dryRun {
		verb = "would be re-encrypted"
	}
	fmt.Fprintf(w, "done: %d secret(s) scanned; %d %s; %d already under the active key; %d changed concurrently (skipped); %d failed\n",
		r.Scanned, r.Rotated, verb, r.Current, r.Skipped, r.Failed)
	for i, f := range r.Failures {
		if i == maxReportedFailures {
			fmt.Fprintf(w, "  ... and %d more\n", len(r.Failures)-maxReportedFailures)
			break
		}
		fmt.Fprintf(w, "  failed user %s: %s\n", f.UserID, f.Reason)
	}
}

// rotate re-encrypts every stored TOTP secret that is not already sealed under
// the cipher's active key, batch by batch, writing a progress line after each
// batch. Each write is a compare-and-swap, so a secret changed by someone else
// in the meantime is left alone. It never prints a secret or a ciphertext.
func rotate(ctx context.Context, repo auth.MFARepository, cipher auth.RotatableCipher, opts options, progress io.Writer) (result, error) {
	var (
		res   result
		after string
		start = time.Now()
	)
	for batch := 1; ; batch++ {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		rows, err := repo.ListTOTPSecrets(ctx, after, opts.batchSize)
		if err != nil {
			return res, fmt.Errorf("read batch %d: %w", batch, err)
		}
		if len(rows) == 0 {
			return res, nil
		}
		for _, row := range rows {
			res.Scanned++
			if !cipher.NeedsRotation(row.Ciphertext) {
				res.Current++
				continue
			}
			plaintext, err := cipher.Open(row.UserID, row.Ciphertext)
			if err != nil {
				res.fail(row.UserID, err)
				continue
			}
			if opts.dryRun {
				res.Rotated++
				continue
			}
			next, err := cipher.Seal(row.UserID, plaintext)
			if err != nil {
				res.fail(row.UserID, err)
				continue
			}
			swapped, err := repo.RotateTOTPSecret(ctx, "", row.UserID, row.Ciphertext, next)
			switch {
			case err != nil:
				res.fail(row.UserID, err)
			case !swapped:
				res.Skipped++
			default:
				res.Rotated++
			}
		}
		after = rows[len(rows)-1].UserID
		fmt.Fprintf(progress, "batch %d: scanned=%d rotated=%d current=%d skipped=%d failed=%d (%s)\n",
			batch, res.Scanned, res.Rotated, res.Current, res.Skipped, res.Failed, time.Since(start).Round(time.Millisecond))
	}
}

func (r *result) fail(userID string, err error) {
	r.Failed++
	r.Failures = append(r.Failures, failure{UserID: userID, Reason: err.Error()})
}
