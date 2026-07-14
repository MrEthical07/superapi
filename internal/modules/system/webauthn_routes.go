package system

import (
	"encoding/base64"
	"net/http"
	"time"

	goauth "github.com/MrEthical07/goAuth"
	apperr "github.com/MrEthical07/superapi/internal/core/errors"
	"github.com/MrEthical07/superapi/internal/core/httpx"
	"github.com/MrEthical07/superapi/internal/core/policy"
)

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

// registerWebAuthnRoutes mounts the WebAuthn ceremony endpoints. They are
// registered unconditionally and protected by auth; when WEBAUTHN_ENABLED is
// false the underlying goAuth calls return a "webauthn disabled" error, so the
// endpoints act as a working, self-documenting example that turns on with
// config + the optional migration. See docs/enabling-webauthn.md.
func (m *Module) registerWebAuthnRoutes(r httpx.Router) {
	// Policies are passed directly (no variadic spread) so static route
	// verification can analyze ordering and dependencies.
	r.Handle(http.MethodPost, "/api/v1/system/auth/webauthn/register/begin", httpx.Adapter(m.webAuthnRegisterBegin),
		policy.AuthRequired(m.runtime.AuthEngine(), m.runtime.AuthMode()),
	)
	r.Handle(http.MethodPost, "/api/v1/system/auth/webauthn/register/finish", httpx.Adapter(m.webAuthnRegisterFinish),
		policy.AuthRequired(m.runtime.AuthEngine(), m.runtime.AuthMode()),
	)
	r.Handle(http.MethodGet, "/api/v1/system/auth/webauthn/credentials", httpx.Adapter(m.webAuthnListCredentials),
		policy.AuthRequired(m.runtime.AuthEngine(), m.runtime.AuthMode()),
	)
	r.Handle(http.MethodPost, "/api/v1/system/auth/webauthn/credentials/remove", httpx.Adapter(m.webAuthnRemoveCredential),
		policy.AuthRequired(m.runtime.AuthEngine(), m.runtime.AuthMode()),
	)
}

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
		return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "ceremony_id is required")
	}
	if r.ResponseJSON == "" {
		return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "response_json is required")
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
		return apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "credential_id is required")
	}
	return nil
}

type webAuthnRemoveCredentialResponse struct {
	Removed bool `json:"removed"`
}

func (m *Module) webAuthnRegisterBegin(ctx *httpx.Context, _ httpx.NoBody) (webAuthnRegisterBeginResponse, error) {
	principal, ok := ctx.Auth()
	if !ok {
		return webAuthnRegisterBeginResponse{}, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required")
	}

	challenge, err := m.auth.beginWebAuthnRegistration(ctx.Context(), principal.UserID)
	if err != nil {
		return webAuthnRegisterBeginResponse{}, err
	}
	return webAuthnRegisterBeginResponse{
		CeremonyID:  challenge.CeremonyID,
		OptionsJSON: string(challenge.OptionsJSON),
	}, nil
}

func (m *Module) webAuthnRegisterFinish(ctx *httpx.Context, req webAuthnRegisterFinishRequest) (webAuthnCredentialView, error) {
	principal, ok := ctx.Auth()
	if !ok {
		return webAuthnCredentialView{}, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required")
	}

	cred, err := m.auth.finishWebAuthnRegistration(ctx.Context(), principal.UserID, req.CeremonyID, []byte(req.ResponseJSON))
	if err != nil {
		return webAuthnCredentialView{}, err
	}
	return webAuthnCredentialToView(*cred), nil
}

func (m *Module) webAuthnListCredentials(ctx *httpx.Context, _ httpx.NoBody) (webAuthnCredentialsResponse, error) {
	principal, ok := ctx.Auth()
	if !ok {
		return webAuthnCredentialsResponse{}, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required")
	}

	creds, err := m.auth.listWebAuthnCredentials(ctx.Context(), principal.UserID)
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
	principal, ok := ctx.Auth()
	if !ok {
		return webAuthnRemoveCredentialResponse{}, apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "authentication required")
	}

	credentialID, err := base64.RawURLEncoding.DecodeString(req.CredentialID)
	if err != nil {
		return webAuthnRemoveCredentialResponse{}, apperr.New(apperr.CodeBadRequest, http.StatusBadRequest, "credential_id must be base64url-encoded")
	}

	if err := m.auth.removeWebAuthnCredential(ctx.Context(), principal.UserID, credentialID); err != nil {
		return webAuthnRemoveCredentialResponse{}, err
	}
	return webAuthnRemoveCredentialResponse{Removed: true}, nil
}
