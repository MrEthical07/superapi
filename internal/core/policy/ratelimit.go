package policy

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
	"github.com/MrEthical07/superapi/internal/core/response"
)

func RateLimit(limiter ratelimit.Limiter, rule ratelimit.Rule) Policy {
	return RateLimitWithKeyer(limiter, "", rule, nil)
}

func RateLimitWithKeyer(limiter ratelimit.Limiter, name string, rule ratelimit.Rule, keyer ratelimit.Keyer) Policy {
	if limiter == nil {
		return Noop()
	}

	if keyer != nil {
		rule.Keyer = keyer
	}

	if err := rule.Validate(); err != nil {
		return Noop()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := routePattern(r)
			scope, identifier := ratelimit.ResolveScopeAndIdentifier(r, rule)

			decision, err := limiter.Allow(r.Context(), ratelimit.Request{
				Route:      route,
				Scope:      scope,
				Identifier: identifier,
				Limit:      rule.Limit,
				Window:     rule.Window,
			})
			if err != nil {
				response.Error(w, apperr.New(apperr.CodeInternal, http.StatusInternalServerError, "internal server error"), "")
				return
			}

			if !decision.Allowed {
				if seconds := ratelimit.RetryAfterSeconds(decision.RetryAfter); seconds > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(seconds))
				}
				response.Error(w, apperr.New(apperr.CodeTooManyRequests, http.StatusTooManyRequests, "rate limit exceeded"), "")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return "unknown"
	}
	if pattern := rctx.RoutePattern(); pattern != "" {
		return pattern
	}
	return "unknown"
}
