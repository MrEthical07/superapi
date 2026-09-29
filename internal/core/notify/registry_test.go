package notify

import (
	"context"
	"strings"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

type stubNotifier struct{ name string }

func (stubNotifier) SendPasswordReset(context.Context, string, string) error     { return nil }
func (stubNotifier) SendEmailVerification(context.Context, string, string) error { return nil }

func mustPanic(t *testing.T, wantContains string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic")
		}
		if msg, _ := r.(string); !strings.Contains(msg, wantContains) {
			t.Fatalf("panic = %v, want it to mention %q", r, wantContains)
		}
	}()
	fn()
}

func TestBuiltInDriversAreRegistered(t *testing.T) {
	names := strings.Join(config.NotifyDriverNames(), ",")
	wanted := []string{"noop", "log"}
	// template:begin smtp
	wanted = append(wanted, "smtp")
	// template:end smtp
	for _, want := range wanted {
		if !strings.Contains(names, want) {
			t.Fatalf("driver %q missing from %s", want, names)
		}
	}
}

// A project adds a driver from its own package: New builds it and config lint
// accepts the name, with no edit to notify.New or the config package.
func TestRegisterDriverExtendsNewAndConfigLint(t *testing.T) {
	var gotEnv string
	var gotCfg config.NotifyConfig
	RegisterDriver("Test-Provider", func(cfg config.NotifyConfig, env string, _ *logx.Logger) (Notifier, error) {
		gotCfg, gotEnv = cfg, env
		return stubNotifier{name: "provider"}, nil
	})

	cfg := config.NotifyConfig{Driver: "TEST-provider"}
	n, err := New(cfg, "staging", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s, ok := n.(stubNotifier); !ok || s.name != "provider" {
		t.Fatalf("driver = %#v", n)
	}
	if gotEnv != "staging" || gotCfg.Driver != "TEST-provider" {
		t.Fatalf("factory received env=%q cfg=%+v", gotEnv, gotCfg)
	}

	if !strings.Contains(strings.Join(config.NotifyDriverNames(), ","), "test-provider") {
		t.Fatalf("config does not know the registered driver: %v", config.NotifyDriverNames())
	}
	full := &config.Config{ServiceName: "svc", Env: "dev"}
	full.Notify = config.NotifyConfig{Driver: "test-provider", Timeout: 1}
	if err := full.Lint(); err != nil && strings.Contains(err.Error(), "invalid notify driver") {
		t.Fatalf("lint must accept a registered driver: %v", err)
	}
}

func TestRegisterDriverFactoryErrorPropagates(t *testing.T) {
	RegisterDriver("failing-provider", func(config.NotifyConfig, string, *logx.Logger) (Notifier, error) {
		return nil, context.DeadlineExceeded
	})
	if _, err := New(config.NotifyConfig{Driver: "failing-provider"}, "dev", nil); err == nil {
		t.Fatal("a factory error must fail New")
	}
}

func TestRegisterDriverRejectsMisuse(t *testing.T) {
	ok := func(config.NotifyConfig, string, *logx.Logger) (Notifier, error) { return Noop{}, nil }

	mustPanic(t, "empty driver name", func() { RegisterDriver("  ", ok) })
	mustPanic(t, "nil factory", func() { RegisterDriver("nil-factory", nil) })
	mustPanic(t, "registered twice", func() { RegisterDriver("NOOP", ok) })
	mustPanic(t, "registered twice", func() { RegisterDriver("Log", ok) })
}

func TestNewUnknownDriverListsWhatIsRegistered(t *testing.T) {
	_, err := New(config.NotifyConfig{Driver: "no-such-driver"}, "dev", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"no-such-driver", "noop", "log"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err, want)
		}
	}
}
