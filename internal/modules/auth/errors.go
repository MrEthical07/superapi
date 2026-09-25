package auth

import (
	"errors"
	"net/http"

	goauth "github.com/MrEthical07/goAuth"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

// mapAuthEndpointError maps goAuth errors from credential endpoints (login,
// refresh, logout, MFA confirm, WebAuthn) onto API errors by category. Every
// validation failure collapses to invalidMessage so responses never reveal
// why a credential was rejected.
func mapAuthEndpointError(err error, invalidMessage string) error {
	if err == nil {
		return nil
	}

	var authErr *goauth.AuthError
	if errors.As(err, &authErr) {
		switch authErr.Category {
		case goauth.CategoryAuthAbuse:
			return apperr.WithCause(apperr.New(apperr.CodeTooManyRequests, http.StatusTooManyRequests, "authentication temporarily limited"), err)
		case goauth.CategoryAuthState:
			return apperr.WithCause(apperr.New(apperr.CodeForbidden, http.StatusForbidden, "authentication state rejected"), err)
		case goauth.CategorySystem:
			if authErr.Code == string(goauth.CodeSystemInternalError) {
				return apperr.WithCause(apperr.New(apperr.CodeInternal, http.StatusInternalServerError, "authentication failed"), err)
			}
			return apperr.WithCause(apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "authentication unavailable"), err)
		default:
			return apperr.WithCause(apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, invalidMessage), err)
		}
	}

	if errors.Is(err, goauth.ErrLoginRateLimited) {
		return apperr.WithCause(apperr.New(apperr.CodeTooManyRequests, http.StatusTooManyRequests, "authentication temporarily limited"), err)
	}

	return apperr.WithCause(apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, invalidMessage), err)
}

// mapFlowError maps goAuth errors from account-lifecycle endpoints (register,
// reset, verification, password change, TOTP). User-correctable input errors
// become 400s (never 401, so clients do not mistake them for an expired
// session); everything else falls back to mapAuthEndpointError.
func mapFlowError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, goauth.ErrPasswordPolicy):
		return apperr.WithCause(badRequestErr("password does not meet the password policy"), err)
	case errors.Is(err, goauth.ErrPasswordReuse):
		return apperr.WithCause(badRequestErr("new password must differ from the current password"), err)
	case errors.Is(err, goauth.ErrPasswordResetInvalid), errors.Is(err, goauth.ErrEmailVerificationInvalid):
		return apperr.WithCause(badRequestErr("invalid or expired challenge"), err)
	case errors.Is(err, goauth.ErrInvalidCredentials):
		return apperr.WithCause(badRequestErr("current password is incorrect"), err)
	case errors.Is(err, goauth.ErrTOTPInvalid), errors.Is(err, goauth.ErrBackupCodeInvalid), errors.Is(err, goauth.ErrMFALoginInvalid):
		return apperr.WithCause(badRequestErr("invalid code"), err)
	case errors.Is(err, goauth.ErrTOTPRequired), errors.Is(err, goauth.ErrMFALoginRequired):
		return apperr.WithCause(badRequestErr("a second-factor code is required"), err)
	case errors.Is(err, goauth.ErrTOTPNotConfigured),
		errors.Is(err, goauth.ErrBackupCodesNotConfigured),
		errors.Is(err, goauth.ErrBackupCodeRegenerationRequiresTOTP):
		return apperr.WithCause(apperr.New(apperr.CodeConflict, http.StatusConflict, "totp is not enabled for this account"), err)
	case errors.Is(err, goauth.ErrPasswordResetDisabled),
		errors.Is(err, goauth.ErrEmailVerificationDisabled),
		errors.Is(err, goauth.ErrTOTPFeatureDisabled),
		errors.Is(err, goauth.ErrAccountCreationDisabled):
		// Routes are only registered when their feature flag is on, so this
		// means goAuth config and route flags diverged. Behave as absent.
		return apperr.WithCause(apperr.New(apperr.CodeNotFound, http.StatusNotFound, "not found"), err)
	case errors.Is(err, goauth.ErrUserNotFound):
		return apperr.WithCause(apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required"), err)
	}
	return mapAuthEndpointError(err, "request rejected")
}

func badRequestErr(msg string) *apperr.AppError {
	return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, msg)
}
