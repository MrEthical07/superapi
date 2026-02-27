package system

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/core/auth"
)

type Module struct {
	authProvider auth.Provider
	authMode     auth.Mode
}

func New() *Module { return &Module{} }

var _ app.Module = (*Module)(nil)
var _ app.DependencyBinder = (*Module)(nil)

func (m *Module) Name() string { return "system" }

func (m *Module) BindDependencies(deps *app.Dependencies) {
	if deps == nil {
		m.authProvider = auth.NewDisabledProvider()
		m.authMode = auth.ModeHybrid
		return
	}
	m.authProvider = deps.Auth
	m.authMode = deps.AuthMode
}
