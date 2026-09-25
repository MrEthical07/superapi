package system

import (
	"github.com/MrEthical07/superapi/internal/core/app"
)

// Module provides system utility routes.
type Module struct{}

// New constructs the system module.
func New() *Module { return &Module{} }

var _ app.Module = (*Module)(nil)

// Name returns module registry name.
func (m *Module) Name() string { return "system" }
