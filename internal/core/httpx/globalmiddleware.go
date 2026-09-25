package httpx

import (
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
	"github.com/MrEthical07/superapi/internal/core/tracing"
)

// AssembleGlobalMiddleware wraps a base handler with configured global middleware.
//
// Execution order (outermost -> innermost):
//  1. RequestID (if enabled)
//  2. ClientIP (trusted proxies optional)
//  3. Recoverer (if enabled)
//  4. CORS (if enabled)
//  5. SecurityHeaders (if enabled)
//  6. MaxBodyBytes (if enabled)
//  7. RequestTimeout (if enabled)
//  8. Tracing (if enabled)
//  9. AccessLog (if enabled)
//  10. Tenant resolution (only when supplied via WithTenantResolver, i.e.
//     TENANCY_ENABLED=true)
//
// This order keeps request_id available in recover logs, and keeps recoverer
// around downstream middleware/handlers. Tenant resolution runs last, after
// RequestID/ClientIP and just before routing, so its rejections are still
// logged, traced, time-bounded, and carry security headers, and CORS preflight
// requests are answered before a tenant is demanded.
//
// For cache and rate-limit route policy behavior, see docs/cache-guide.md and
// docs/policies.md.
func AssembleGlobalMiddleware(base http.Handler, cfg config.HTTPMiddlewareConfig, log *logx.Logger, tracingSvc *tracing.Service, opts ...GlobalOption) http.Handler {
	var options globalOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	handler := base
	if options.tenantResolver != nil {
		handler = options.tenantResolver(handler)
	}

	handler = AccessLog(cfg.AccessLog, log)(handler)
	handler = TracingWithExcludes(tracingSvc, cfg.TracingExcludePaths)(handler)
	handler = RequestTimeout(cfg.RequestTimeout)(handler)

	if cfg.MaxBodyBytes > 0 {
		handler = MaxBodyBytes(cfg.MaxBodyBytes)(handler)
	}
	if cfg.SecurityHeadersEnabled {
		handler = SecurityHeaders(handler)
	}
	if cfg.CORS.Enabled {
		handler = CORS(cfg.CORS)(handler)
	}
	if cfg.RecovererEnabled {
		handler = Recoverer(log)(handler)
	}
	handler = ClientIP(cfg.ClientIP)(handler)
	if cfg.RequestIDEnabled {
		handler = RequestID(handler)
	}

	return handler
}

// GlobalOption customizes AssembleGlobalMiddleware.
type GlobalOption func(*globalOptions)

type globalOptions struct {
	tenantResolver func(http.Handler) http.Handler
}

// WithTenantResolver installs the tenant resolution middleware as the innermost
// global middleware (see internal/core/tenant.Middleware). Pass nil to skip.
func WithTenantResolver(mw func(http.Handler) http.Handler) GlobalOption {
	return func(o *globalOptions) {
		o.tenantResolver = mw
	}
}
