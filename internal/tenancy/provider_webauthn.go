package tenancy

import (
	"context"

	goauth "github.com/MrEthical07/goAuth"
)

// The core provider satisfies goauth.WebAuthnCredentialProvider; the wrapper
// must too, or goAuth's Build fails (WebAuthn enabled) or WebAuthn is lost.
var _ goauth.WebAuthnCredentialProvider = (*Provider)(nil)

// GetWebAuthnCredentials lists the credentials of a user of the request tenant.
// Engine.ListWebAuthnCredentials calls it without resolving the user first, so
// the tenant scope is enforced here.
func (p *Provider) GetWebAuthnCredentials(ctx context.Context, userID string) ([]goauth.WebAuthnCredential, error) {
	inScope, err := p.userInScope(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !inScope {
		return nil, goauth.ErrUserNotFound
	}
	return p.StoreUserProvider.GetWebAuthnCredentials(ctx, userID)
}

// RemoveWebAuthnCredential deletes a credential of a user of the request
// tenant; Engine.RemoveWebAuthnCredential reaches it without resolving the
// user first.
func (p *Provider) RemoveWebAuthnCredential(ctx context.Context, userID string, credentialID []byte) error {
	inScope, err := p.userInScope(ctx, userID)
	if err != nil {
		return err
	}
	if !inScope {
		return goauth.ErrUserNotFound
	}
	return p.StoreUserProvider.RemoveWebAuthnCredential(ctx, userID, credentialID)
}
