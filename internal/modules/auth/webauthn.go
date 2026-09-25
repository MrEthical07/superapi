package auth

import (
	"context"
	"encoding/base64"
	"time"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/httpx"
)

// --- WebAuthn ceremonies (return an error while WEBAUTHN_ENABLED=false) ---

func (s *service) beginWebAuthnRegistration(ctx context.Context, userID string) (*goauth.WebAuthnRegistrationChallenge, error) {
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

func (s *service) finishWebAuthnRegistration(ctx context.Context, userID, ceremonyID string, responseJSON []byte) (*goauth.WebAuthnCredential, error) {
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

func (s *service) listWebAuthnCredentials(ctx context.Context, userID string) ([]goauth.WebAuthnCredential, error) {
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

func (s *service) removeWebAuthnCredential(ctx context.Context, userID string, credentialID []byte) error {
	engine, err := s.requireEngine()
	if err != nil {
		return err
	}
	if err := engine.RemoveWebAuthnCredential(ctx, userID, credentialID); err != nil {
		return mapAuthEndpointError(err, "webauthn credential removal failed")
	}
	return nil
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
