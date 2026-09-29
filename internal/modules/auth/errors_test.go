package auth

import (
	"fmt"
	"net/http"
	"testing"

	goauth "github.com/MrEthical07/goAuth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

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

func TestMapFlowErrorGoAuthV06Sentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   apperr.Code
		wantMsg    string
	}{
		{
			name:       "totp already enabled maps to conflict",
			err:        goauth.ErrTOTPAlreadyEnabled,
			wantStatus: http.StatusConflict,
			wantCode:   apperr.CodeConflict,
			wantMsg:    "totp is already enabled; disable it before enrolling again",
		},
		{
			name:       "totp already enabled through a wrapped error",
			err:        fmt.Errorf("setup: %w", goauth.ErrTOTPAlreadyEnabled),
			wantStatus: http.StatusConflict,
			wantCode:   apperr.CodeConflict,
			wantMsg:    "totp is already enabled; disable it before enrolling again",
		},
		{
			name:       "password verify rate limit maps to too many requests",
			err:        goauth.ErrPasswordVerifyRateLimited,
			wantStatus: http.StatusTooManyRequests,
			wantCode:   apperr.CodeTooManyRequests,
			wantMsg:    "authentication temporarily limited",
		},
		{
			name:       "password verify rate limit through a wrapped error",
			err:        fmt.Errorf("verify: %w", goauth.ErrPasswordVerifyRateLimited),
			wantStatus: http.StatusTooManyRequests,
			wantCode:   apperr.CodeTooManyRequests,
			wantMsg:    "authentication temporarily limited",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			appErr, ok := apperr.AsAppError(mapFlowError(tc.err))
			if !ok {
				t.Fatalf("expected AppError")
			}
			if appErr.StatusCode != tc.wantStatus || appErr.Code != tc.wantCode || appErr.Message != tc.wantMsg {
				t.Fatalf("got %d/%s/%q want %d/%s/%q", appErr.StatusCode, appErr.Code, appErr.Message, tc.wantStatus, tc.wantCode, tc.wantMsg)
			}
			if appErr.Cause == nil {
				t.Fatalf("expected the goAuth error kept as the cause")
			}
		})
	}
}

// The service's own pre-check and goAuth's sentinel must produce the same
// response, so a client cannot tell which layer refused.
func TestTOTPAlreadyEnabledResponsesAreIdentical(t *testing.T) {
	t.Parallel()

	pre := totpAlreadyEnabledErr()
	mapped, ok := apperr.AsAppError(mapFlowError(goauth.ErrTOTPAlreadyEnabled))
	if !ok {
		t.Fatalf("expected AppError")
	}
	if pre.StatusCode != mapped.StatusCode || pre.Code != mapped.Code || pre.Message != mapped.Message {
		t.Fatalf("pre-check %d/%s/%q != mapped %d/%s/%q", pre.StatusCode, pre.Code, pre.Message, mapped.StatusCode, mapped.Code, mapped.Message)
	}
}
