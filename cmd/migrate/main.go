package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

type action string

const (
	actionUp      action = "up"
	actionDown    action = "down"
	actionVersion action = "version"
	actionForce   action = "force"
)

type cliCommand struct {
	showHelp bool
	action   action
	path     string
	steps    int
	version  int
}

type runner interface {
	Up() (bool, error)
	Down(steps int) (bool, error)
	Version() (db.MigrationVersion, error)
	Force(version int) error
	Close() error
}

type runDeps struct {
	loadConfig func() (*config.Config, error)
	newLogger  func(cfg logx.Config) (*logx.Logger, error)
	newRunner  func(dbURL, sourceURL string) (runner, error)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, runDeps{
		loadConfig: config.Load,
		newLogger:  logx.New,
		newRunner: func(dbURL, sourceURL string) (runner, error) {
			return db.NewMigrationRunner(dbURL, sourceURL)
		},
	}))
}

func run(args []string, out io.Writer, errOut io.Writer, deps runDeps) int {
	cmd, err := parseCLI(args)
	if err != nil {
		fmt.Fprintf(errOut, "error: %v\n\n", err)
		printUsage(errOut)
		return 2
	}
	if cmd.showHelp {
		printUsage(out)
		return 0
	}

	cfg, err := deps.loadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "config load failed: %v\n", err)
		return 1
	}
	if err := cfg.Lint(); err != nil {
		fmt.Fprintf(errOut, "config lint failed: %v\n", err)
		return 1
	}
	if !cfg.Postgres.Enabled {
		fmt.Fprintf(errOut, "config error: POSTGRES_ENABLED must be true for migrate command\n")
		return 1
	}
	if strings.TrimSpace(cfg.Postgres.URL) == "" {
		fmt.Fprintf(errOut, "config error: POSTGRES_URL is required for migrate command\n")
		return 1
	}

	logger, err := deps.newLogger(logx.Config{Level: cfg.Log.Level, Format: cfg.Log.Format})
	if err != nil {
		fmt.Fprintf(errOut, "logger init failed: %v\n", err)
		return 1
	}

	sourceURL, err := db.MigrationSourceURL(cmd.path)
	if err != nil {
		logger.Error().Err(err).Msg("resolve migration source failed")
		return 1
	}

	r, err := deps.newRunner(cfg.Postgres.URL, sourceURL)
	if err != nil {
		logger.Error().
			Err(err).
			Str("action", string(cmd.action)).
			Str("source", sourceURL).
			Msg("migration runner init failed")
		return 1
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			logger.Error().Err(closeErr).Msg("migration runner close failed")
		}
	}()

	logger.Info().
		Str("action", string(cmd.action)).
		Str("source", sourceURL).
		Int("steps", cmd.steps).
		Int("version", cmd.version).
		Msg("migration command started")

	if err := executeCommand(cmd, r, logger); err != nil {
		logger.Error().Err(err).Str("action", string(cmd.action)).Msg("migration command failed")
		return 1
	}

	return 0
}

func executeCommand(cmd cliCommand, r runner, logger *logx.Logger) error {
	switch cmd.action {
	case actionUp:
		noChange, err := r.Up()
		if err != nil {
			return err
		}
		result := "success"
		if noChange {
			result = "no_change"
		}
		logger.Info().Str("action", string(cmd.action)).Str("result", result).Msg("migration command completed")
		return nil

	case actionDown:
		noChange, err := r.Down(cmd.steps)
		if err != nil {
			return err
		}
		result := "success"
		if noChange {
			result = "no_change"
		}
		logger.Info().
			Str("action", string(cmd.action)).
			Int("steps", cmd.steps).
			Str("result", result).
			Msg("migration command completed")
		return nil

	case actionVersion:
		v, err := r.Version()
		if err != nil {
			return err
		}
		if !v.HasVersion {
			logger.Info().Str("action", string(cmd.action)).Str("result", "no_version").Msg("migration command completed")
			return nil
		}
		logger.Info().
			Str("action", string(cmd.action)).
			Str("result", "success").
			Uint("version", v.Version).
			Bool("dirty", v.Dirty).
			Msg("migration command completed")
		return nil

	case actionForce:
		if err := r.Force(cmd.version); err != nil {
			return err
		}
		logger.Info().
			Str("action", string(cmd.action)).
			Int("version", cmd.version).
			Str("result", "success").
			Msg("migration command completed")
		return nil

	default:
		return fmt.Errorf("unsupported action: %q", cmd.action)
	}
}

func parseCLI(args []string) (cliCommand, error) {
	if len(args) == 0 {
		return cliCommand{showHelp: true}, nil
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		return cliCommand{showHelp: true}, nil
	}

	sub := args[0]
	switch sub {
	case string(actionUp):
		fs := flag.NewFlagSet(sub, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		path := fs.String("path", "db/migrations", "Path to migrations directory")
		if err := fs.Parse(args[1:]); err != nil {
			return cliCommand{}, err
		}
		if fs.NArg() != 0 {
			return cliCommand{}, errors.New("up does not accept positional arguments")
		}
		return cliCommand{action: actionUp, path: *path}, nil

	case string(actionDown):
		fs := flag.NewFlagSet(sub, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		path := fs.String("path", "db/migrations", "Path to migrations directory")
		steps := fs.Int("steps", 1, "Number of migration steps to roll back")
		if err := fs.Parse(args[1:]); err != nil {
			return cliCommand{}, err
		}
		if fs.NArg() != 0 {
			return cliCommand{}, errors.New("down does not accept positional arguments")
		}
		if *steps <= 0 {
			return cliCommand{}, errors.New("--steps must be > 0")
		}
		return cliCommand{action: actionDown, path: *path, steps: *steps}, nil

	case string(actionVersion):
		fs := flag.NewFlagSet(sub, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		path := fs.String("path", "db/migrations", "Path to migrations directory")
		if err := fs.Parse(args[1:]); err != nil {
			return cliCommand{}, err
		}
		if fs.NArg() != 0 {
			return cliCommand{}, errors.New("version does not accept positional arguments")
		}
		return cliCommand{action: actionVersion, path: *path}, nil

	case string(actionForce):
		fs := flag.NewFlagSet(sub, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		path := fs.String("path", "db/migrations", "Path to migrations directory")
		version := fs.Int("version", -1, "Version to force")
		if err := fs.Parse(args[1:]); err != nil {
			return cliCommand{}, err
		}
		if fs.NArg() != 0 {
			return cliCommand{}, errors.New("force does not accept positional arguments")
		}
		if *version < 0 {
			return cliCommand{}, errors.New("--version must be >= 0")
		}
		return cliCommand{action: actionForce, path: *path, version: *version}, nil

	default:
		return cliCommand{}, fmt.Errorf("unknown command: %q", sub)
	}
}

func printUsage(out io.Writer) {
	_, _ = fmt.Fprintln(out, "Usage: migrate <command> [flags]")
	_, _ = fmt.Fprintln(out, "")
	_, _ = fmt.Fprintln(out, "Commands:")
	_, _ = fmt.Fprintln(out, "  up [--path=db/migrations]")
	_, _ = fmt.Fprintln(out, "  down [--steps=1] [--path=db/migrations]")
	_, _ = fmt.Fprintln(out, "  version [--path=db/migrations]")
	_, _ = fmt.Fprintln(out, "  force --version=<N> [--path=db/migrations]")
	_, _ = fmt.Fprintln(out, "")
	_, _ = fmt.Fprintln(out, "Environment:")
	_, _ = fmt.Fprintln(out, "  POSTGRES_ENABLED=true")
	_, _ = fmt.Fprintln(out, "  POSTGRES_URL=postgres://user:pass@host:5432/db?sslmode=disable")
}

func init() {
	log.SetFlags(0)
}
