package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

type fakeDirectory struct {
	tenants map[string]Record
	err     error
	calls   atomic.Int32
}

func (d *fakeDirectory) Get(_ context.Context, id string) (Record, error) {
	d.calls.Add(1)
	if d.err != nil {
		return Record{}, d.err
	}
	rec, ok := d.tenants[id]
	if !ok {
		return Record{}, ErrTenantNotFound
	}
	return rec, nil
}

func newDirectory() *fakeDirectory {
	return &fakeDirectory{tenants: map[string]Record{
		"acme":    {ID: "acme", Status: StatusActive},
		"dormant": {ID: "dormant", Status: StatusInactive},
	}}
}

// echoTenant writes the tenant the downstream handler observed.
var echoTenant = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.RequestTenantFromContext(r.Context())
	_, _ = w.Write([]byte(id))
})

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode error body %q: %v", body, err)
	}
	return env.Error.Code + ":" + env.Error.Message
}

func TestMiddlewareHeaderResolver(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		headers    []string
		dir        *fakeDirectory
		wantStatus int
		wantBody   string
		wantError  string
	}{
		{name: "valid tenant", path: "/api/v1/x", headers: []string{"acme"}, dir: newDirectory(), wantStatus: 200, wantBody: "acme"},
		{name: "missing header", path: "/api/v1/x", dir: newDirectory(), wantStatus: 400, wantError: "bad_request:tenant required"},
		{name: "repeated header", path: "/api/v1/x", headers: []string{"acme", "other"}, dir: newDirectory(), wantStatus: 400, wantError: "bad_request:tenant required"},
		{name: "malformed tenant", path: "/api/v1/x", headers: []string{"../etc"}, dir: newDirectory(), wantStatus: 400, wantError: "bad_request:tenant invalid"},
		{name: "unknown tenant", path: "/api/v1/x", headers: []string{"nope"}, dir: newDirectory(), wantStatus: 404, wantError: "not_found:tenant not found"},
		{name: "inactive tenant", path: "/api/v1/x", headers: []string{"dormant"}, dir: newDirectory(), wantStatus: 404, wantError: "not_found:tenant not found"},
		{name: "directory failure", path: "/api/v1/x", headers: []string{"acme"}, dir: &fakeDirectory{err: errors.New("db down")}, wantStatus: 503, wantError: "dependency_unavailable:tenant lookup unavailable"},
		{name: "healthz exempt", path: "/healthz", dir: newDirectory(), wantStatus: 200, wantBody: ""},
		{name: "readyz exempt", path: "/readyz", dir: newDirectory(), wantStatus: 200, wantBody: ""},
		{name: "metrics exempt", path: "/metrics", dir: newDirectory(), wantStatus: 200, wantBody: ""},
		{name: "no validation", path: "/api/v1/x", headers: []string{"anything"}, dir: nil, wantStatus: 200, wantBody: "anything"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ResolverConfig{
				Resolver:    ResolverHeader,
				Header:      "X-Tenant-ID",
				ExemptPaths: []string{"/healthz", "/readyz", "/metrics"},
			}
			if tc.dir != nil {
				cfg.Directory = tc.dir
			}
			h := Middleware(cfg)(echoTenant)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			for _, v := range tc.headers {
				req.Header.Add("X-Tenant-ID", v)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("status=%d want %d body=%s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if tc.wantError != "" {
				if got := errorCode(t, rr.Body.Bytes()); got != tc.wantError {
					t.Fatalf("error=%q want %q", got, tc.wantError)
				}
				return
			}
			if rr.Body.String() != tc.wantBody {
				t.Fatalf("body=%q want %q", rr.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestMiddlewareSubdomainResolver(t *testing.T) {
	h := Middleware(ResolverConfig{Resolver: ResolverSubdomain, BaseDomain: "example.com", Directory: newDirectory()})(echoTenant)
	cases := []struct {
		host       string
		wantStatus int
		wantBody   string
	}{
		{host: "acme.example.com", wantStatus: 200, wantBody: "acme"},
		{host: "ACME.example.com:8443", wantStatus: 200, wantBody: "acme"},
		{host: "example.com", wantStatus: 400},
		{host: "a.b.example.com", wantStatus: 400},
		{host: "acme.evil.com", wantStatus: 400},
		{host: "acmeexample.com", wantStatus: 400},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
			req.Host = tc.host
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status=%d want %d", rr.Code, tc.wantStatus)
			}
			if tc.wantBody != "" && rr.Body.String() != tc.wantBody {
				t.Fatalf("body=%q want %q", rr.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestMiddlewareCachesValidation(t *testing.T) {
	dir := newDirectory()
	h := Middleware(ResolverConfig{Header: "X-Tenant-ID", Directory: dir, CacheTTL: time.Minute})(echoTenant)
	for i := 0; i < 5; i++ {
		for _, id := range []string{"acme", "unknown"} {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Header.Set("X-Tenant-ID", id)
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
	}
	if got := dir.calls.Load(); got != 2 {
		t.Fatalf("directory calls=%d want 2 (positive and negative results cached)", got)
	}
}

func TestValidationCacheExpiry(t *testing.T) {
	c := newValidationCache(time.Second)
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	c.put("acme", true)
	if active, ok := c.get("acme"); !ok || !active {
		t.Fatal("expected fresh cache hit")
	}
	now = now.Add(2 * time.Second)
	if _, ok := c.get("acme"); ok {
		t.Fatal("expected expired entry to miss")
	}
}

func TestValidTenantID(t *testing.T) {
	valid := []string{"0", "acme", "Acme-01", "a.b_c-d"}
	invalid := []string{"", "-acme", ".acme", "acme corp", "acme/1", "ünïcode", string(make([]byte, 65))}
	for _, id := range valid {
		if !ValidTenantID(id) {
			t.Errorf("ValidTenantID(%q)=false want true", id)
		}
	}
	for _, id := range invalid {
		if ValidTenantID(id) {
			t.Errorf("ValidTenantID(%q)=true want false", id)
		}
	}
}
