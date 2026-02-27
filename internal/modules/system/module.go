package system

import (
	"time"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

type Module struct {
	authProvider auth.Provider
	authMode     auth.Mode
	limiter      ratelimit.Limiter
	rateRule     ratelimit.Rule
}

func New() *Module { return &Module{} }

var _ app.Module = (*Module)(nil)
var _ app.DependencyBinder = (*Module)(nil)

func (m *Module) Name() string { return "system" }

func (m *Module) BindDependencies(deps *app.Dependencies) {
	if deps == nil {
		m.authProvider = auth.NewDisabledProvider()
		m.authMode = auth.ModeHybrid
		m.rateRule = ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ratelimit.ScopeUser}
		return
	}
	m.authProvider = deps.Auth
	m.authMode = deps.AuthMode
	m.limiter = deps.Limiter

	limit := deps.RateLimit.DefaultLimit
	if limit <= 0 {
		limit = 10
	}
	window := deps.RateLimit.DefaultWindow
	if window <= 0 {
		window = time.Minute
	}
	m.rateRule = ratelimit.Rule{
		Limit:  limit,
		Window: window,
		Scope:  ratelimit.ScopeUser,
	}
}
