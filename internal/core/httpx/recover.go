package httpx

import (
	"fmt"
	"net/http"

	"github.com/MrEthical07/superapi/internal/core/logx"
	"github.com/MrEthical07/superapi/internal/core/response"
)

// Recoverer returns middleware that catches panics, logs them with structured
// context, and returns a sanitized 500 response. The logger is injected
// explicitly — no global state.
func Recoverer(log *logx.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					rid := RequestIDFromContext(r.Context())
					log.Error().
						Str("request_id", rid).
						Str("method", r.Method).
						Str("path", r.URL.Path).
						Str("remote_addr", r.RemoteAddr).
						Str("panic", fmt.Sprintf("%v", rec)).
						Msg("panic recovered")
					response.Error(w, nil, rid)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
