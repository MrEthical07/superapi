package health

import (
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/response"
)

func (m *Module) Register(r httpx.Router) error {
	r.Handle(http.MethodGet, "/healthz", http.HandlerFunc(m.healthz))
	r.Handle(http.MethodGet, "/readyz", http.HandlerFunc(m.readyz))
	return nil
}

func (m *Module) healthz(w http.ResponseWriter, r *http.Request) {
	response.OK(w, map[string]any{
		"status": "ok",
	}, httpx.RequestIDFromContext(r.Context()))
}

func (m *Module) readyz(w http.ResponseWriter, r *http.Request) {
	response.OK(w, map[string]any{
		"status": "ready",
	}, httpx.RequestIDFromContext(r.Context()))
}
