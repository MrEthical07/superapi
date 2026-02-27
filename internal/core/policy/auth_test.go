package policy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

type mockProvider struct {
	principal auth.AuthContext
	err       error
}

func (m mockProvider) Authenticate(ctx context.Context, token string, mode auth.Mode) (auth.AuthContext, error) {
	if token == "valid-token" && m.err == nil {
		return m.principal, nil
	}
	if m.err != nil {
		return auth.AuthContext{}, m.err
	}
	return auth.AuthContext{}, auth.ErrUnauthenticated
}

func TestAuthRequiredMissingTokenUnauthorized(t *testing.T) {
	handlerCalled := false
	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(mockProvider{}, auth.ModeHybrid),
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
	provider := mockProvider{principal: auth.AuthContext{UserID: "u1", TenantID: "t1", Role: "admin", Permissions: []string{"system.whoami"}}}

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
		AuthRequired(provider, auth.ModeHybrid),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusOK)
	}
}

func TestRequirePermForbiddenWhenMissing(t *testing.T) {
	provider := mockProvider{principal: auth.AuthContext{UserID: "u1", Permissions: []string{"a.read"}}}

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(provider, auth.ModeHybrid),
		RequirePerm("a.write"),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusForbidden)
	}
	if !strings.Contains(rr.Body.String(), `"code":"forbidden"`) {
		t.Fatalf("expected forbidden code, got body=%s", rr.Body.String())
	}
}

func TestRequireRoleForbiddenWhenMissing(t *testing.T) {
	provider := mockProvider{principal: auth.AuthContext{UserID: "u1", Role: "viewer"}}

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(provider, auth.ModeHybrid),
		RequireRole("admin"),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusForbidden)
	}
}

func TestAuthRequiredNoSecretLeakOnFailure(t *testing.T) {
	provider := mockProvider{err: errors.New("token signature mismatch: very-secret")}

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthRequired(provider, auth.ModeHybrid),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnauthorized)
	}
	if strings.Contains(rr.Body.String(), "very-secret") {
		t.Fatalf("response leaked secret: %s", rr.Body.String())
	}
}
