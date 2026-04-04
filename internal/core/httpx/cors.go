package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MrEthical07/superapi/internal/core/config"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/requestid"
	"github.com/MrEthical07/superapi/internal/core/response"
)

// CORS applies CORS headers and handles preflight requests.
func CORS(cfg config.CORSConfig) func(http.Handler) http.Handler {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler { return next }
	}

	allowOrigins := normalizeOrigins(cfg.AllowOrigins)
	denyOrigins := normalizeOrigins(cfg.DenyOrigins)
	_, allowAll := allowOrigins["*"]

	allowMethods := normalizeTokens(cfg.AllowMethods, []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodHead,
		http.MethodOptions,
	})
	allowHeaders := normalizeTokens(cfg.AllowHeaders, nil)
	exposeHeaders := normalizeTokens(cfg.ExposeHeaders, nil)

	maxAge := maxAgeSeconds(cfg.MaxAge)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			if isDeniedOrigin(origin, denyOrigins) || !isAllowedOrigin(origin, allowOrigins, allowAll, cfg.AllowCredentials) {
				if isPreflight(r) {
					rid := requestid.FromContext(r.Context())
					response.Error(w, apperr.New(apperr.CodeForbidden, http.StatusForbidden, "origin not allowed"), rid)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			appendVary(w.Header(), "Origin")
			if allowAll && !cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			if cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if len(exposeHeaders) > 0 {
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(exposeHeaders, ", "))
			}

			if isPreflight(r) {
				appendVary(w.Header(), "Access-Control-Request-Method")
				appendVary(w.Header(), "Access-Control-Request-Headers")

				if len(allowMethods) > 0 {
					w.Header().Set("Access-Control-Allow-Methods", strings.Join(allowMethods, ", "))
				}

				if len(allowHeaders) > 0 {
					w.Header().Set("Access-Control-Allow-Headers", strings.Join(allowHeaders, ", "))
				} else if reqHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers")); reqHeaders != "" {
					w.Header().Set("Access-Control-Allow-Headers", reqHeaders)
				}

				if maxAge != "" {
					w.Header().Set("Access-Control-Max-Age", maxAge)
				}

				if cfg.AllowPrivateNetwork && strings.EqualFold(strings.TrimSpace(r.Header.Get("Access-Control-Request-Private-Network")), "true") {
					w.Header().Set("Access-Control-Allow-Private-Network", "true")
				}

				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
}

func normalizeOrigins(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		out[strings.ToLower(trimmed)] = struct{}{}
	}
	return out
}

func isDeniedOrigin(origin string, deny map[string]struct{}) bool {
	if len(deny) == 0 {
		return false
	}
	_, deniedAll := deny["*"]
	if deniedAll {
		return true
	}
	_, denied := deny[strings.ToLower(origin)]
	return denied
}

func isAllowedOrigin(origin string, allow map[string]struct{}, allowAll bool, allowCredentials bool) bool {
	if allowAll {
		return !allowCredentials
	}
	if len(allow) == 0 {
		return false
	}
	_, ok := allow[strings.ToLower(origin)]
	return ok
}

func normalizeTokens(values []string, fallback []string) []string {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func appendVary(h http.Header, value string) {
	existing := h.Values("Vary")
	for _, v := range existing {
		parts := strings.Split(v, ",")
		for _, part := range parts {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	h.Add("Vary", value)
}

func maxAgeSeconds(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	seconds := int(d.Seconds())
	if seconds <= 0 {
		return ""
	}
	return strconv.Itoa(seconds)
}
