package tenancy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serveHost(h http.Handler, host string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
	req.Host = host
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func subdomainHandler(dir Directory, ttl time.Duration) http.Handler {
	return Middleware(ResolverConfig{Resolver: ResolverSubdomain, BaseDomain: "example.com", Directory: dir, CacheTTL: ttl})(echoTenant)
}

// The subdomain is a slug: it is looked up in tenants.slug and the tenant's id
// is what reaches the request context.
func TestSubdomainResolverAttachesTheTenantIDNotTheSlug(t *testing.T) {
	h := subdomainHandler(newDirectory(), 0)

	rr := serveHost(h, "globex.example.com")
	if rr.Code != http.StatusOK || rr.Body.String() != "t-42" {
		t.Fatalf("status=%d body=%q, want 200 and the tenant id t-42", rr.Code, rr.Body.String())
	}

	// Slug and id are different namespaces: the id is not a valid subdomain
	// for that tenant, and the slug is not a valid header value.
	if rr := serveHost(h, "t-42.example.com"); rr.Code != http.StatusNotFound {
		t.Fatalf("subdomain spelled as the tenant id: status=%d, want 404", rr.Code)
	}
}

func TestSubdomainResolverRejections(t *testing.T) {
	h := subdomainHandler(newDirectory(), 0)
	cases := []struct {
		name, host string
		wantStatus int
		wantCode   string
	}{
		{"unknown slug", "nobody.example.com", http.StatusNotFound, "not_found:tenant not found"},
		{"inactive tenant", "dormant.example.com", http.StatusNotFound, "not_found:tenant not found"},
		{"no subdomain", "example.com", http.StatusBadRequest, "bad_request:tenant required"},
		{"nested subdomain", "a.acme.example.com", http.StatusBadRequest, "bad_request:tenant required"},
		{"foreign domain", "acme.evil.com", http.StatusBadRequest, "bad_request:tenant required"},
		{"hyphen first", "-acme.example.com", http.StatusBadRequest, "bad_request:tenant invalid"},
		{"space in label", "ac me.example.com", http.StatusBadRequest, "bad_request:tenant invalid"},
		{"slash in label", "ac/me.example.com", http.StatusBadRequest, "bad_request:tenant invalid"},
		{"too long", strings.Repeat("a", 65) + ".example.com", http.StatusBadRequest, "bad_request:tenant invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := serveHost(h, tc.host)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status=%d want %d (%s)", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if got := errorCode(t, rr.Body.Bytes()); got != tc.wantCode {
				t.Fatalf("code=%q want %q", got, tc.wantCode)
			}
		})
	}
}

// Host names are case-insensitive: the label is lower-cased before the lookup.
func TestSubdomainResolverLowerCasesTheSlug(t *testing.T) {
	dir := newDirectory()
	h := subdomainHandler(dir, 0)
	for _, host := range []string{"GLOBEX.example.com", "Globex.Example.COM:8443", "globex.example.com."} {
		rr := serveHost(h, host)
		if rr.Code != http.StatusOK || rr.Body.String() != "t-42" {
			t.Fatalf("host %q: status=%d body=%q", host, rr.Code, rr.Body.String())
		}
	}
}

func TestValidSlugAndNormalizeSlug(t *testing.T) {
	for _, s := range []string{"acme", "a", "0", "acme-01", "a.b_c-d", "xn--nxasmq6b"} {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "Acme", "-acme", ".acme", "acme corp", "acme/1", "ünï", strings.Repeat("a", 65)} {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true, want false", s)
		}
	}
	for in, want := range map[string]string{"  Acme ": "acme", "ACME-01": "acme-01", "acme": "acme", "": ""} {
		if got := NormalizeSlug(in); got != want {
			t.Errorf("NormalizeSlug(%q) = %q, want %q", in, got, want)
		}
	}
	// A normalized, valid tenant id is a valid slug.
	if !ValidSlug(NormalizeSlug(" Acme-01 ")) {
		t.Error("a normalized slug must validate")
	}
}

// The cache is keyed by resolver and value, and remembers the id: a repeat
// request costs no lookup and still attaches the id.
func TestSubdomainResolverCachesBySlugAndKeepsTheID(t *testing.T) {
	dir := newDirectory()
	h := subdomainHandler(dir, time.Minute)
	for i := 0; i < 5; i++ {
		rr := serveHost(h, "globex.example.com")
		if rr.Code != http.StatusOK || rr.Body.String() != "t-42" {
			t.Fatalf("request %d: status=%d body=%q", i, rr.Code, rr.Body.String())
		}
		if rr := serveHost(h, "nobody.example.com"); rr.Code != http.StatusNotFound {
			t.Fatalf("request %d: unknown slug status=%d", i, rr.Code)
		}
	}
	if got := dir.slugCalls.Load(); got != 2 {
		t.Fatalf("slug lookups = %d, want 2 (one per distinct slug, positive and negative cached)", got)
	}
	if got := dir.calls.Load(); got != 2 {
		t.Fatalf("total lookups = %d, want 2", got)
	}
}

// One value, two resolvers, two different tenants: an id "acme" and a slug
// "acme" must not share a cache entry.
func TestCacheKeysSeparateIDsFromSlugs(t *testing.T) {
	dir := &fakeDirectory{tenants: map[string]Record{
		"acme":  {ID: "acme", Slug: "zeta", Status: StatusActive},
		"other": {ID: "other", Slug: "acme", Status: StatusActive},
	}}
	cache := newValidationCache(time.Minute)

	byID, active, err := lookupTenant(t.Context(), dir, cache, ResolverHeader, "acme")
	if err != nil || !active || byID != "acme" {
		t.Fatalf("header 'acme': id=%q active=%v err=%v", byID, active, err)
	}
	bySlug, active, err := lookupTenant(t.Context(), dir, cache, ResolverSubdomain, "acme")
	if err != nil || !active || bySlug != "other" {
		t.Fatalf("subdomain 'acme': id=%q active=%v err=%v (a shared cache entry would return 'acme')", bySlug, active, err)
	}
	// Both are now cached independently.
	before := dir.calls.Load()
	if id, _, _ := lookupTenant(t.Context(), dir, cache, ResolverHeader, "acme"); id != "acme" {
		t.Fatalf("cached header lookup = %q", id)
	}
	if id, _, _ := lookupTenant(t.Context(), dir, cache, ResolverSubdomain, "acme"); id != "other" {
		t.Fatalf("cached subdomain lookup = %q", id)
	}
	if dir.calls.Load() != before {
		t.Fatal("cached lookups must not reach the directory")
	}

	if cacheKey(ResolverHeader, "acme") == cacheKey(ResolverSubdomain, "acme") {
		t.Fatal("cache keys must include the resolver")
	}
}

// The header resolver is unchanged: it takes an id, never a slug.
func TestHeaderResolverStillUsesTheTenantID(t *testing.T) {
	h := Middleware(ResolverConfig{Header: "X-Tenant-ID", Directory: newDirectory()})(echoTenant)
	for header, want := range map[string]int{"t-42": http.StatusOK, "globex": http.StatusNotFound} {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("X-Tenant-ID", header)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != want {
			t.Fatalf("X-Tenant-ID %q: status=%d want %d", header, rr.Code, want)
		}
	}
}

// Without a directory a slug cannot become an id, so the subdomain resolver
// fails closed instead of attaching the slug.
func TestSubdomainResolverWithoutDirectoryFailsClosed(t *testing.T) {
	h := Middleware(ResolverConfig{Resolver: ResolverSubdomain, BaseDomain: "example.com"})(echoTenant)
	rr := serveHost(h, "globex.example.com")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%q, want 503", rr.Code, rr.Body.String())
	}
	if rr.Body.String() == "globex" {
		t.Fatal("the slug must never be attached as the tenant")
	}
	if got := errorCode(t, rr.Body.Bytes()); got != "dependency_unavailable:tenant lookup unavailable" {
		t.Fatalf("code=%q", got)
	}
}

func TestSubdomainResolverDirectoryErrorIs503(t *testing.T) {
	dir := newDirectory()
	dir.err = http.ErrHandlerTimeout
	rr := serveHost(subdomainHandler(dir, 0), "globex.example.com")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", rr.Code)
	}
}
