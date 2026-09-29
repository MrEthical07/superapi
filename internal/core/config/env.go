package config

import (
	"os"
	"time"
)

// The Env* readers let an optional feature load its own settings the same way
// Load reads the core ones: empty or unparsable values fall back to the given
// default, and APP_PROFILE defaults apply while Load runs. A feature keeps its
// settings in its own package and reads them here; superapi-verify discovers
// the keys from these calls, so a key missing from .env.example or
// docs/environment-variables.md is still reported.

// EnvString returns the string value of key, or fallback when unset or empty.
func EnvString(key, fallback string) string { return getenv(key, fallback) }

// EnvBool returns the boolean value of key, or fallback when unset or invalid.
func EnvBool(key string, fallback bool) bool { return getBool(key, fallback) }

// EnvInt returns the integer value of key, or fallback when unset or invalid.
func EnvInt(key string, fallback int) int { return getInt(key, fallback) }

// EnvDuration returns the duration value of key, or fallback when unset or
// invalid.
func EnvDuration(key string, fallback time.Duration) time.Duration {
	return getDuration(key, fallback)
}

// EnvCSV returns the comma-separated value of key with blanks removed, or
// fallback when unset or empty.
func EnvCSV(key string, fallback []string) []string { return getCSV(key, fallback) }

// EnvDeprecated reports whether a deprecated key is set. It exists so the
// startup warning can be raised without the key counting as a live setting:
// superapi-verify requires it in docs/environment-variables.md (to explain the
// deprecation) but not in .env.example.
func EnvDeprecated(key string) bool {
	_, ok := os.LookupEnv(key)
	return ok
}
