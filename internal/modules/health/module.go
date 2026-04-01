package health

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/metrics"
	"github.com/MrEthical07/superapi/internal/core/readiness"
)

// Module exposes liveness/readiness endpoints and optional readiness metrics.
type Module struct {
	readiness *readiness.Service
	metrics   *metrics.Service
}

// New constructs the health module.
func New() *Module { return &Module{} }

var _ app.Module = (*Module)(nil)
var _ app.DependencyBinder = (*Module)(nil)

// Name returns module registry name.
func (m *Module) Name() string { return "health" }

// BindDependencies injects readiness and metrics services into module runtime.
func (m *Module) BindDependencies(deps *app.Dependencies) {
	if deps == nil {
		m.readiness = nil
		m.metrics = nil
		return
	}
	m.readiness = deps.Readiness
	m.metrics = deps.Metrics
}
