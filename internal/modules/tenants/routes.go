package tenants

import (
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/httpx"
)

func (m *Module) Register(r httpx.Router) error {
	if m.handler == nil {
		m.handler = NewHandler(nil)
	}

	r.Handle(http.MethodPost, "/api/v1/tenants", m.handler.Create())
	r.Handle(http.MethodGet, "/api/v1/tenants", http.HandlerFunc(m.handler.List))
	r.Handle(http.MethodGet, "/api/v1/tenants/{id}", http.HandlerFunc(m.handler.GetByID))

	return nil
}
