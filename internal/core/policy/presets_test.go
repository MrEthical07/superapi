package policy

import (
	"context"
	"net/http"
	"testing"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/alicebob/miniredis/v2"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

type presetAllowLimiter struct{}

func (presetAllowLimiter) Allow(context.Context, ratelimit.Request) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: true, Outcome: ratelimit.OutcomeAllowed}, nil
}

func newPresetDeps(t *testing.T) (*goauth.Engine, ratelimit.Limiter, *cache.Manager) {
	t.Helper()
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)
	return &goauth.Engine{}, presetAllowLimiter{}, mgr
}

func TestTenantReadPresetPassesValidator(t *testing.T) {
	engine, limiter, mgr := newPresetDeps(t)

	policies := TenantRead(
		WithAuthEngine(engine, auth.ModeStrict),
		WithLimiter(limiter),
		WithCacheManager(mgr),
		WithCache(45*time.Second, cache.CacheTagSpec{Name: "project"}),
	)

	metas, err := DescribePolicies(policies...)
	if err != nil {
		t.Fatalf("DescribePolicies() error = %v", err)
	}
	if err := ValidateRouteMetadata(http.MethodGet, "/api/v1/projects/{id}", metas); err != nil {
		t.Fatalf("ValidateRouteMetadata() error = %v", err)
	}
}

func TestTenantWritePresetPassesValidator(t *testing.T) {
	engine, limiter, mgr := newPresetDeps(t)

	policies := TenantWrite(
		WithAuthEngine(engine, auth.ModeStrict),
		WithLimiter(limiter),
		WithCacheManager(mgr),
		WithInvalidateTags(cache.CacheTagSpec{Name: "project"}),
	)

	metas, err := DescribePolicies(policies...)
	if err != nil {
		t.Fatalf("DescribePolicies() error = %v", err)
	}
	if err := ValidateRouteMetadata(http.MethodPost, "/api/v1/projects", metas); err != nil {
		t.Fatalf("ValidateRouteMetadata() error = %v", err)
	}
}

func TestPublicReadPresetPassesValidator(t *testing.T) {
	_, limiter, mgr := newPresetDeps(t)

	policies := PublicRead(
		WithLimiter(limiter),
		WithCacheManager(mgr),
		WithCache(20*time.Second, cache.CacheTagSpec{Name: "public"}),
	)

	metas, err := DescribePolicies(policies...)
	if err != nil {
		t.Fatalf("DescribePolicies() error = %v", err)
	}
	if err := ValidateRouteMetadata(http.MethodGet, "/api/v1/public/status", metas); err != nil {
		t.Fatalf("ValidateRouteMetadata() error = %v", err)
	}
}

func TestTenantReadPresetPanicsWithoutAuth(t *testing.T) {
	_, limiter, mgr := newPresetDeps(t)

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatalf("expected panic")
		}
	}()

	_ = TenantRead(
		WithLimiter(limiter),
		WithCacheManager(mgr),
	)
}

func TestPublicReadPresetOrderIsStable(t *testing.T) {
	_, limiter, mgr := newPresetDeps(t)

	policies := PublicRead(
		WithLimiter(limiter),
		WithCacheManager(mgr),
	)
	metas, err := DescribePolicies(policies...)
	if err != nil {
		t.Fatalf("DescribePolicies() error = %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("len(metas)=%d want=2", len(metas))
	}
	if metas[0].Type != PolicyTypeRateLimit {
		t.Fatalf("first policy=%s want=%s", metas[0].Type, PolicyTypeRateLimit)
	}
	if metas[1].Type != PolicyTypeCacheRead {
		t.Fatalf("second policy=%s want=%s", metas[1].Type, PolicyTypeCacheRead)
	}
}
