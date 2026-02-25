package health

import (
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/readiness"
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
	report := readiness.Report{
		Status:       readiness.StatusReady,
		Dependencies: map[string]readiness.DependencyStatus{},
	}
	if m.readiness != nil {
		report = m.readiness.Check(r.Context())
	}

	status := http.StatusOK
	ok := true
	if report.Status != readiness.StatusReady {
		status = http.StatusServiceUnavailable
		ok = false
	}

	response.JSON(w, status, response.Envelope{
		OK:        ok,
		Data:      report,
		RequestID: httpx.RequestIDFromContext(r.Context()),
	})
}
