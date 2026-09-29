package config

import (
	"strings"
	"testing"
)

func TestLintNotifyDriverListsValidDrivers(t *testing.T) {
	cfg := &Config{ServiceName: "svc"}
	cfg.Notify.Driver = "carrier-pigeon"
	err := cfg.lintNotifyDriver()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"carrier-pigeon", "noop", "log"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err, want)
		}
	}

	RegisterNotifyDriver("Custom-Mailer")
	cfg.Notify.Driver = "CUSTOM-mailer"
	if err := cfg.lintNotifyDriver(); err != nil {
		t.Fatalf("a registered driver must be accepted: %v", err)
	}
}
