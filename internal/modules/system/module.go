package system

import "github.com/MrEthical07/superapi/internal/core/app"

type Module struct{}

func New() *Module { return &Module{} }

var _ app.Module = (*Module)(nil)

func (m *Module) Name() string { return "system" }
