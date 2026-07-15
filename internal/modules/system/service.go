package system

import (
	"context"
	"net/http"
	"strings"

	goauth "github.com/MrEthical07/goAuth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
)

// authService owns the auth workflows for the system module (login, refresh,
// logout, MFA confirm). Handlers stay thin and transport-only; the service
// mediates all calls to the goAuth engine, mirroring the enforced
// handler -> service architecture.
type authService struct {
	engine *goauth.Engine
}

// newAuthService constructs the service over a goAuth engine. The engine may be
// nil (auth disabled); callers surface a dependency error in that case.
func newAuthService(engine *goauth.Engine) *authService {
	return &authService{engine: engine}
}

func (s *authService) requireEngine() (*goauth.Engine, error) {
	if s == nil || s.engine == nil {
		return nil, apperr.New(apperr.CodeDependencyFailure, http.StatusServiceUnavailable, "auth engine unavailable")
	}
	return s.engine, nil
}

// loginOutcome carries the result of a login attempt: either tokens, or an MFA
// challenge the caller must complete.
type loginOutcome struct {
	AccessToken  string
	RefreshToken string
	MFARequired  bool
	MFAType      string
	MFASession   string
	MFATypes     []string
}

// login authenticates a user, honoring remember-me. When the account requires a
// second factor it returns an outcome with MFARequired set instead of tokens.
func (s *authService) login(ctx context.Context, identifier, password string, rememberMe bool) (loginOutcome, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return loginOutcome{}, err
	}

	result, err := engine.LoginWithOptions(ctx, strings.TrimSpace(identifier), password, goauth.LoginOptions{
		RememberMe: rememberMe,
	})
	if err != nil {
		return loginOutcome{}, mapAuthEndpointError(err, "invalid credentials")
	}

	return loginOutcome{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		MFARequired:  result.MFARequired,
		MFAType:      result.MFAType,
		MFASession:   result.MFASession,
		MFATypes:     append([]string(nil), result.MFATypes...),
	}, nil
}

// confirmMFA completes an MFA challenge with the given factor type and code.
func (s *authService) confirmMFA(ctx context.Context, challengeID, code, mfaType string) (loginOutcome, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return loginOutcome{}, err
	}

	result, err := engine.ConfirmLoginMFAWithType(ctx, strings.TrimSpace(challengeID), strings.TrimSpace(code), strings.TrimSpace(mfaType))
	if err != nil {
		return loginOutcome{}, mapAuthEndpointError(err, "invalid mfa challenge")
	}

	return loginOutcome{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		MFARequired:  result.MFARequired,
		MFAType:      result.MFAType,
		MFASession:   result.MFASession,
		MFATypes:     append([]string(nil), result.MFATypes...),
	}, nil
}

// refresh exchanges a refresh token for a new token pair.
func (s *authService) refresh(ctx context.Context, refreshToken string) (accessToken, newRefreshToken string, err error) {
	engine, err := s.requireEngine()
	if err != nil {
		return "", "", err
	}

	accessToken, newRefreshToken, err = engine.Refresh(ctx, strings.TrimSpace(refreshToken))
	if err != nil {
		return "", "", mapAuthEndpointError(err, "invalid refresh token")
	}
	return accessToken, newRefreshToken, nil
}

// logout revokes the session behind an access token. goAuth v0.4.0 accepts an
// expired-but-authentic access token and returns nil; only a structurally
// invalid token is rejected.
func (s *authService) logout(ctx context.Context, accessToken string) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}

	if err := engine.LogoutByAccessToken(ctx, strings.TrimSpace(accessToken)); err != nil {
		return mapAuthEndpointError(err, "invalid access token")
	}
	return nil
}

// --- WebAuthn ceremonies (scaffolded; return an error when WebAuthn is off) ---
//
// These relay JSON to/from goAuth's WebAuthn engine. When WEBAUTHN_ENABLED is
// false the engine returns ErrWebAuthnDisabled, which maps to a 403-class
// response — so the endpoints are safe to register unconditionally and act as a
// working example once WebAuthn is enabled.

func (s *authService) beginWebAuthnRegistration(ctx context.Context, userID string) (*goauth.WebAuthnRegistrationChallenge, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return nil, err
	}
	challenge, err := engine.BeginWebAuthnRegistration(ctx, userID)
	if err != nil {
		return nil, mapAuthEndpointError(err, "webauthn registration unavailable")
	}
	return challenge, nil
}

func (s *authService) finishWebAuthnRegistration(ctx context.Context, userID, ceremonyID string, responseJSON []byte) (*goauth.WebAuthnCredential, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return nil, err
	}
	cred, err := engine.FinishWebAuthnRegistration(ctx, userID, ceremonyID, responseJSON)
	if err != nil {
		return nil, mapAuthEndpointError(err, "webauthn registration failed")
	}
	return cred, nil
}

func (s *authService) listWebAuthnCredentials(ctx context.Context, userID string) ([]goauth.WebAuthnCredential, error) {
	engine, err := s.requireEngine()
	if err != nil {
		return nil, err
	}
	creds, err := engine.ListWebAuthnCredentials(ctx, userID)
	if err != nil {
		return nil, mapAuthEndpointError(err, "webauthn unavailable")
	}
	return creds, nil
}

func (s *authService) removeWebAuthnCredential(ctx context.Context, userID string, credentialID []byte) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}
	if err := engine.RemoveWebAuthnCredential(ctx, userID, credentialID); err != nil {
		return mapAuthEndpointError(err, "webauthn credential removal failed")
	}
	return nil
}
