package auth

import (
	"context"

	goauth "github.com/MrEthical07/goAuth"
)

var _ goauth.WebAuthnCredentialProvider = (*StoreUserProvider)(nil)

// WithWebAuthnRepository attaches a WebAuthn credential repository so the
// provider can satisfy goAuth's WebAuthn ceremonies. Optional; only needed when
// WebAuthn is enabled.
func (p *StoreUserProvider) WithWebAuthnRepository(repo WebAuthnCredentialRepository) *StoreUserProvider {
	if p != nil {
		p.webauthnRepo = repo
	}
	return p
}

// --- WebAuthn credential capability (goauth.WebAuthnCredentialProvider) ---
//
// These delegate to the WebAuthn repository when one is wired. With no repository
// (the default, WebAuthn disabled) the store behaves as empty: listing returns
// no credentials, and mutations that require an existing credential report
// not-found. goAuth only invokes these while WebAuthn is enabled.

// GetWebAuthnCredentials returns every credential registered for the user.
func (p *StoreUserProvider) GetWebAuthnCredentials(ctx context.Context, userID string) ([]goauth.WebAuthnCredential, error) {
	if p == nil || p.webauthnRepo == nil {
		return nil, nil
	}
	return p.webauthnRepo.ListByUser(ctx, userID)
}

// AddWebAuthnCredential persists a newly registered credential.
func (p *StoreUserProvider) AddWebAuthnCredential(ctx context.Context, userID string, credential goauth.WebAuthnCredential) error {
	if p == nil || p.webauthnRepo == nil {
		return goauth.ErrUnauthorized
	}
	return p.webauthnRepo.Add(ctx, userID, credential)
}

// UpdateWebAuthnCredentialSignCount stores the authenticator's new signature
// counter after a successful assertion.
func (p *StoreUserProvider) UpdateWebAuthnCredentialSignCount(ctx context.Context, userID string, credentialID []byte, signCount uint32) error {
	if p == nil || p.webauthnRepo == nil {
		return goauth.ErrUnauthorized
	}
	return p.webauthnRepo.UpdateSignCount(ctx, credentialID, signCount)
}

// RemoveWebAuthnCredential deletes the credential with the given ID. Removing an
// unknown credential returns an error, per the provider contract.
func (p *StoreUserProvider) RemoveWebAuthnCredential(ctx context.Context, userID string, credentialID []byte) error {
	if p == nil || p.webauthnRepo == nil {
		return ErrWebAuthnCredentialNotFound
	}
	return p.webauthnRepo.Delete(ctx, userID, credentialID)
}
