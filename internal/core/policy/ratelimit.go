package policy

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
	"github.com/MrEthical07/superapi/internal/core/requestid"
	"github.com/MrEthical07/superapi/internal/core/response"
)

// RateLimit applies route-level throttling using default rule key resolution.
//
// Use RateLimitWithKeyer when you need custom identity key extraction.
func RateLimit(limiter ratelimit.Limiter, rule ratelimit.Rule) Policy {
	return RateLimitWithKeyer(limiter, "", rule, nil)
}

// RateLimitWithKeyer applies route-level throttling with optional custom keyer.
//
// Behavior:
// - Returns 429 when budget is exceeded
// - Returns 503 when limiter fails in fail-closed mode
// - Emits Retry-After header when retry delay is known
func RateLimitWithKeyer(limiter ratelimit.Limiter, name string, rule ratelimit.Rule, keyer ratelimit.Keyer) Policy {
	if limiter == nil {
		panicInvalidRouteConfigf("%s requires a non-nil limiter", PolicyTypeRateLimit)
	}

	if keyer != nil {
		rule.Keyer = keyer
	}

	if err := rule.Validate(); err != nil {
		panicInvalidRouteConfigf("%s rule is invalid: %v", PolicyTypeRateLimit, err)
	}

	p := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := requestid.FromContext(r.Context())
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
				response.Error(w, apperr.New(apperr.CodeInternal, http.StatusInternalServerError, "internal server error"), rid)
				return
			}

			if !decision.Allowed {
				if decision.Outcome == ratelimit.OutcomeError {
					response.Error(w, apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "rate limiter unavailable"), rid)
					return
				}

				if seconds := ratelimit.RetryAfterSeconds(decision.RetryAfter); seconds > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(seconds))
				}
				response.Error(w, apperr.New(apperr.CodeTooManyRequests, http.StatusTooManyRequests, "rate limit exceeded"), rid)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	return annotatePolicy(p, Metadata{Type: PolicyTypeRateLimit, Name: "RateLimit"})
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
