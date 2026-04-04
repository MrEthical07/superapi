package policy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
)

func newCacheManagerForPolicyTests(tb testing.TB, redisAddr string, failOpen bool) *cache.Manager {
	tb.Helper()
	client := redis.NewClient(&redis.Options{Addr: redisAddr, DialTimeout: 10 * time.Millisecond, ReadTimeout: 10 * time.Millisecond, WriteTimeout: 10 * time.Millisecond})
	tb.Cleanup(func() { _ = client.Close() })
	mgr, err := cache.NewManager(client, cache.ManagerConfig{Env: "test", FailOpen: failOpen, DefaultMaxBytes: 128})
	if err != nil {
		tb.Fatalf("NewManager() error = %v", err)
	}
	return mgr
}

func TestCacheReadMissThenHitReturnsSameBodyAndContentType(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)

	calls := 0
	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{id}", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"data":{"id":"` + chi.URLParam(r, "id") + `"}}`))
		}),
		CacheRead(mgr, cache.CacheReadConfig{
			TTL: 30 * time.Second,
			VaryBy: cache.CacheVaryBy{
				PathParams: []string{"id"},
			},
		}),
	).ServeHTTP)

	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
	rr1 := httptest.NewRecorder()
	r.ServeHTTP(rr1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)

	if calls != 1 {
		t.Fatalf("handler calls=%d want=1", calls)
	}
	if rr2.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", rr2.Code, http.StatusOK)
	}
	if rr1.Body.String() != rr2.Body.String() {
		t.Fatalf("body mismatch first=%q second=%q", rr1.Body.String(), rr2.Body.String())
	}
	if got := rr2.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type=%q expected application/json", got)
	}
}

func TestCacheReadBypassesLargeResponses(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)

	calls := 0
	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{id}", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(strings.Repeat("x", 64)))
		}),
		CacheRead(mgr, cache.CacheReadConfig{
			TTL:      30 * time.Second,
			MaxBytes: 16,
			VaryBy: cache.CacheVaryBy{
				PathParams: []string{"id"},
			},
		}),
	).ServeHTTP)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil))

	if calls != 2 {
		t.Fatalf("handler calls=%d want=2 (large response should bypass cache)", calls)
	}
}

func TestCacheReadBypassesSetCookieResponses(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)

	calls := 0
	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{id}", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Set-Cookie", "session=abc")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
		CacheRead(mgr, cache.CacheReadConfig{TTL: 30 * time.Second, VaryBy: cache.CacheVaryBy{PathParams: []string{"id"}}}),
	).ServeHTTP)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil))

	if calls != 2 {
		t.Fatalf("handler calls=%d want=2 (set-cookie should bypass cache)", calls)
	}
}

func TestCacheReadAuthSafetyPanicsWithoutUserOrTenantVary(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{id}", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
		CacheRead(mgr, cache.CacheReadConfig{TTL: 30 * time.Second, VaryBy: cache.CacheVaryBy{PathParams: []string{"id"}}}),
	).ServeHTTP)

	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
	req1 = req1.WithContext(auth.WithContext(req1.Context(), auth.AuthContext{UserID: "u1", TenantID: "t1"}))

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("expected panic for unsafe authenticated cache configuration")
		}
		if !strings.Contains(toString(recovered), "invalid route config") {
			t.Fatalf("unexpected panic message: %v", recovered)
		}
	}()

	r.ServeHTTP(httptest.NewRecorder(), req1)
}

func TestCacheReadAuthVaryByUserProducesDifferentEntries(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{id}", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, _ := auth.FromContext(r.Context())
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"user":"` + principal.UserID + `"}`))
		}),
		CacheRead(mgr, cache.CacheReadConfig{
			TTL: 30 * time.Second,
			VaryBy: cache.CacheVaryBy{
				PathParams: []string{"id"},
				UserID:     true,
			},
		}),
	).ServeHTTP)

	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
	req1 = req1.WithContext(auth.WithContext(req1.Context(), auth.AuthContext{UserID: "u1"}))
	rr1 := httptest.NewRecorder()
	r.ServeHTTP(rr1, req1)

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
	req2 = req2.WithContext(auth.WithContext(req2.Context(), auth.AuthContext{UserID: "u2"}))
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)

	if rr1.Body.String() == rr2.Body.String() {
		t.Fatalf("expected different cached outputs by user")
	}
}

func TestCacheInvalidateBumpsTagVersionForcesMiss(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)

	getCalls := 0

	r := chi.NewRouter()
	r.Get("/api/v1/tenants/{id}", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			getCalls++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
		CacheRead(mgr, cache.CacheReadConfig{
			TTL:      time.Minute,
			TagSpecs: []cache.CacheTagSpec{{Name: "tenant"}},
			VaryBy:   cache.CacheVaryBy{PathParams: []string{"id"}},
		}),
	).ServeHTTP)

	r.Post("/api/v1/tenants", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
		CacheInvalidate(mgr, cache.CacheInvalidateConfig{TagSpecs: []cache.CacheTagSpec{{Name: "tenant"}}}),
	).ServeHTTP)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil))
	if getCalls != 1 {
		t.Fatalf("get calls=%d want=1 after warm cache", getCalls)
	}

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/tenants", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil))

	if getCalls != 2 {
		t.Fatalf("get calls=%d want=2 after invalidate", getCalls)
	}
}

func TestCacheReadFailOpenOnRedisError(t *testing.T) {
	mgr := newCacheManagerForPolicyTests(t, "127.0.0.1:1", true)

	called := false
	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
		CacheRead(mgr, cache.CacheReadConfig{TTL: 30 * time.Second}),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
	h.ServeHTTP(rr, req)

	if !called {
		t.Fatalf("expected handler to run in fail-open mode")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
	}
}

func TestCacheReadFailClosedOnRedisError(t *testing.T) {
	mgr := newCacheManagerForPolicyTests(t, "127.0.0.1:1", false)

	called := false
	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
		CacheRead(mgr, cache.CacheReadConfig{TTL: 30 * time.Second}),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1", nil)
	h.ServeHTTP(rr, req)

	if called {
		t.Fatalf("expected handler not to run in fail-closed mode")
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rr.Body.String(), `"code":"dependency_unavailable"`) {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestCacheInvalidateNoopWithoutSuccessStatus(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}),
		CacheInvalidate(mgr, cache.CacheInvalidateConfig{TagSpecs: []cache.CacheTagSpec{{Name: "tenant", PathParams: []string{"id"}}}}),
	)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/tenants", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusBadRequest)
	}

	token, err := mgr.TagVersionToken(context.Background(), []string{"tenant"})
	if err != nil {
		t.Fatalf("TagVersionToken() error = %v", err)
	}
	if token != "tenant=0" {
		t.Fatalf("token=%q want=%q", token, "tenant=0")
	}
}

func TestCacheInvalidateScopedByPathParamDoesNotEvictOtherDetails(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)

	getCalls := map[string]int{}

	r := chi.NewRouter()
	r.Get("/api/v1/projects/{id}", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			getCalls[id]++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"id":"` + id + `"}`))
		}),
		CacheRead(mgr, cache.CacheReadConfig{
			TTL:      time.Minute,
			TagSpecs: []cache.CacheTagSpec{{Name: "project", PathParams: []string{"id"}}},
			VaryBy:   cache.CacheVaryBy{PathParams: []string{"id"}},
		}),
	).ServeHTTP)

	r.Put("/api/v1/projects/{id}", Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		CacheInvalidate(mgr, cache.CacheInvalidateConfig{TagSpecs: []cache.CacheTagSpec{{Name: "project", PathParams: []string{"id"}}}}),
	).ServeHTTP)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/projects/p2", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/projects/p2", nil))

	if getCalls["p1"] != 1 || getCalls["p2"] != 1 {
		t.Fatalf("expected warm-cache hits before update, calls=%v", getCalls)
	}

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPut, "/api/v1/projects/p1", nil))

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/projects/p2", nil))

	if getCalls["p1"] != 2 {
		t.Fatalf("expected p1 cache to be invalidated, calls=%v", getCalls)
	}
	if getCalls["p2"] != 1 {
		t.Fatalf("expected p2 cache entry to stay warm, calls=%v", getCalls)
	}
}
