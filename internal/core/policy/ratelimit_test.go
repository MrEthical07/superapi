package policy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

type mockLimiter struct {
	decision ratelimit.Decision
	err      error
	request  ratelimit.Request
}

func (m *mockLimiter) Allow(ctx context.Context, req ratelimit.Request) (ratelimit.Decision, error) {
	m.request = req
	return m.decision, m.err
}

func TestRateLimitBlocksWithRetryAfter(t *testing.T) {
	limiter := &mockLimiter{decision: ratelimit.Decision{Allowed: false, RetryAfter: 3 * time.Second}}

	r := chi.NewRouter()
	r.Get("/api/v1/system/whoami", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		RateLimit(limiter, ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ratelimit.ScopeAnon}),
	).ServeHTTP)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/whoami", nil)
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusTooManyRequests)
	}
	if got := rr.Header().Get("Retry-After"); got != "3" {
		t.Fatalf("Retry-After=%q want=%q", got, "3")
	}
	if !strings.Contains(rr.Body.String(), `"code":"too_many_requests"`) {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
	if limiter.request.Route != "/api/v1/system/whoami" {
		t.Fatalf("route=%q want=%q", limiter.request.Route, "/api/v1/system/whoami")
	}
}

func TestRateLimitAllowsWhenUnderLimit(t *testing.T) {
	limiter := &mockLimiter{decision: ratelimit.Decision{Allowed: true, Remaining: 9}}

	r := chi.NewRouter()
	r.Get("/api/v1/system/whoami", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		RateLimit(limiter, ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ratelimit.ScopeAnon}),
	).ServeHTTP)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/whoami", nil)
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
	}
}

func TestRateLimitReturnsDependencyUnavailableOnLimiterErrorOutcome(t *testing.T) {
	limiter := &mockLimiter{decision: ratelimit.Decision{Allowed: false, Outcome: ratelimit.OutcomeError}}

	r := chi.NewRouter()
	r.Get("/api/v1/system/whoami", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		RateLimit(limiter, ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ratelimit.ScopeAnon}),
	).ServeHTTP)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/whoami", nil)
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rr.Body.String(), `"code":"dependency_unavailable"`) {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}
