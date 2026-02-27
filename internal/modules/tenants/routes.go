package tenants

import (
	"net/http"
	"time"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
)

func (m *Module) Register(r httpx.Router) error {
	if m.handler == nil {
		m.handler = NewHandler(nil)
	}

	r.Handle(http.MethodGet, "/api/v1/tenants", http.HandlerFunc(m.handler.List))
	r.Handle(
		http.MethodGet,
		"/api/v1/tenants/self",
		http.HandlerFunc(m.handler.GetSelf),
		policy.AuthRequired(m.authProvider(), m.authMode()),
		policy.TenantRequired(),
	)
	r.Handle(
		http.MethodPost,
		"/api/v1/tenants",
		m.handler.Create(),
		policy.CacheInvalidate(m.cache, cache.CacheInvalidateConfig{Tags: []string{"tenant"}}),
	)
	r.Handle(
		http.MethodGet,
		"/api/v1/tenants/{id}",
		http.HandlerFunc(m.handler.GetByID),
		policy.CacheRead(m.cache, cache.CacheReadConfig{
			TTL:  30 * time.Second,
			Tags: []string{"tenant"},
			VaryBy: cache.CacheVaryBy{
				PathParams: []string{"id"},
			},
		}),
	)

	return nil
}

func (m *Module) authProvider() auth.Provider {
	if m.auth == nil {
		return auth.NewDisabledProvider()
	}
	return m.auth
}

func (m *Module) authMode() auth.Mode {
	if m.mode == "" {
		return auth.ModeHybrid
	}
	return m.mode
}
