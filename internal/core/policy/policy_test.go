package policy

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestChainOrderDeterministic(t *testing.T) {
	steps := make([]string, 0, 8)

	record := func(name string) Policy {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				steps = append(steps, name+":before")
				next.ServeHTTP(w, r)
				steps = append(steps, name+":after")
			})
		}
	}

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			steps = append(steps, "handler")
			w.WriteHeader(http.StatusNoContent)
		}),
		record("p1"),
		record("p2"),
		record("p3"),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusNoContent)
	}

	want := []string{"p1:before", "p2:before", "p3:before", "handler", "p3:after", "p2:after", "p1:after"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps=%v want=%v", steps, want)
	}
}

func TestChainShortCircuit(t *testing.T) {
	handlerCalled := false

	deny := func(status int) Policy {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "denied", status)
			})
		}
	}

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		deny(http.StatusForbidden),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusForbidden)
	}
	if handlerCalled {
		t.Fatalf("expected handler not called on short-circuit")
	}
}

func TestChainEmptyPoliciesPassThrough(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusAccepted)
	}
}

func TestRequireJSONRejectsNonJSONForBodyMethods(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), RequireJSON())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Type", "text/plain")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnsupportedMediaType)
	}
	if !strings.Contains(rr.Body.String(), `"code":"unsupported_media_type"`) {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestRequireJSONAllowsJSONCharset(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}), RequireJSON())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusCreated)
	}
}

func TestRequireJSONRejectsJSONPrefixLookalike(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}), RequireJSON())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Type", "application/jsonx")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnsupportedMediaType)
	}
}

func TestWithHeaderSetsResponseHeader(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), WithHeader("X-Test", "ok"))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Test"); got != "ok" {
		t.Fatalf("header X-Test=%q want=%q", got, "ok")
	}
}

func TestCacheControlSetsCacheHeaders(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), CacheControl(CacheControlConfig{
		Public:       true,
		MaxAge:       60 * time.Second,
		SharedMaxAge: 120 * time.Second,
		Immutable:    true,
		Vary:         []string{"Accept-Encoding", "Accept-Language", "accept-encoding"},
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rr, req)

	cacheControl := rr.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "public") {
		t.Fatalf("cache-control missing public directive: %q", cacheControl)
	}
	if !strings.Contains(cacheControl, "max-age=60") {
		t.Fatalf("cache-control missing max-age directive: %q", cacheControl)
	}
	if !strings.Contains(cacheControl, "s-maxage=120") {
		t.Fatalf("cache-control missing s-maxage directive: %q", cacheControl)
	}
	if !strings.Contains(cacheControl, "immutable") {
		t.Fatalf("cache-control missing immutable directive: %q", cacheControl)
	}

	vary := rr.Header().Values("Vary")
	if len(vary) == 0 {
		t.Fatalf("expected vary header to be set")
	}
	joined := strings.Join(vary, ",")
	if !strings.Contains(strings.ToLower(joined), "accept-encoding") {
		t.Fatalf("vary header missing accept-encoding: %q", joined)
	}
	if !strings.Contains(strings.ToLower(joined), "accept-language") {
		t.Fatalf("vary header missing accept-language: %q", joined)
	}
}

func TestCacheControlPanicsOnInvalidConfig(t *testing.T) {
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatalf("expected invalid cache-control config panic")
		}
	}()

	_ = CacheControl(CacheControlConfig{Public: true, Private: true})
}
