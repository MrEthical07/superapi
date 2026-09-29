package tenancy

import (
	"flag"
	"net/http"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/policy"
)

var (
	_ app.Feature = (*Feature)(nil)
	_ app.UserCLI = (*Feature)(nil)
)

// Feature is the tenancy feature. Register it with internal/features (one line)
// and it loads its own TENANCY_* settings, then plugs into core through
// app.Hooks. See docs/architecture.md, "Optional features", for the pattern;
// this is the worked example.
type Feature struct {
	cfg Config
}

// New returns the feature.
func New() *Feature { return &Feature{} }

// Name implements app.Feature.
func (f *Feature) Name() string { return "tenancy" }

// Config returns the settings loaded by Load.
func (f *Feature) Config() Config { return f.cfg }

// Load implements app.Feature. It reads and lints TENANCY_*, and returns the
// hooks:
//
//   - always: the principal's tenant attribute (goAuth's default tenant "0"
//     when tenancy is off) and the tenancy route rules;
//   - with TENANCY_ENABLED=true: goAuth's multi-tenant mode, the provider and
//     user repository decorators, the tenant token binding check, and the
//     resolver middleware, installed innermost just before routing so its
//     rejections are still logged, traced, time-bounded and carry security
//     headers, and CORS preflight requests are answered before a tenant is
//     demanded.
func (f *Feature) Load(core *config.Config) (*app.Hooks, error) {
	cfg := LoadConfig()
	if err := cfg.Lint(core); err != nil {
		return nil, err
	}
	f.cfg = cfg
	active.Store(cfg.Enabled)

	hooks := &app.Hooks{
		Deprecations:  deprecations(),
		AuthExtension: AuthExtension(cfg.Enabled),
		RouteRules:    []policy.RouteRule{RouteRule(cfg.Enabled)},
	}
	if !cfg.Enabled {
		return hooks, nil
	}

	hooks.GoAuthConfig = EnableMultiTenant
	hooks.UserProvider = func(d *app.Dependencies, base *auth.StoreUserProvider) goauth.UserProvider {
		return NewProvider(base, NewUserStore(d.DB))
	}
	hooks.UserRepository = func(d *app.Dependencies, base auth.UserRepository) auth.UserRepository {
		return WrapUserRepository(base, NewUserStore(d.DB))
	}
	hooks.Middleware = func(d *app.Dependencies) func(http.Handler) http.Handler {
		return f.middleware(d)
	}
	return hooks, nil
}

// middleware builds the resolver middleware.
func (f *Feature) middleware(d *app.Dependencies) func(http.Handler) http.Handler {
	exempt := append([]string(nil), f.cfg.ExemptPaths...)
	if d != nil && d.Metrics != nil && d.Metrics.Enabled() {
		exempt = append(exempt, d.Metrics.Path())
	}

	resolverCfg := ResolverConfig{
		Resolver:    f.cfg.Resolver,
		Header:      f.cfg.Header,
		BaseDomain:  f.cfg.BaseDomain,
		ExemptPaths: exempt,
		CacheTTL:    f.cfg.ValidateCacheTTL,
	}
	if f.cfg.Validate && d != nil && d.DB != nil {
		resolverCfg.Directory = NewRepository(d.DB)
	}
	return Middleware(resolverCfg)
}

// UserFlags implements app.UserCLI: --tenant and --create-tenant for
// cmd/createuser.
func (f *Feature) UserFlags(fs *flag.FlagSet) app.UserStep {
	step := &userStep{}
	fs.StringVar(&step.tenantID, "tenant", "", "tenant id (required when TENANCY_ENABLED=true)")
	fs.BoolVar(&step.createTenant, "create-tenant", false, "create the tenant (active) if it does not exist")
	return step
}

// EnableMultiTenant is the goAuth config mutator (app.Hooks.GoAuthConfig) that
// switches goAuth to multi-tenant mode: every user lookup is scoped to the
// tenant attached to the request context, and Build fails unless the provider
// implements goauth.TenantAwareUserProvider.
//
// MultiTenant.EnforceIsolation and MultiTenant.TenantHeader are deprecated
// no-ops in goAuth v0.5.0 and are intentionally left unset (the header is owned
// by the resolver, TENANCY_HEADER). Identifier uniqueness is owned by the users
// schema (see docs/multi-tenancy.md), so
// Account.AllowDuplicateIdentifierAcrossTenants is left at its default.
func EnableMultiTenant(c *goauth.Config) {
	c.MultiTenant.Enabled = true
	// goAuth's DefaultConfig pre-fills this deprecated no-op field, which would
	// trigger the tenant_header_noop lint once multi-tenancy is on.
	c.MultiTenant.TenantHeader = "" //nolint:staticcheck // clearing the deprecated no-op field is the point
}
