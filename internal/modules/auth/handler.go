package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/golang-jwt/jwt/v5"

	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/httpx"
)

// requestContext enriches the request context with the client IP and
// User-Agent so goAuth's abuse limiters, device binding, and audit trail see
// them. The tenant (when tenancy is on) is already attached by the tenant
// middleware.
func requestContext(ctx *httpx.Context) context.Context {
	c := ctx.Context()
	if ip, ok := ctx.ClientIP(); ok && ip != "" {
		c = goauth.WithClientIP(c, ip)
	}
	if ua := ctx.Header("User-Agent"); ua != "" {
		c = goauth.WithUserAgent(c, ua)
	}
	return c
}

func principalID(ctx *httpx.Context) (string, error) {
	principal, ok := ctx.Auth()
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		return "", apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required")
	}
	return principal.UserID, nil
}

func accepted() httpx.Result[acceptedResponse] {
	return httpx.Result[acceptedResponse]{Status: http.StatusAccepted, Data: acceptedResponse{Accepted: true}}
}

// --- login / MFA / refresh / logout / whoami ---

func (m *Module) login(ctx *httpx.Context, req loginRequest) (tokenResponse, error) {
	outcome, err := m.svc.login(requestContext(ctx), req.Identifier, req.Password, req.RememberMe)
	if err != nil {
		return tokenResponse{}, err
	}
	return buildLoginResponse(outcome)
}

func (m *Module) confirmMFA(ctx *httpx.Context, req mfaConfirmRequest) (tokenResponse, error) {
	outcome, err := m.svc.confirmMFA(requestContext(ctx), req.Challenge, req.Code, req.Type)
	if err != nil {
		return tokenResponse{}, err
	}
	return buildLoginResponse(outcome)
}

func (m *Module) refresh(ctx *httpx.Context, req refreshRequest) (tokenResponse, error) {
	access, next, err := m.svc.refresh(requestContext(ctx), req.RefreshToken)
	if err != nil {
		return tokenResponse{}, err
	}
	return buildTokenResponse(access, next)
}

func (m *Module) logout(ctx *httpx.Context, req logoutRequest) (logoutResponse, error) {
	token := strings.TrimSpace(req.AccessToken)
	if token == "" {
		token = bearerToken(ctx.Header("Authorization"))
	}
	if token == "" {
		return logoutResponse{}, badRequest("access token is required")
	}
	if err := m.svc.logout(requestContext(ctx), token); err != nil {
		return logoutResponse{}, err
	}
	return logoutResponse{LoggedOut: true}, nil
}

func (m *Module) logoutAll(ctx *httpx.Context, _ httpx.NoBody) (logoutResponse, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return logoutResponse{}, err
	}
	if err := m.svc.logoutAll(requestContext(ctx), userID); err != nil {
		return logoutResponse{}, err
	}
	return logoutResponse{LoggedOut: true}, nil
}

func (m *Module) whoami(ctx *httpx.Context, _ httpx.NoBody) (whoamiResponse, error) {
	principal, ok := ctx.Auth()
	if !ok {
		return whoamiResponse{}, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required")
	}
	return whoamiResponse{
		UserID:      principal.UserID,
		TenantID:    principal.TenantID,
		Role:        principal.Role,
		Permissions: append([]string(nil), principal.Permissions...),
	}, nil
}

func (m *Module) sessions(ctx *httpx.Context, _ httpx.NoBody) (sessionsResponse, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return sessionsResponse{}, err
	}
	list, err := m.svc.listSessions(requestContext(ctx), userID)
	if err != nil {
		return sessionsResponse{}, err
	}
	views := make([]sessionView, 0, len(list))
	for _, s := range list {
		views = append(views, sessionView{
			SessionID:  s.SessionID,
			CreatedUTC: unixToRFC3339(s.CreatedAt),
			ExpiresUTC: unixToRFC3339(s.ExpiresAt),
		})
	}
	return sessionsResponse{Sessions: views}, nil
}

func (m *Module) changePassword(ctx *httpx.Context, req passwordChangeRequest) (changedResponse, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return changedResponse{}, err
	}
	if err := m.svc.changePassword(requestContext(ctx), userID, req.CurrentPassword, req.NewPassword); err != nil {
		return changedResponse{}, err
	}
	return changedResponse{Changed: true}, nil
}

// --- registration ---

// registerAccount always answers 202 {"accepted":true} unless auto-login issued
// tokens for a new account (201). See service.register for the enumeration
// trade-off of auto-login.
func (m *Module) registerAccount(ctx *httpx.Context, req registerRequest) (httpx.Result[any], error) {
	outcome, err := m.svc.register(requestContext(ctx), req.Identifier, req.Password, req.RememberMe)
	if err != nil {
		return httpx.Result[any]{}, err
	}
	if outcome.AccessToken == "" {
		return httpx.Result[any]{Status: http.StatusAccepted, Data: acceptedResponse{Accepted: true}}, nil
	}
	tokens, err := buildTokenResponse(outcome.AccessToken, outcome.RefreshToken)
	if err != nil {
		return httpx.Result[any]{}, err
	}
	return httpx.Result[any]{Status: http.StatusCreated, Data: tokens}, nil
}

// --- password reset ---

func (m *Module) passwordResetRequest(ctx *httpx.Context, req identifierRequest) (httpx.Result[acceptedResponse], error) {
	if err := m.svc.requestPasswordReset(requestContext(ctx), req.Identifier); err != nil {
		return httpx.Result[acceptedResponse]{}, err
	}
	return accepted(), nil
}

func (m *Module) passwordResetConfirm(ctx *httpx.Context, req passwordResetConfirmRequest) (changedResponse, error) {
	if err := m.svc.confirmPasswordReset(requestContext(ctx), req.Challenge, req.NewPassword, req.MFAType, req.MFACode); err != nil {
		return changedResponse{}, err
	}
	return changedResponse{Changed: true}, nil
}

// --- email verification ---

func (m *Module) emailVerifyRequest(ctx *httpx.Context, req identifierRequest) (httpx.Result[acceptedResponse], error) {
	if err := m.svc.requestEmailVerification(requestContext(ctx), req.Identifier); err != nil {
		return httpx.Result[acceptedResponse]{}, err
	}
	return accepted(), nil
}

func (m *Module) emailVerifyConfirm(ctx *httpx.Context, req emailVerifyConfirmRequest) (confirmedResponse, error) {
	if err := m.svc.confirmEmailVerification(requestContext(ctx), req.Challenge, req.VerificationID, req.Code); err != nil {
		return confirmedResponse{}, err
	}
	return confirmedResponse{Confirmed: true}, nil
}

// --- TOTP / backup codes ---

func (m *Module) totpSetup(ctx *httpx.Context, _ httpx.NoBody) (totpSetupResponse, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return totpSetupResponse{}, err
	}
	setup, err := m.svc.setupTOTP(requestContext(ctx), userID)
	if err != nil {
		return totpSetupResponse{}, err
	}
	return totpSetupResponse{SecretBase32: setup.SecretBase32, OTPAuthURI: setup.OTPAuthURI}, nil
}

func (m *Module) totpConfirm(ctx *httpx.Context, req totpCodeRequest) (totpConfirmResponse, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return totpConfirmResponse{}, err
	}
	codes, err := m.svc.confirmTOTP(requestContext(ctx), userID, req.Code)
	if err != nil {
		return totpConfirmResponse{}, err
	}
	return totpConfirmResponse{Enabled: true, BackupCodes: codes}, nil
}

func (m *Module) totpDisable(ctx *httpx.Context, req totpCodeRequest) (totpDisableResponse, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return totpDisableResponse{}, err
	}
	if err := m.svc.disableTOTP(requestContext(ctx), userID, req.Code); err != nil {
		return totpDisableResponse{}, err
	}
	return totpDisableResponse{Disabled: true}, nil
}

func (m *Module) backupCodesRegenerate(ctx *httpx.Context, req totpCodeRequest) (backupCodesResponse, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return backupCodesResponse{}, err
	}
	codes, err := m.svc.regenerateBackupCodes(requestContext(ctx), userID, req.Code)
	if err != nil {
		return backupCodesResponse{}, err
	}
	return backupCodesResponse{BackupCodes: codes}, nil
}

// --- helpers ---

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(header) >= len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}

func unixToRFC3339(sec int64) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

// buildLoginResponse turns a login/MFA-confirm outcome into the token response:
// the challenge shape (no tokens) when MFA is required, otherwise tokens.
func buildLoginResponse(outcome loginOutcome) (tokenResponse, error) {
	if outcome.MFARequired {
		return tokenResponse{
			MFARequired:  true,
			MFAChallenge: outcome.MFASession,
			MFAType:      outcome.MFAType,
			MFATypes:     outcome.MFATypes,
		}, nil
	}
	return buildTokenResponse(outcome.AccessToken, outcome.RefreshToken)
}

func buildTokenResponse(accessToken, refreshToken string) (tokenResponse, error) {
	expiresUnix, err := parseJWTExpiryUnix(accessToken)
	if err != nil {
		return tokenResponse{}, apperr.WithCause(apperr.New(apperr.CodeInternal, http.StatusInternalServerError, "invalid access token payload"), err)
	}
	return tokenResponse{
		AccessToken:       accessToken,
		RefreshToken:      refreshToken,
		AccessExpiresUTC:  time.Unix(expiresUnix, 0).UTC().Format(time.RFC3339),
		AccessExpiresUnix: expiresUnix,
	}, nil
}

// parseJWTExpiryUnix reads exp from a token goAuth just issued (so it is not
// re-verified here).
func parseJWTExpiryUnix(accessToken string) (int64, error) {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(accessToken, claims); err != nil {
		return 0, err
	}
	expRaw, ok := claims["exp"]
	if !ok {
		return 0, errors.New("jwt exp claim missing")
	}
	switch exp := expRaw.(type) {
	case float64:
		return int64(exp), nil
	case int64:
		return exp, nil
	case int:
		return int64(exp), nil
	case json.Number:
		return exp.Int64()
	default:
		return 0, errors.New("jwt exp claim has invalid type")
	}
}

// --- WebAuthn ceremonies ---

type webAuthnRegisterBeginResponse struct {
	CeremonyID  string `json:"ceremony_id"`
	OptionsJSON string `json:"options_json"`
}

type webAuthnRegisterFinishRequest struct {
	CeremonyID   string `json:"ceremony_id"`
	ResponseJSON string `json:"response_json"`
}

// Validate ensures the ceremony id and authenticator response are present.
func (r webAuthnRegisterFinishRequest) Validate() error {
	if r.CeremonyID == "" {
		return badRequest("ceremony_id is required")
	}
	if r.ResponseJSON == "" {
		return badRequest("response_json is required")
	}
	return nil
}

type webAuthnCredentialView struct {
	CredentialID string `json:"credential_id"`
	SignCount    uint32 `json:"sign_count"`
	CreatedUTC   string `json:"created_utc,omitempty"`
	LastUsedUTC  string `json:"last_used_utc,omitempty"`
}

type webAuthnCredentialsResponse struct {
	Credentials []webAuthnCredentialView `json:"credentials"`
}

type webAuthnRemoveCredentialRequest struct {
	CredentialID string `json:"credential_id"`
}

// Validate ensures the credential id is present.
func (r webAuthnRemoveCredentialRequest) Validate() error {
	if r.CredentialID == "" {
		return badRequest("credential_id is required")
	}
	return nil
}

type webAuthnRemoveCredentialResponse struct {
	Removed bool `json:"removed"`
}

func webAuthnCredentialToView(c goauth.WebAuthnCredential) webAuthnCredentialView {
	view := webAuthnCredentialView{
		CredentialID: base64.RawURLEncoding.EncodeToString(c.CredentialID),
		SignCount:    c.SignCount,
	}
	if !c.CreatedAt.IsZero() {
		view.CreatedUTC = c.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !c.LastUsedAt.IsZero() {
		view.LastUsedUTC = c.LastUsedAt.UTC().Format(time.RFC3339)
	}
	return view
}

func (m *Module) webAuthnRegisterBegin(ctx *httpx.Context, _ httpx.NoBody) (webAuthnRegisterBeginResponse, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return webAuthnRegisterBeginResponse{}, err
	}
	challenge, err := m.svc.beginWebAuthnRegistration(requestContext(ctx), userID)
	if err != nil {
		return webAuthnRegisterBeginResponse{}, err
	}
	return webAuthnRegisterBeginResponse{CeremonyID: challenge.CeremonyID, OptionsJSON: string(challenge.OptionsJSON)}, nil
}

func (m *Module) webAuthnRegisterFinish(ctx *httpx.Context, req webAuthnRegisterFinishRequest) (webAuthnCredentialView, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return webAuthnCredentialView{}, err
	}
	cred, err := m.svc.finishWebAuthnRegistration(requestContext(ctx), userID, req.CeremonyID, []byte(req.ResponseJSON))
	if err != nil {
		return webAuthnCredentialView{}, err
	}
	return webAuthnCredentialToView(*cred), nil
}

func (m *Module) webAuthnListCredentials(ctx *httpx.Context, _ httpx.NoBody) (webAuthnCredentialsResponse, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return webAuthnCredentialsResponse{}, err
	}
	creds, err := m.svc.listWebAuthnCredentials(requestContext(ctx), userID)
	if err != nil {
		return webAuthnCredentialsResponse{}, err
	}
	views := make([]webAuthnCredentialView, 0, len(creds))
	for _, c := range creds {
		views = append(views, webAuthnCredentialToView(c))
	}
	return webAuthnCredentialsResponse{Credentials: views}, nil
}

func (m *Module) webAuthnRemoveCredential(ctx *httpx.Context, req webAuthnRemoveCredentialRequest) (webAuthnRemoveCredentialResponse, error) {
	userID, err := principalID(ctx)
	if err != nil {
		return webAuthnRemoveCredentialResponse{}, err
	}
	credentialID, err := base64.RawURLEncoding.DecodeString(req.CredentialID)
	if err != nil {
		return webAuthnRemoveCredentialResponse{}, badRequest("credential_id must be base64url-encoded")
	}
	if err := m.svc.removeWebAuthnCredential(requestContext(ctx), userID, credentialID); err != nil {
		return webAuthnRemoveCredentialResponse{}, err
	}
	return webAuthnRemoveCredentialResponse{Removed: true}, nil
}
