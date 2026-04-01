package policy

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

type allowLimiter struct{}

func (allowLimiter) Allow(context.Context, ratelimit.Request) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: true, Outcome: ratelimit.OutcomeAllowed}, nil
}

func TestMustValidateRoutePanicsOnPolicyOrderViolation(t *testing.T) {
	assertRouteConfigPanic(t, "cannot appear after", func() {
		MustValidateRoute(
			http.MethodGet,
			"/api/v1/system/whoami",
			RateLimit(allowLimiter{}, ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ratelimit.ScopeAnon}),
			AuthRequired(nil, auth.ModeHybrid),
		)
	})
}

func TestMustValidateRoutePanicsOnMissingAuthDependency(t *testing.T) {
	assertRouteConfigPanic(t, string(PolicyTypeAuthRequired), func() {
		MustValidateRoute(
			http.MethodGet,
			"/api/v1/system/whoami",
			RequirePerm("system.whoami"),
		)
	})
}

func TestMustValidateRoutePanicsOnUnsafeAuthenticatedCache(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)

	assertRouteConfigPanic(t, "VaryBy.UserID or VaryBy.TenantID", func() {
		MustValidateRoute(
			http.MethodGet,
			"/api/v1/system/whoami",
			AuthRequired(nil, auth.ModeHybrid),
			CacheRead(mgr, cache.CacheReadConfig{TTL: time.Minute}),
		)
	})
}

func TestMustValidateRoutePanicsOnTenantPathWithoutMatchPolicy(t *testing.T) {
	assertRouteConfigPanic(t, string(PolicyTypeTenantMatchFromPath), func() {
		MustValidateRoute(
			http.MethodGet,
			"/api/v1/tenants/{tenant_id}/projects",
			AuthRequired(nil, auth.ModeHybrid),
			TenantRequired(),
		)
	})
}

func TestMustValidateRoutePassesOnStrictValidConfiguration(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := newCacheManagerForPolicyTests(t, mr.Addr(), true)

	assertRouteConfigDoesNotPanic(t, func() {
		MustValidateRoute(
			http.MethodGet,
			"/api/v1/tenants/{tenant_id}/projects/{id}",
			AuthRequired(nil, auth.ModeHybrid),
			TenantRequired(),
			TenantMatchFromPath("tenant_id"),
			RequirePerm("project.read"),
			RateLimit(allowLimiter{}, ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ratelimit.ScopeTenant}),
			CacheRead(mgr, cache.CacheReadConfig{
				TTL: time.Minute,
				VaryBy: cache.CacheVaryBy{
					TenantID: true,
				},
			}),
		)
	})
}

func assertRouteConfigPanic(t *testing.T, expectedContains string, fn func()) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("expected panic")
		}
		message := strings.TrimSpace(toString(recovered))
		if !strings.Contains(message, "invalid route config") {
			t.Fatalf("unexpected panic message: %q", message)
		}
		if expectedContains != "" && !strings.Contains(message, expectedContains) {
			t.Fatalf("panic message %q does not contain %q", message, expectedContains)
		}
	}()

	fn()
}

func assertRouteConfigDoesNotPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()

	fn()
}

func toString(v any) string {
	s, ok := v.(string)
	if ok {
		return s
	}
	return "panic"
}
