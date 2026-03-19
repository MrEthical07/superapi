package tenants

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	coredb "github.com/MrEthical07/superapi/internal/core/db"
	"github.com/MrEthical07/superapi/internal/core/modulekit"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Module struct {
	runtime modulekit.Runtime
	pool    *pgxpool.Pool
	handler *Handler
}

func New() *Module { return &Module{} }

var _ app.Module = (*Module)(nil)
var _ app.DependencyBinder = (*Module)(nil)

func (m *Module) Name() string { return "tenants" }

func (m *Module) BindDependencies(deps *app.Dependencies) {
	m.runtime = modulekit.New(deps)

	if deps == nil || deps.Postgres == nil {
		m.pool = nil
		m.handler = NewHandler(nil)
		return
	}

	m.pool = deps.Postgres
	repo := NewRepository(coredb.NewQueries(m.pool))
	svc := NewService(m.pool, repo)
	m.handler = NewHandler(svc)
}
