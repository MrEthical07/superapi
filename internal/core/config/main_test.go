package config

import (
	"os"
	"strings"
	"testing"
)

// TestMain keeps Load/Lint tests hermetic with respect to tenancy settings.
// The quality gate also runs the suite with TENANCY_ENABLED=true to exercise
// tenancy code paths elsewhere; config tests assert defaults and set any
// TENANCY_* values they need explicitly (see tenancy_test.go).
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if key, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(key, "TENANCY_") {
			_ = os.Unsetenv(key)
		}
	}
	os.Exit(m.Run())
}
