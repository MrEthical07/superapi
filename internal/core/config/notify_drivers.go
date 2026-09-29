package config

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// notifyDrivers is the set of NOTIFY_DRIVER values config lint accepts. The
// built-in drivers add themselves; a project's own driver joins through
// notify.RegisterDriver (which calls RegisterNotifyDriver), so a new driver
// never requires editing this package.
var notifyDrivers = struct {
	mu    sync.RWMutex
	names map[string]struct{}
}{names: map[string]struct{}{NotifyDriverNoop: {}, NotifyDriverLog: {}}}

// RegisterNotifyDriver makes config lint accept name as a NOTIFY_DRIVER value.
// Names are compared case-insensitively. Most code should call
// notify.RegisterDriver instead, which registers the factory as well.
func RegisterNotifyDriver(name string) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		panic("config: RegisterNotifyDriver: empty driver name")
	}
	notifyDrivers.mu.Lock()
	defer notifyDrivers.mu.Unlock()
	notifyDrivers.names[name] = struct{}{}
}

// NotifyDriverNames returns every accepted NOTIFY_DRIVER value, sorted.
func NotifyDriverNames() []string {
	notifyDrivers.mu.RLock()
	defer notifyDrivers.mu.RUnlock()
	out := make([]string, 0, len(notifyDrivers.names))
	for name := range notifyDrivers.names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func notifyDriverKnown(name string) bool {
	notifyDrivers.mu.RLock()
	defer notifyDrivers.mu.RUnlock()
	_, ok := notifyDrivers.names[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func (c *Config) lintNotifyDriver() error {
	if notifyDriverKnown(c.Notify.Driver) {
		return nil
	}
	return fmt.Errorf("invalid notify driver: %q (valid: %s)", c.Notify.Driver, strings.Join(NotifyDriverNames(), ", "))
}
