package auth

import (
	"time"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/modulekit"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

// Module exposes the goAuth authentication lifecycle under /api/v1/auth.
type Module struct {
	runtime  modulekit.Runtime
	rateRule ratelimit.Rule
	features features
	svc      *service
}

// New constructs the auth module.
func New() *Module { return &Module{} }

var _ app.Module = (*Module)(nil)
var _ app.DependencyBinder = (*Module)(nil)

// Name returns module registry name.
func (m *Module) Name() string { return "auth" }

// BindDependencies captures runtime dependencies, feature flags, and the
// default per-user rate-limit rule for authenticated routes.
func (m *Module) BindDependencies(deps *app.Dependencies) {
	m.runtime = modulekit.New(deps)
	m.rateRule = ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ratelimit.ScopeUser}
	if deps == nil {
		m.features = features{}
		m.svc = newService(nil, nil, nil, m.features)
		return
	}

	m.features = features{
		Registration:      deps.Auth.RegistrationEnabled,
		AutoLogin:         deps.Auth.RegistrationEnabled && deps.Auth.RegistrationAutoLogin,
		PasswordReset:     deps.Auth.PasswordResetEnabled,
		EmailVerification: deps.Auth.EmailVerificationEnabled,
		TOTP:              deps.Auth.TOTPEnabled,
	}

	var msg messenger
	if deps.Notifier != nil {
		msg = deps.Notifier
	}
	m.svc = newService(deps.AuthEngine, newAccountRepository(deps.AuthUsers), msg, m.features)

	if deps.RateLimit.DefaultLimit > 0 {
		m.rateRule.Limit = deps.RateLimit.DefaultLimit
	}
	if deps.RateLimit.DefaultWindow > 0 {
		m.rateRule.Window = deps.RateLimit.DefaultWindow
	}
}
