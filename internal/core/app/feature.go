package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/policy"
)

// Feature is an optional capability compiled into the binary and switched on
// by its own settings, for example multi-tenancy. Core never imports a feature:
// the project lists its features in one place (internal/features) and passes
// them to New and NewDependencies, and each feature plugs into core through
// the Hooks below. This is the pattern future optional features follow; see
// docs/architecture.md, "Optional features".
type Feature interface {
	// Name identifies the feature in errors.
	Name() string
	// Load reads and lints the feature's own environment (through config.Env*)
	// once core config is loaded. It returns the hooks to install, or nil when
	// the feature contributes nothing for this configuration, which leaves the
	// app exactly as if the feature were not compiled in.
	Load(core *config.Config) (*Hooks, error)
}

// Hooks is everything a feature can contribute. Every field is optional. The
// hooks are plain functions applied once at startup, in feature order; nothing
// is registered globally.
type Hooks struct {
	// Deprecations are startup warnings for settings the feature has retired.
	Deprecations []string
	// GoAuthConfig adjusts the goAuth configuration before the engine is built.
	GoAuthConfig auth.ConfigMutator
	// UserProvider wraps the core goAuth user provider (a decorator). The
	// wrapper must keep every optional goAuth interface the core provider
	// implements, because goAuth detects capabilities by type assertion. At
	// most one feature may set it.
	UserProvider func(d *Dependencies, base *auth.StoreUserProvider) goauth.UserProvider
	// UserRepository wraps the core auth user repository that modules receive as
	// Dependencies.AuthUsers.
	UserRepository func(d *Dependencies, base auth.UserRepository) auth.UserRepository
	// Middleware returns global middleware installed after RequestID and
	// ClientIP and just before routing (see httpx.WithFeatureMiddleware). Return
	// nil to install none.
	Middleware func(d *Dependencies) func(http.Handler) http.Handler
	// AuthExtension takes part in every AuthRequired policy: it adds principal
	// attributes and can reject a request after goAuth accepted the token.
	AuthExtension *policy.AuthExtension
	// RouteRules are extra checks the router applies to every registered route.
	RouteRules []policy.RouteRule
}

// UserCLI is implemented by a feature that adds flags and steps to
// cmd/createuser.
type UserCLI interface {
	// UserFlags registers the feature's flags and returns the step that applies
	// them once flags are parsed.
	UserFlags(fs *flag.FlagSet) UserStep
}

// UserStep is a feature's part of creating an account from the command line.
type UserStep interface {
	// Validate rejects flag combinations that do not fit the loaded config. The
	// feature has been loaded by then.
	Validate() error
	// Prepare runs once dependencies exist and before the account is created.
	// It returns the context the account is created with.
	Prepare(ctx context.Context, deps *Dependencies) (context.Context, error)
	// Summary returns extra lines for the "created user" output.
	Summary() []string
}

// loadedFeature pairs a feature with what Load returned.
type loadedFeature struct {
	feature Feature
	hooks   *Hooks
}

func loadFeatures(cfg *config.Config, features []Feature) ([]loadedFeature, error) {
	out := make([]loadedFeature, 0, len(features))
	providers := 0
	for _, f := range features {
		if f == nil {
			continue
		}
		hooks, err := f.Load(cfg)
		if err != nil {
			return nil, fmt.Errorf("feature %s: %w", f.Name(), err)
		}
		if hooks == nil {
			continue
		}
		if hooks.UserProvider != nil {
			providers++
		}
		out = append(out, loadedFeature{feature: f, hooks: hooks})
	}
	if providers > 1 {
		return nil, errors.New("more than one feature wraps the goAuth user provider; only one may")
	}
	return out, nil
}

// Deprecations returns the startup warnings of every loaded feature.
func (d *Dependencies) Deprecations() []string {
	if d == nil {
		return nil
	}
	var out []string
	for _, lf := range d.features {
		out = append(out, lf.hooks.Deprecations...)
	}
	return out
}
