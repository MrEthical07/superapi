package system

import (
	"net/http"
	"net/http/httptest"
	"testing"

	goauth "github.com/MrEthical07/goAuth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/httpx"
)

func TestWhoamiRequiresAuth(t *testing.T) {
	m := &Module{}
	m.BindDependencies(nil)

	r := httpx.NewMux()
	if err := m.Register(r); err != nil {
		t.Fatalf("register: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/whoami", nil)
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want=%d", rr.Code, http.StatusUnauthorized)
	}
}

func TestMapAuthEndpointErrorByCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		invalidMsg    string
		wantStatus    int
		wantCode      apperr.Code
		wantPublicMsg string
	}{
		{
			name:          "auth abuse maps to too many requests",
			err:           goauth.NewAuthError(goauth.CategoryAuthAbuse, string(goauth.CodeAuthTooManyAttempts), "too many attempts"),
			invalidMsg:    "invalid credentials",
			wantStatus:    http.StatusTooManyRequests,
			wantCode:      apperr.CodeTooManyRequests,
			wantPublicMsg: "authentication temporarily limited",
		},
		{
			name:          "auth state maps to forbidden",
			err:           goauth.NewAuthError(goauth.CategoryAuthState, string(goauth.CodeAuthAccountLocked), "account locked"),
			invalidMsg:    "invalid credentials",
			wantStatus:    http.StatusForbidden,
			wantCode:      apperr.CodeForbidden,
			wantPublicMsg: "authentication state rejected",
		},
		{
			name:          "system internal maps to internal",
			err:           goauth.NewAuthError(goauth.CategorySystem, string(goauth.CodeSystemInternalError), "internal error"),
			invalidMsg:    "invalid credentials",
			wantStatus:    http.StatusInternalServerError,
			wantCode:      apperr.CodeInternal,
			wantPublicMsg: "authentication failed",
		},
		{
			name:          "system unavailable maps to dependency failure",
			err:           goauth.NewAuthError(goauth.CategorySystem, string(goauth.CodeSystemUnavailable), "service unavailable"),
			invalidMsg:    "invalid credentials",
			wantStatus:    http.StatusServiceUnavailable,
			wantCode:      apperr.CodeDependencyFailure,
			wantPublicMsg: "authentication unavailable",
		},
		{
			name:          "auth validation keeps endpoint invalid message",
			err:           goauth.NewAuthError(goauth.CategoryAuthValidation, string(goauth.CodeAuthInvalidCredentials), "invalid credentials"),
			invalidMsg:    "invalid credentials",
			wantStatus:    http.StatusUnauthorized,
			wantCode:      apperr.CodeUnauthorized,
			wantPublicMsg: "invalid credentials",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := mapAuthEndpointError(tc.err, tc.invalidMsg)
			appErr, ok := apperr.AsAppError(got)
			if !ok {
				t.Fatalf("expected AppError, got %T", got)
			}

			if appErr.StatusCode != tc.wantStatus {
				t.Fatalf("status=%d want=%d", appErr.StatusCode, tc.wantStatus)
			}
			if appErr.Code != tc.wantCode {
				t.Fatalf("code=%s want=%s", appErr.Code, tc.wantCode)
			}
			if appErr.Message != tc.wantPublicMsg {
				t.Fatalf("message=%q want=%q", appErr.Message, tc.wantPublicMsg)
			}
			if appErr.Cause == nil {
				t.Fatalf("expected wrapped cause for %q", tc.name)
			}
		})
	}
}

func TestMapAuthEndpointErrorLegacyRateLimitFallback(t *testing.T) {
	t.Parallel()

	got := mapAuthEndpointError(goauth.ErrLoginRateLimited, "invalid credentials")
	appErr, ok := apperr.AsAppError(got)
	if !ok {
		t.Fatalf("expected AppError, got %T", got)
	}

	if appErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status=%d want=%d", appErr.StatusCode, http.StatusTooManyRequests)
	}
	if appErr.Code != apperr.CodeTooManyRequests {
		t.Fatalf("code=%s want=%s", appErr.Code, apperr.CodeTooManyRequests)
	}
}
