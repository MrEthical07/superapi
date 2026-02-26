package httpx

import (
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/logx"
)

// AssembleGlobalMiddleware wraps a base handler with configured global middleware.
//
// Execution order (outermost -> innermost):
//  1. RequestID (if enabled)
//  2. Recoverer (if enabled)
//  3. SecurityHeaders (if enabled)
//  4. MaxBodyBytes (if enabled)
//  5. AccessLog (if enabled)
//
// This order keeps request_id available in recover logs, and keeps recoverer
// around downstream middleware/handlers.
func AssembleGlobalMiddleware(base http.Handler, cfg config.HTTPMiddlewareConfig, log *logx.Logger) http.Handler {
	handler := base

	handler = AccessLog(cfg.AccessLog, log)(handler)

	if cfg.MaxBodyBytes > 0 {
		handler = MaxBodyBytes(cfg.MaxBodyBytes)(handler)
	}
	if cfg.SecurityHeadersEnabled {
		handler = SecurityHeaders(handler)
	}
	if cfg.RecovererEnabled {
		handler = Recoverer(log)(handler)
	}
	if cfg.RequestIDEnabled {
		handler = RequestID(handler)
	}

	return handler
}
