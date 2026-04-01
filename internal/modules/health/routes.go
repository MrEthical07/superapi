package health

import (
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/readiness"
)

// Register mounts health and readiness routes for process monitoring.
func (m *Module) Register(r httpx.Router) error {
	r.Handle(http.MethodGet, "/healthz", httpx.Adapter(m.healthz))
	r.Handle(http.MethodGet, "/readyz", httpx.Adapter(m.readyz))
	return nil
}

func (m *Module) healthz(_ *httpx.Context, _ httpx.NoBody) (map[string]any, error) {
	return map[string]any{
		"status": "ok",
	}, nil
}

func (m *Module) readyz(ctx *httpx.Context, _ httpx.NoBody) (httpx.Result[readiness.Report], error) {
	report := readiness.Report{
		Status:       readiness.StatusReady,
		Dependencies: map[string]readiness.DependencyStatus{},
	}
	if m.readiness != nil {
		report = m.readiness.Check(ctx.Context())
	}
	if m.metrics != nil {
		m.metrics.ObserveReadiness(report)
	}

	status := http.StatusOK
	ok := true
	if report.Status != readiness.StatusReady {
		status = http.StatusServiceUnavailable
		ok = false
	}

	return httpx.Result[readiness.Report]{
		Status: status,
		OK:     httpx.BoolPtr(ok),
		Data:   report,
	}, nil
}
