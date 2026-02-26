package logx

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog.Logger to provide a minimal structured logging
// abstraction for core runtime paths. Modules and middleware receive
// a *Logger via dependency injection — no global singleton.
type Logger struct {
	zl zerolog.Logger
}

// Config holds logger configuration. Populated from the application config.
type Config struct {
	// Level is the minimum log level (debug, info, warn, error, fatal).
	Level string
	// Format is the log output format: "json" (default/production) or "text" (dev console).
	Format string
}

// New creates a production-ready Logger from the given Config.
// It returns an error if the config contains an invalid log level.
func New(cfg Config) (*Logger, error) {
	return NewWithWriter(cfg, os.Stdout)
}

// NewWithWriter creates a logger writing to the provided writer.
func NewWithWriter(cfg Config, out io.Writer) (*Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	var w io.Writer = out
	if strings.EqualFold(cfg.Format, "text") {
		w = zerolog.NewConsoleWriter(func(cw *zerolog.ConsoleWriter) {
			cw.Out = out
		})
	}

	zl := zerolog.New(w).Level(level).With().Timestamp().Logger()
	return &Logger{zl: zl}, nil
}

// Info starts an info-level log event.
func (l *Logger) Info() *zerolog.Event { return l.zl.Info() }

// Warn starts a warn-level log event.
func (l *Logger) Warn() *zerolog.Event { return l.zl.Warn() }

// Error starts an error-level log event.
func (l *Logger) Error() *zerolog.Event { return l.zl.Error() }

// Fatal starts a fatal-level log event. After the event is logged, os.Exit(1) is called.
func (l *Logger) Fatal() *zerolog.Event { return l.zl.Fatal() }

// Debug starts a debug-level log event.
func (l *Logger) Debug() *zerolog.Event { return l.zl.Debug() }

// parseLevel converts a string level name to a zerolog.Level.
func parseLevel(s string) (zerolog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return zerolog.DebugLevel, nil
	case "info", "":
		return zerolog.InfoLevel, nil
	case "warn", "warning":
		return zerolog.WarnLevel, nil
	case "error":
		return zerolog.ErrorLevel, nil
	case "fatal":
		return zerolog.FatalLevel, nil
	default:
		return zerolog.NoLevel, fmt.Errorf("invalid log level: %q (valid: debug, info, warn, error, fatal)", s)
	}
}
