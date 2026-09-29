package policy

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/alicebob/miniredis/v2"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/cache"
)

func TestAuthExtensionAddsAttributeAndCanReject(t *testing.T) {
	engine, token := newPolicyTestAuthEngine(t)
	UseAuthExtensions(engine, AuthExtension{
		Attribute: func(result *goauth.AuthResult) (string, string) { return "origin", "user:" + result.UserID },
		Check: func(r *http.Request, _ *goauth.AuthResult) error {
			if r.Header.Get("X-Reject") != "" {
				return errors.New("rejected by feature")
			}
			return nil
		},
	})
	t.Cleanup(func() { UseAuthExtensions(engine) })

	var seen string
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, _ := auth.FromContext(r.Context())
		seen = principal.Attribute("origin")
		w.WriteHeader(http.StatusOK)
	}), AuthRequired(engine, auth.ModeHybrid))

	do := func(reject bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/secure", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if reject {
			req.Header.Set("X-Reject", "1")
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	if rr := do(false); rr.Code != http.StatusOK || seen != "user:u1" {
		t.Fatalf("status=%d attribute=%q, want 200 and user:u1", rr.Code, seen)
	}

	seen = ""
	rr := do(true)
	if rr.Code != http.StatusUnauthorized || seen != "" {
		t.Fatalf("a failing check must give 401 without running the handler: status=%d seen=%q", rr.Code, seen)
	}
	if strings.Contains(rr.Body.String(), "rejected by feature") || !strings.Contains(rr.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("the rejection must look like any other 401: %s", rr.Body.String())
	}
}

func TestRouteRulesAndStages(t *testing.T) {
	rule := func(_, pattern string, metas []Metadata) error {
		for _, m := range metas {
			if m.Data["forbid"] != "" && strings.Contains(pattern, m.Data["forbid"]) {
				return errors.New("pattern " + pattern + " is forbidden by a feature rule")
			}
		}
		return nil
	}
	// Policies are keyed by their func value, so each needs its own literal.
	feature := Annotate(func(next http.Handler) http.Handler { return next },
		Metadata{Type: PolicyTypeCustom, Name: "Feature", Stage: StageIsolation, Data: map[string]string{"forbid": "/blocked"}})

	if err := ValidateRouteWith([]RouteRule{rule}, http.MethodGet, "/ok", feature); err != nil {
		t.Fatalf("allowed route rejected: %v", err)
	}
	if err := ValidateRouteWith([]RouteRule{rule}, http.MethodGet, "/blocked/x", feature); err == nil {
		t.Fatal("the feature rule must reject /blocked/x")
	}
	if err := ValidateRoute(http.MethodGet, "/blocked/x", feature); err != nil {
		t.Fatalf("without the rule the route is valid: %v", err)
	}

	// A feature policy at StageIsolation may not follow rate limiting.
	limited := Annotate(func(next http.Handler) http.Handler { return next },
		Metadata{Type: PolicyTypeRateLimit, Name: "RateLimit"})
	if err := ValidateRoute(http.MethodGet, "/ok", limited, feature); err == nil || !strings.Contains(err.Error(), "cannot appear after") {
		t.Fatalf("stage ordering must apply to feature policies, got %v", err)
	}
}

func TestCacheReadIdentityPartSatisfiesAuthSafety(t *testing.T) {
	read := func(vary CacheReadMetadata) error {
		metas := []Metadata{
			{Type: PolicyTypeAuthRequired, Name: "AuthRequired"},
			{Type: PolicyTypeCacheRead, Name: "CacheRead", CacheRead: vary},
		}
		return ValidateRouteMetadata(http.MethodGet, "/x", metas)
	}
	if err := read(CacheReadMetadata{}); err == nil {
		t.Fatal("an authenticated cache read that varies by nothing must be rejected")
	}
	if err := read(CacheReadMetadata{VaryByIdentityPart: true}); err != nil {
		t.Fatalf("an identity-bearing part must satisfy the rule: %v", err)
	}
	if err := read(CacheReadMetadata{VaryByUserID: true}); err != nil {
		t.Fatalf("UserID must still satisfy the rule: %v", err)
	}
}

func TestCacheReadRejectsInvalidVaryPart(t *testing.T) {
	mgr := newCacheManagerForPolicyTests(t, miniredis.RunT(t).Addr(), true)
	defer func() {
		if recover() == nil {
			t.Fatal("CacheRead must panic on a part without an extractor")
		}
	}()
	CacheRead(mgr, cache.CacheReadConfig{TTL: time.Minute, VaryBy: cache.CacheVaryBy{Parts: []cache.KeyPart{{Name: "org"}}}})
}
