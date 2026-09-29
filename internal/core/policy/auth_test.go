package policy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

func TestAuthRequiredMissingTokenUnauthorized(t *testing.T) {
	engine, _ := newPolicyTestAuthEngine(t)

	handlerCalled := false
	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(engine, auth.ModeHybrid),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnauthorized)
	}
	if handlerCalled {
		t.Fatalf("expected handler not called")
	}
	if !strings.Contains(rr.Body.String(), `"code":"unauthorized"`) {
		t.Fatalf("expected unauthorized error code, got body=%s", rr.Body.String())
	}
}

func TestAuthRequiredValidTokenInjectsContext(t *testing.T) {
	engine, token := newPolicyTestAuthEngine(t)

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.FromContext(r.Context())
			if !ok {
				t.Fatalf("expected principal in context")
			}
			if principal.UserID != "u1" {
				t.Fatalf("principal.user_id=%q want=%q", principal.UserID, "u1")
			}
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(engine, auth.ModeHybrid),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
	}
}

func TestRequirePermForbiddenWhenMissing(t *testing.T) {
	engine, token := newPolicyTestAuthEngine(t)

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(engine, auth.ModeHybrid),
		RequirePerm("project.write"),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusForbidden)
	}
	if !strings.Contains(rr.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("expected forbidden code, got body=%s", rr.Body.String())
	}
}

func TestAuthRequiredNoSecretLeakOnFailure(t *testing.T) {
	engine, _ := newPolicyTestAuthEngine(t)

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(engine, auth.ModeHybrid),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer token-signature-mismatch")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnauthorized)
	}
	if strings.Contains(strings.ToLower(rr.Body.String()), "secret") {
		t.Fatalf("response leaked secret: %s", rr.Body.String())
	}
}

func TestRequireAnyPermForbiddenWhenMissingAll(t *testing.T) {
	engine, token := newPolicyTestAuthEngine(t)

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(engine, auth.ModeHybrid),
		RequireAnyPerm("project.write", "project.admin"),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusForbidden)
	}
}

func TestRequireAnyPermAllowsWhenAnyPresent(t *testing.T) {
	engine, token := newPolicyTestAuthEngine(t)

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(engine, auth.ModeHybrid),
		RequireAnyPerm("project.write", "system.whoami"),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
	}
}
