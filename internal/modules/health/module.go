package health

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/metrics"
	"github.com/MrEthical07/superapi/internal/core/readiness"
)

type Module struct {
	readiness *readiness.Service
	metrics   *metrics.Service
}

func New() *Module { return &Module{} }

var _ app.Module = (*Module)(nil)
var _ app.DependencyBinder = (*Module)(nil)

func (m *Module) Name() string { return "health" }

func (m *Module) BindDependencies(deps *app.Dependencies) {
	if deps == nil {
		m.readiness = nil
		m.metrics = nil
		return
	}
	m.readiness = deps.Readiness
	m.metrics = deps.Metrics
}
