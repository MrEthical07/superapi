// Package features is the one place that lists the optional features compiled
// into this project. Each feature plugs into core through app.Hooks and is
// switched on by its own settings; see docs/architecture.md, "Optional
// features", for the hooks and how to add one.
package features

import "github.com/MrEthical07/superapi/internal/core/app"

// All returns the optional features in registration order. Adding a feature is
// one line here; removing one is deleting its line and its directory.
func All() []app.Feature {
	return []app.Feature{
		// template:begin tenancy
		tenancyFeature(),
		// template:end tenancy
	}
}
