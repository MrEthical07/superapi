package notify

import (
	"fmt"
	"strings"
	"sync"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

// DriverFactory builds a Notifier for a NOTIFY_DRIVER value. It receives the
// loaded notify config, the APP_ENV value, and the application logger.
// Drivers with their own settings read them from the environment themselves
// (or from the process's own config) and validate them here, returning an
// error to fail startup.
type DriverFactory func(cfg config.NotifyConfig, env string, log *logx.Logger) (Notifier, error)

var drivers = struct {
	mu        sync.RWMutex
	factories map[string]DriverFactory
}{factories: make(map[string]DriverFactory)}

// RegisterDriver makes name selectable through NOTIFY_DRIVER. Config lint
// accepts the name as soon as it is registered, so adding SendGrid, SES or any
// other provider is a matter of registering it from the project's own package
// (typically in an init function that main imports for side effects):
//
//	func init() {
//		notify.RegisterDriver("sendgrid", func(cfg config.NotifyConfig, env string, log *logx.Logger) (notify.Notifier, error) {
//			return newSendGrid(os.Getenv("SENDGRID_API_KEY"))
//		})
//	}
//
// Names are case-insensitive. Like database/sql.Register it panics on an empty
// name, a nil factory, or a duplicate, because those are programming errors
// that should fail at startup.
func RegisterDriver(name string, factory DriverFactory) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		panic("notify: RegisterDriver: empty driver name")
	}
	if factory == nil {
		panic("notify: RegisterDriver: nil factory for driver " + name)
	}
	drivers.mu.Lock()
	defer drivers.mu.Unlock()
	if _, dup := drivers.factories[name]; dup {
		panic("notify: RegisterDriver: driver " + name + " registered twice")
	}
	drivers.factories[name] = factory
	config.RegisterNotifyDriver(name)
}

func lookupDriver(name string) (DriverFactory, bool) {
	drivers.mu.RLock()
	defer drivers.mu.RUnlock()
	f, ok := drivers.factories[strings.ToLower(strings.TrimSpace(name))]
	return f, ok
}

// New builds the notifier selected by cfg.Driver from the driver registry.
// The built-in drivers are noop (the default), log and smtp.
func New(cfg config.NotifyConfig, env string, log *logx.Logger) (Notifier, error) {
	name := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if name == "" {
		name = config.NotifyDriverNoop
	}
	factory, ok := lookupDriver(name)
	if !ok {
		return nil, fmt.Errorf("unknown notify driver %q (registered: %s)", cfg.Driver, strings.Join(config.NotifyDriverNames(), ", "))
	}
	return factory(cfg, env, log)
}

func init() {
	RegisterDriver(config.NotifyDriverNoop, func(config.NotifyConfig, string, *logx.Logger) (Notifier, error) {
		return Noop{}, nil
	})
	RegisterDriver(config.NotifyDriverLog, func(cfg config.NotifyConfig, env string, log *logx.Logger) (Notifier, error) {
		show := cfg.LogSecrets && strings.EqualFold(strings.TrimSpace(env), "dev")
		return NewLog(log, show), nil
	})
}
