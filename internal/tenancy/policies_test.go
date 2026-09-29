package tenancy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
	"github.com/MrEthical07/superapi/internal/core/policy"
	"github.com/MrEthical07/superapi/internal/core/ratelimit"
)

// withPrincipal returns req carrying an authenticated principal in tenant t
// ("" leaves the tenant attribute off).
func withPrincipal(req *http.Request, tenantID string) *http.Request {
	principal := auth.AuthContext{UserID: "u1"}
	if tenantID != "" {
		principal.Attributes = []auth.Attribute{{Key: AttrTenantID, Value: tenantID}}
	}
	return req.WithContext(auth.WithContext(req.Context(), principal))
}

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

func TestTenantRequiredUnauthorizedWhenMissingAuth(t *testing.T) {
	h := policy.Chain(okHandler, TenantRequired())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/secure", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnauthorized)
	}
}

func TestTenantRequiredForbiddenWhenTenantMissing(t *testing.T) {
	h := policy.Chain(okHandler, TenantRequired())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, withPrincipal(httptest.NewRequest(http.MethodGet, "/secure", nil), ""))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusForbidden)
	}
}

func TestTenantRequiredRejectsRequestTenantMismatch(t *testing.T) {
	h := TenantRequired()(okHandler)

	cases := []struct {
		name          string
		principal     string
		requestTenant string
		wantStatus    int
	}{
		{name: "match", principal: "acme", requestTenant: "acme", wantStatus: http.StatusOK},
		{name: "no request tenant", principal: "acme", requestTenant: "", wantStatus: http.StatusOK},
		{name: "mismatch", principal: "acme", requestTenant: "other", wantStatus: http.StatusNotFound},
		{name: "principal without tenant", principal: "", requestTenant: "acme", wantStatus: http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := withPrincipal(httptest.NewRequest(http.MethodGet, "/x", nil), tc.principal)
			if tc.requestTenant != "" {
				req = req.WithContext(WithRequestTenant(req.Context(), tc.requestTenant))
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status=%d want %d", rr.Code, tc.wantStatus)
			}
		})
	}
}

func TestTenantMatchFromPath(t *testing.T) {
	route := func() http.Handler {
		r := chi.NewRouter()
		r.With(TenantMatchFromPath("id")).Get("/api/v1/orgs/{id}", okHandler)
		return r
	}

	t.Run("passes on match", func(t *testing.T) {
		rr := httptest.NewRecorder()
		route().ServeHTTP(rr, withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/orgs/t1", nil), "t1"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
		}
	})
	t.Run("not found on mismatch", func(t *testing.T) {
		rr := httptest.NewRecorder()
		route().ServeHTTP(rr, withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/orgs/t2", nil), "t1"))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=%d", rr.Code, http.StatusNotFound)
		}
	})
	t.Run("bad request on missing param", func(t *testing.T) {
		h := policy.Chain(okHandler, TenantMatchFromPath("id"))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/orgs", nil), "t1"))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", rr.Code, http.StatusBadRequest)
		}
	})
	t.Run("unauthenticated", func(t *testing.T) {
		rr := httptest.NewRecorder()
		route().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/orgs/t1", nil))
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnauthorized)
		}
	})
}

// --- route rule (validator) ---

func validate(strict bool, pattern string, policies ...policy.Policy) error {
	return policy.ValidateRouteWith([]policy.RouteRule{RouteRule(strict)}, http.MethodGet, pattern, policies...)
}

func TestTenantPathStrictWhenTenancyEnabled(t *testing.T) {
	// A {tenant_id} route without tenant policies must fail validation.
	if err := validate(true, "/api/v1/orgs/{tenant_id}/projects", policy.AuthRequired(nil, auth.ModeHybrid)); err == nil {
		t.Fatal("expected validation error for {tenant_id} route without tenant policies when tenancy enabled")
	}
}

func TestTenantPathLenientWhenTenancyDisabled(t *testing.T) {
	// With tenancy off, {tenant_id} is an ordinary parameter: the same route
	// validates without tenant policies.
	if err := validate(false, "/api/v1/orgs/{tenant_id}/projects", policy.AuthRequired(nil, auth.ModeHybrid)); err != nil {
		t.Fatalf("expected {tenant_id} route to validate without tenant policies when tenancy disabled, got: %v", err)
	}
}

func TestTenantMatchStillRequiresTenantRequiredWhenDisabled(t *testing.T) {
	// The dependency rule holds whenever the tenant policies are explicitly
	// used, regardless of the flag.
	for _, strict := range []bool{true, false} {
		err := validate(strict, "/api/v1/orgs/{tenant_id}/projects",
			policy.AuthRequired(nil, auth.ModeHybrid), TenantMatchFromPath("tenant_id"))
		if err == nil || !strings.Contains(err.Error(), string(PolicyTypeTenantRequired)) {
			t.Fatalf("strict=%v: expected TenantMatchFromPath without TenantRequired to fail, got %v", strict, err)
		}
	}
}

func TestTenantRequiredNeedsAuthAndOrdering(t *testing.T) {
	if err := validate(true, "/x", TenantRequired()); err == nil || !strings.Contains(err.Error(), "AuthRequired") && !strings.Contains(err.Error(), string(policy.PolicyTypeAuthRequired)) {
		t.Fatalf("TenantRequired without AuthRequired must fail, got %v", err)
	}
	// TenantRequired (stage isolation) may not come after rate limiting.
	limited := policy.Annotate(func(next http.Handler) http.Handler { return next }, policy.Metadata{Type: policy.PolicyTypeRateLimit, Name: "RateLimit"})
	if err := validate(true, "/x", policy.AuthRequired(nil, auth.ModeHybrid), limited, TenantRequired()); err == nil || !strings.Contains(err.Error(), "cannot appear after") {
		t.Fatalf("tenant policy after rate limit must fail ordering, got %v", err)
	}
}

func TestStrictRouteWithTenantPoliciesPasses(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := cacheManager(t, mr.Addr())

	err := validate(true, "/api/v1/orgs/{tenant_id}/projects/{id}",
		policy.AuthRequired(nil, auth.ModeHybrid),
		TenantRequired(),
		TenantMatchFromPath("tenant_id"),
		policy.RequirePerm("project.read"),
		policy.RateLimit(allowLimiter{}, ratelimit.Rule{Limit: 10, Window: time.Minute, Scope: ScopeTenant, Keyer: KeyByTenant()}),
		policy.CacheRead(mgr, cache.CacheReadConfig{TTL: time.Minute, VaryBy: cache.CacheVaryBy{Parts: []cache.KeyPart{CacheVary()}}}),
	)
	if err != nil {
		t.Fatalf("strict valid configuration rejected: %v", err)
	}
}

// --- presets ---

type allowLimiter struct{}

func (allowLimiter) Allow(context.Context, ratelimit.Request) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: true, Outcome: ratelimit.OutcomeAllowed}, nil
}

func cacheManager(t testing.TB, addr string) *cache.Manager {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	mgr, err := cache.NewManager(client, cache.ManagerConfig{Env: "test", FailOpen: true, DefaultMaxBytes: 128})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return mgr
}

func presetDeps(t *testing.T) (*goauth.Engine, ratelimit.Limiter, *cache.Manager) {
	t.Helper()
	return &goauth.Engine{}, allowLimiter{}, cacheManager(t, miniredis.RunT(t).Addr())
}

func withEnabled(t *testing.T, enabled bool) {
	t.Helper()
	prev := Enabled()
	active.Store(enabled)
	t.Cleanup(func() { active.Store(prev) })
}

func TestTenantReadPresetPassesValidator(t *testing.T) {
	withEnabled(t, true)
	engine, limiter, mgr := presetDeps(t)

	policies := TenantRead(
		policy.WithAuthEngine(engine, auth.ModeStrict),
		policy.WithLimiter(limiter),
		policy.WithCacheManager(mgr),
		policy.WithCache(45*time.Second, cache.CacheTagSpec{Name: "project"}),
	)

	metas, err := policy.DescribePolicies(policies...)
	if err != nil {
		t.Fatalf("DescribePolicies() error = %v", err)
	}
	if err := policy.ValidateRouteMetadataWith([]policy.RouteRule{RouteRule(true)}, http.MethodGet, "/api/v1/projects/{id}", metas); err != nil {
		t.Fatalf("ValidateRouteMetadata() error = %v", err)
	}
	if len(metas) != 4 || metas[1].Type != PolicyTypeTenantRequired {
		t.Fatalf("preset order = %+v, want auth, tenant, rate limit, cache", metas)
	}
	if !metas[3].CacheRead.VaryByIdentityPart {
		t.Fatal("with tenancy on the read preset must vary its cache by the tenant part")
	}
}

func TestTenantWritePresetPassesValidator(t *testing.T) {
	withEnabled(t, true)
	engine, limiter, mgr := presetDeps(t)

	policies := TenantWrite(
		policy.WithAuthEngine(engine, auth.ModeStrict),
		policy.WithLimiter(limiter),
		policy.WithCacheManager(mgr),
		policy.WithInvalidateTags(cache.CacheTagSpec{Name: "project"}),
	)

	metas, err := policy.DescribePolicies(policies...)
	if err != nil {
		t.Fatalf("DescribePolicies() error = %v", err)
	}
	if err := policy.ValidateRouteMetadataWith([]policy.RouteRule{RouteRule(true)}, http.MethodPost, "/api/v1/projects", metas); err != nil {
		t.Fatalf("ValidateRouteMetadata() error = %v", err)
	}
}

func TestTenantReadPresetPanicsWithoutAuth(t *testing.T) {
	_, limiter, mgr := presetDeps(t)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = TenantRead(policy.WithLimiter(limiter), policy.WithCacheManager(mgr))
}

// The preset's default cache key follows the feature's switch: by tenant when
// on, by user when off (a shared entry for tenant "0" would serve one user's
// response to another). An explicit WithCacheVaryBy always wins.
func TestPresetCacheVaryDefaultFollowsTenancySwitch(t *testing.T) {
	read := func(opts ...policy.PresetOption) policy.CacheReadMetadata {
		engine, limiter, mgr := presetDeps(t)
		opts = append([]policy.PresetOption{
			policy.WithAuthEngine(engine, auth.ModeStrict), policy.WithLimiter(limiter), policy.WithCacheManager(mgr),
		}, opts...)
		metas, err := policy.DescribePolicies(TenantRead(opts...)...)
		if err != nil {
			t.Fatal(err)
		}
		return metas[3].CacheRead
	}

	withEnabled(t, true)
	if m := read(); !m.VaryByIdentityPart || m.VaryByUserID {
		t.Fatalf("tenancy on: %+v, want tenant part only", m)
	}
	withEnabled(t, false)
	if m := read(); m.VaryByIdentityPart || !m.VaryByUserID {
		t.Fatalf("tenancy off: %+v, want user only", m)
	}
	if m := read(policy.WithCacheVaryBy(cache.CacheVaryBy{UserID: true, Parts: []cache.KeyPart{CacheVary()}})); !m.VaryByUserID || !m.VaryByIdentityPart {
		t.Fatalf("explicit vary must win: %+v", m)
	}
}

// --- key parts ---

func TestCacheVaryAndTagAreScopedPerTenant(t *testing.T) {
	mr := miniredis.RunT(t)
	mgr := cacheManager(t, mr.Addr())

	requestIn := func(tenantID string) *http.Request {
		return withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/things", nil), tenantID)
	}
	cfg := cache.CacheReadConfig{TTL: time.Minute, VaryBy: cache.CacheVaryBy{Parts: []cache.KeyPart{CacheVary()}}}

	a, err := mgr.BuildReadKey(context.Background(), requestIn("acme"), "/api/v1/things", cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := mgr.BuildReadKey(context.Background(), requestIn("globex"), "/api/v1/things", cfg)
	again, _ := mgr.BuildReadKey(context.Background(), requestIn("acme"), "/api/v1/things", cfg)
	if a == b || a != again {
		t.Fatalf("keys must differ per tenant and be stable: %q %q %q", a, b, again)
	}

	spec := cache.PrepareTagSpecs([]cache.CacheTagSpec{{Name: "project", Parts: []cache.KeyPart{CacheTag()}}})
	tags, err := cache.ResolveTagNames(requestIn("acme"), spec)
	if err != nil || len(tags) != 1 || !strings.Contains(tags[0], "tenant=acme") {
		t.Fatalf("tag = %v err=%v, want scoped to tenant=acme", tags, err)
	}
	if _, err := cache.ResolveTagNames(requestIn(""), spec); err == nil {
		t.Fatal("a tag with no tenant must fail rather than resolve unscoped")
	}
}

func TestKeyByTenantAndComposite(t *testing.T) {
	req := func(tenantID string, user bool) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		principal := auth.AuthContext{}
		if user {
			principal.UserID = "u1"
		}
		if tenantID != "" {
			principal.Attributes = []auth.Attribute{{Key: AttrTenantID, Value: tenantID}}
		}
		return r.WithContext(auth.WithContext(r.Context(), principal))
	}

	if scope, id := KeyByTenant()(req("acme", true)); scope != ScopeTenant || id != "acme" {
		t.Fatalf("KeyByTenant = %s/%s", scope, id)
	}
	if scope, _ := KeyByTenant()(req("", true)); scope != ratelimit.ScopeAnon {
		t.Fatalf("no tenant must be anonymous, got %s", scope)
	}
	composite := KeyByUserOrTenantOrTokenHash(16)
	if scope, id := composite(req("acme", true)); scope != ratelimit.ScopeUser || id != "u1" {
		t.Fatalf("user first: %s/%s", scope, id)
	}
	if scope, id := composite(req("acme", false)); scope != ScopeTenant || id != "acme" {
		t.Fatalf("tenant second: %s/%s", scope, id)
	}
	plain := httptest.NewRequest(http.MethodGet, "/x", nil)
	plain.Header.Set("Authorization", "Bearer abc")
	if scope, _ := composite(plain); scope != ratelimit.ScopeToken {
		t.Fatalf("token third, got %s", scope)
	}
}
