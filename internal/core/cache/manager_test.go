package cache

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

func newTestManager(tb testing.TB) (*Manager, *miniredis.Miniredis) {
	tb.Helper()
	mr := miniredis.RunT(tb)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	tb.Cleanup(func() { _ = client.Close() })
	mgr, err := NewManager(client, ManagerConfig{Env: "test", FailOpen: true, DefaultMaxBytes: 256 * 1024, TagVersionCacheTTL: 250 * time.Millisecond})
	if err != nil {
		tb.Fatalf("NewManager() error = %v", err)
	}
	return mgr, mr
}

func TestBuildReadKeyDeterministicWithOrderedQuery(t *testing.T) {
	mgr, _ := newTestManager(t)

	r1 := httptest.NewRequest("GET", "/api/v1/tenants/123?limit=10&sort=desc", nil)
	r2 := httptest.NewRequest("GET", "/api/v1/tenants/123?sort=desc&limit=10", nil)

	rctx1 := chi.NewRouteContext()
	rctx1.URLParams.Add("id", "123")
	r1 = r1.WithContext(context.WithValue(r1.Context(), chi.RouteCtxKey, rctx1))

	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("id", "123")
	r2 = r2.WithContext(context.WithValue(r2.Context(), chi.RouteCtxKey, rctx2))

	cfg := CacheReadConfig{
		TTL: 30 * time.Second,
		VaryBy: CacheVaryBy{
			PathParams:  []string{"id"},
			QueryParams: []string{"limit", "sort"},
		},
	}

	k1, err := mgr.BuildReadKey(context.Background(), r1, "/api/v1/tenants/{id}", cfg)
	if err != nil {
		t.Fatalf("BuildReadKey(r1) error = %v", err)
	}
	k2, err := mgr.BuildReadKey(context.Background(), r2, "/api/v1/tenants/{id}", cfg)
	if err != nil {
		t.Fatalf("BuildReadKey(r2) error = %v", err)
	}

	if k1 != k2 {
		t.Fatalf("keys differ for equivalent selected query params: %q vs %q", k1, k2)
	}
}

func TestBuildReadKeyIgnoresUnselectedQueryParams(t *testing.T) {
	mgr, _ := newTestManager(t)

	r1 := httptest.NewRequest("GET", "/api/v1/tenants/123?limit=10&debug=1", nil)
	r2 := httptest.NewRequest("GET", "/api/v1/tenants/123?limit=10&debug=2", nil)

	rctx1 := chi.NewRouteContext()
	rctx1.URLParams.Add("id", "123")
	r1 = r1.WithContext(context.WithValue(r1.Context(), chi.RouteCtxKey, rctx1))

	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("id", "123")
	r2 = r2.WithContext(context.WithValue(r2.Context(), chi.RouteCtxKey, rctx2))

	cfg := CacheReadConfig{
		TTL: 30 * time.Second,
		VaryBy: CacheVaryBy{
			PathParams:  []string{"id"},
			QueryParams: []string{"limit"},
		},
	}

	k1, err := mgr.BuildReadKey(context.Background(), r1, "/api/v1/tenants/{id}", cfg)
	if err != nil {
		t.Fatalf("BuildReadKey(r1) error = %v", err)
	}
	k2, err := mgr.BuildReadKey(context.Background(), r2, "/api/v1/tenants/{id}", cfg)
	if err != nil {
		t.Fatalf("BuildReadKey(r2) error = %v", err)
	}

	if k1 != k2 {
		t.Fatalf("keys differ for unselected query params: %q vs %q", k1, k2)
	}
}

func TestBuildReadKeyAuthVaryByUserAndTenant(t *testing.T) {
	mgr, _ := newTestManager(t)

	r1 := httptest.NewRequest("GET", "/api/v1/tenants/123", nil)
	r1 = r1.WithContext(auth.WithContext(r1.Context(), auth.AuthContext{UserID: "u1", TenantID: "t1"}))
	r2 := httptest.NewRequest("GET", "/api/v1/tenants/123", nil)
	r2 = r2.WithContext(auth.WithContext(r2.Context(), auth.AuthContext{UserID: "u2", TenantID: "t2"}))

	rctx1 := chi.NewRouteContext()
	rctx1.URLParams.Add("id", "123")
	r1 = r1.WithContext(context.WithValue(r1.Context(), chi.RouteCtxKey, rctx1))

	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("id", "123")
	r2 = r2.WithContext(context.WithValue(r2.Context(), chi.RouteCtxKey, rctx2))

	cfg := CacheReadConfig{
		TTL: 30 * time.Second,
		VaryBy: CacheVaryBy{
			PathParams: []string{"id"},
			UserID:     true,
			TenantID:   true,
		},
		AllowAuthenticated: true,
	}

	k1, err := mgr.BuildReadKey(context.Background(), r1, "/api/v1/tenants/{id}", cfg)
	if err != nil {
		t.Fatalf("BuildReadKey(r1) error = %v", err)
	}
	k2, err := mgr.BuildReadKey(context.Background(), r2, "/api/v1/tenants/{id}", cfg)
	if err != nil {
		t.Fatalf("BuildReadKey(r2) error = %v", err)
	}

	if k1 == k2 {
		t.Fatalf("expected different keys for different user/tenant dimensions")
	}
}

func TestTagVersionTokenChangesAfterBump(t *testing.T) {
	mgr, _ := newTestManager(t)

	before, err := mgr.TagVersionToken(context.Background(), []string{"tenant"})
	if err != nil {
		t.Fatalf("TagVersionToken(before) error = %v", err)
	}

	if err := mgr.BumpTags(context.Background(), []string{"tenant"}); err != nil {
		t.Fatalf("BumpTags() error = %v", err)
	}

	after, err := mgr.TagVersionToken(context.Background(), []string{"tenant"})
	if err != nil {
		t.Fatalf("TagVersionToken(after) error = %v", err)
	}

	if before == after {
		t.Fatalf("expected tag version token to change after bump")
	}
}

func TestBuildReadKeyDiffersByDynamicTagPathParam(t *testing.T) {
	mgr, _ := newTestManager(t)

	r1 := httptest.NewRequest("GET", "/api/v1/projects/p1", nil)
	rctx1 := chi.NewRouteContext()
	rctx1.URLParams.Add("id", "p1")
	r1 = r1.WithContext(context.WithValue(r1.Context(), chi.RouteCtxKey, rctx1))

	r2 := httptest.NewRequest("GET", "/api/v1/projects/p2", nil)
	rctx2 := chi.NewRouteContext()
	rctx2.URLParams.Add("id", "p2")
	r2 = r2.WithContext(context.WithValue(r2.Context(), chi.RouteCtxKey, rctx2))

	cfg := CacheReadConfig{
		TTL:      30 * time.Second,
		TagSpecs: []CacheTagSpec{{Name: "project", PathParams: []string{"id"}}},
		VaryBy:   CacheVaryBy{PathParams: []string{"id"}},
	}

	k1, err := mgr.BuildReadKey(context.Background(), r1, "/api/v1/projects/{id}", cfg)
	if err != nil {
		t.Fatalf("BuildReadKey(r1) error = %v", err)
	}
	k2, err := mgr.BuildReadKey(context.Background(), r2, "/api/v1/projects/{id}", cfg)
	if err != nil {
		t.Fatalf("BuildReadKey(r2) error = %v", err)
	}

	if k1 == k2 {
		t.Fatalf("expected different keys for different dynamic tag path params")
	}
}

func TestBuildReadKeyFailsWhenDynamicTagPathParamMissing(t *testing.T) {
	mgr, _ := newTestManager(t)

	r := httptest.NewRequest("GET", "/api/v1/projects", nil)
	cfg := CacheReadConfig{
		TTL:      30 * time.Second,
		TagSpecs: []CacheTagSpec{{Name: "project", PathParams: []string{"id"}}},
	}

	_, err := mgr.BuildReadKey(context.Background(), r, "/api/v1/projects", cfg)
	if err == nil {
		t.Fatalf("expected error when dynamic tag path param is missing")
	}
}

func TestBuildReadKeyFailsWhenDynamicTagTenantMissing(t *testing.T) {
	mgr, _ := newTestManager(t)

	r := httptest.NewRequest("GET", "/api/v1/projects", nil)
	cfg := CacheReadConfig{
		TTL:      30 * time.Second,
		TagSpecs: []CacheTagSpec{{Name: "project-list", TenantID: true}},
	}

	_, err := mgr.BuildReadKey(context.Background(), r, "/api/v1/projects", cfg)
	if err == nil {
		t.Fatalf("expected error when dynamic tag tenant id is missing")
	}
}
