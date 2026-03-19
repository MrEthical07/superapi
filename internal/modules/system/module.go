package system

import (
	"time"

	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/modulekit"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

type Module struct {
	runtime  modulekit.Runtime
	rateRule ratelimit.Rule
}

func New() *Module { return &Module{} }

var _ app.Module = (*Module)(nil)
var _ app.DependencyBinder = (*Module)(nil)

func (m *Module) Name() string { return "system" }

func (m *Module) BindDependencies(deps *app.Dependencies) {
	m.runtime = modulekit.New(deps)
	if deps == nil {
		m.rateRule = ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ratelimit.ScopeUser}
		return
	}

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
