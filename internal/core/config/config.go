package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env         string
	ServiceName string
	HTTP        HTTPConfig
	Log         LogConfig
}

// LogConfig holds structured logging configuration.
type LogConfig struct {
	// Level is the minimum log level: debug, info, warn, error, fatal.
	Level string
	// Format is the output format: "json" (default) or "text" (dev console).
	Format string
}

type HTTPConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
	Middleware        HTTPMiddlewareConfig
}

type HTTPMiddlewareConfig struct {
	RequestIDEnabled       bool
	RecovererEnabled       bool
	MaxBodyBytes           int64
	SecurityHeadersEnabled bool
	RequestTimeout         time.Duration
}

func Load() (*Config, error) {
	cfg := &Config{
		Env:         getenv("APP_ENV", "dev"),
		ServiceName: getenv("APP_SERVICE_NAME", "api-template"),
		HTTP: HTTPConfig{
			Addr:              getenv("HTTP_ADDR", ":8080"),
			ReadHeaderTimeout: getDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       getDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      getDuration("HTTP_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:       getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout:   getDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
			MaxHeaderBytes:    getInt("HTTP_MAX_HEADER_BYTES", 1<<20), // 1 MiB
			Middleware: HTTPMiddlewareConfig{
				RequestIDEnabled:       getBool("HTTP_MIDDLEWARE_REQUEST_ID_ENABLED", true),
				RecovererEnabled:       getBool("HTTP_MIDDLEWARE_RECOVERER_ENABLED", true),
				MaxBodyBytes:           getInt64("HTTP_MIDDLEWARE_MAX_BODY_BYTES", 0),
				SecurityHeadersEnabled: getBool("HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED", false),
				RequestTimeout:         getDuration("HTTP_MIDDLEWARE_REQUEST_TIMEOUT", 0),
			},
		},
		Log: LogConfig{
			Level:  getenv("LOG_LEVEL", "info"),
			Format: getenv("LOG_FORMAT", "json"),
		},
	}

	return cfg, nil
}

func (c *Config) Lint() error {
	if c.ServiceName == "" {
		return errors.New("service name cannot be empty")
	}
	if c.HTTP.Addr == "" {
		return errors.New("http addr cannot be empty")
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("http read header timeout must be > 0")
	}
	if c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout <= 0 {
		return fmt.Errorf("http read/write timeout must be > 0")
	}
	if c.HTTP.IdleTimeout <= 0 {
		return fmt.Errorf("http idle timeout must be > 0")
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		return fmt.Errorf("http shutdown timeout must be > 0")
	}
	if c.HTTP.MaxHeaderBytes < 4096 {
		return fmt.Errorf("http max header bytes too low: %d", c.HTTP.MaxHeaderBytes)
	}
	if c.HTTP.Middleware.MaxBodyBytes < 0 {
		return fmt.Errorf("http middleware max body bytes must be >= 0")
	}
	if c.HTTP.Middleware.RequestTimeout < 0 {
		return fmt.Errorf("http middleware request timeout must be >= 0")
	}

	if v, ok := os.LookupEnv("HTTP_MIDDLEWARE_REQUEST_ID_ENABLED"); ok {
		if _, err := strconv.ParseBool(v); err != nil {
			return fmt.Errorf("invalid HTTP_MIDDLEWARE_REQUEST_ID_ENABLED: %q", v)
		}
	}
	if v, ok := os.LookupEnv("HTTP_MIDDLEWARE_RECOVERER_ENABLED"); ok {
		if _, err := strconv.ParseBool(v); err != nil {
			return fmt.Errorf("invalid HTTP_MIDDLEWARE_RECOVERER_ENABLED: %q", v)
		}
	}
	if v, ok := os.LookupEnv("HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED"); ok {
		if _, err := strconv.ParseBool(v); err != nil {
			return fmt.Errorf("invalid HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED: %q", v)
		}
	}
	if v, ok := os.LookupEnv("HTTP_MIDDLEWARE_MAX_BODY_BYTES"); ok {
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return fmt.Errorf("invalid HTTP_MIDDLEWARE_MAX_BODY_BYTES: %q", v)
		}
	}
	if v, ok := os.LookupEnv("HTTP_MIDDLEWARE_REQUEST_TIMEOUT"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid HTTP_MIDDLEWARE_REQUEST_TIMEOUT: %q", v)
		}
		if d < 0 {
			return fmt.Errorf("http middleware request timeout must be >= 0")
		}
	}

	switch c.Log.Level {
	case "debug", "info", "warn", "warning", "error", "fatal", "":
		// valid
	default:
		return fmt.Errorf("invalid log level: %q (valid: debug, info, warn, error, fatal)", c.Log.Level)
	}

	switch c.Log.Format {
	case "json", "text", "":
		// valid
	default:
		return fmt.Errorf("invalid log format: %q (valid: json, text)", c.Log.Format)
	}

	return nil
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func getBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
