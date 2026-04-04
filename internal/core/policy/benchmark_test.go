package policy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

type latencyLimiter struct {
	inner ratelimit.Limiter
	delay time.Duration
}

func (l latencyLimiter) Allow(ctx context.Context, req ratelimit.Request) (ratelimit.Decision, error) {
	if l.delay > 0 {
		time.Sleep(l.delay)
	}
	return l.inner.Allow(ctx, req)
}

func BenchmarkAuthRequired_Allow(b *testing.B) {
	engine, token := newPolicyTestAuthEngine(b)
	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(engine, auth.ModeHybrid),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/whoami", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
		}
	}
}

func BenchmarkRateLimit_Allow(b *testing.B) {
	limiter := &mockLimiter{decision: ratelimit.Decision{Allowed: true, Remaining: 99999, Outcome: ratelimit.OutcomeAllowed}}
	r := chi.NewRouter()
	r.Get("/api/v1/system/whoami", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		RateLimit(limiter, ratelimit.Rule{Limit: 100000, Window: time.Minute, Scope: ratelimit.ScopeAnon}),
	).ServeHTTP)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/whoami", nil)
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
		}
	}
}

func BenchmarkRateLimit_AllowWithInjectedDependencyLatency(b *testing.B) {
	limiter := latencyLimiter{
		inner: &mockLimiter{decision: ratelimit.Decision{Allowed: true, Remaining: 99999, Outcome: ratelimit.OutcomeAllowed}},
		delay: 250 * time.Microsecond,
	}
	r := chi.NewRouter()
	r.Get("/api/v1/system/whoami", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		RateLimit(limiter, ratelimit.Rule{Limit: 100000, Window: time.Minute, Scope: ratelimit.ScopeAnon}),
	).ServeHTTP)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/whoami", nil)
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
		}
	}
}

func BenchmarkRateLimit_RedisLimiterAllow(b *testing.B) {
	mr := miniredis.RunT(b)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	b.Cleanup(func() { _ = client.Close() })

	limiter, err := ratelimit.NewRedisLimiter(client, ratelimit.Config{Env: "bench", FailOpen: false})
	if err != nil {
		b.Fatalf("new redis limiter: %v", err)
	}

	r := chi.NewRouter()
	r.Get("/api/v1/system/whoami", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		RateLimit(limiter, ratelimit.Rule{Limit: 1000000, Window: time.Minute, Scope: ratelimit.ScopeAnon}),
	).ServeHTTP)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/whoami", nil)
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
		}
	}
}

func BenchmarkCacheRead_Hit(b *testing.B) {
	mr := miniredis.RunT(b)
	mgr := newCacheManagerForPolicyTests(b, mr.Addr(), true)

	r := chi.NewRouter()
	r.Get("/api/v1/projects/{id}", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"data":{"id":"project_1"}}`))
		}),
		CacheRead(mgr, cache.CacheReadConfig{
			TTL: 30 * time.Second,
			VaryBy: cache.CacheVaryBy{
				PathParams: []string{"id"},
			},
		}),
	).ServeHTTP)

	warmReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project_1", nil)
	r.ServeHTTP(httptest.NewRecorder(), warmReq)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/project_1", nil)
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
		}
	}
}
