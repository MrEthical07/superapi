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
