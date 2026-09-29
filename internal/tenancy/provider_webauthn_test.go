package tenancy_test

import (
	"context"
	"sync"
	"testing"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/auth/authtest"
	"github.com/MrEthical07/superapi/internal/tenancy"
	"github.com/MrEthical07/superapi/internal/tenancy/tenancytest"
)

// memWebAuthn is an in-memory auth.WebAuthnCredentialRepository.
type memWebAuthn struct {
	mu    sync.Mutex
	creds map[string][]goauth.WebAuthnCredential
}

var _ auth.WebAuthnCredentialRepository = (*memWebAuthn)(nil)

func (m *memWebAuthn) ListByUser(_ context.Context, userID string) ([]goauth.WebAuthnCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]goauth.WebAuthnCredential(nil), m.creds[userID]...), nil
}

func (m *memWebAuthn) Add(_ context.Context, userID string, cred goauth.WebAuthnCredential) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.creds[userID] = append(m.creds[userID], cred)
	return nil
}

func (m *memWebAuthn) UpdateSignCount(context.Context, []byte, uint32) error { return nil }

func (m *memWebAuthn) Delete(_ context.Context, userID string, credentialID []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.creds[userID][:0]
	found := false
	for _, c := range m.creds[userID] {
		if string(c.CredentialID) == string(credentialID) {
			found = true
			continue
		}
		kept = append(kept, c)
	}
	m.creds[userID] = kept
	if !found {
		return auth.ErrWebAuthnCredentialNotFound
	}
	return nil
}

// goAuth detects capabilities by type assertion on the provider it is given,
// and a decorator that hid one would silently turn the feature off. The wrapper
// must satisfy everything the core provider does.
func TestProviderKeepsEveryOptionalInterface(t *testing.T) {
	e := tenancytest.NewEngine(t, auth.Features{})
	var provider any = e.Provider
	if _, ok := provider.(goauth.UserProvider); !ok {
		t.Error("the wrapper must be a goauth.UserProvider")
	}
	if _, ok := provider.(goauth.TenantAwareUserProvider); !ok {
		t.Error("the wrapper must add goauth.TenantAwareUserProvider")
	}
	if _, ok := provider.(goauth.WebAuthnCredentialProvider); !ok {
		t.Error("the wrapper must keep goauth.WebAuthnCredentialProvider")
	}
	var core any = auth.NewStoreUserProvider(authtest.NewUserRepository())
	if _, ok := core.(goauth.WebAuthnCredentialProvider); !ok {
		t.Fatal("the core provider stopped implementing goauth.WebAuthnCredentialProvider; update this test")
	}
}

// Engine.ListWebAuthnCredentials and Engine.RemoveWebAuthnCredential call the
// provider without resolving the user in the request tenant first (goAuth
// v0.6.0), so the wrapper scopes them. A user id from tenant A used under a
// tenant B request context is rejected, and nothing of tenant A's is touched.
func TestCrossTenantWebAuthnListAndRemoveAreRejected(t *testing.T) {
	t.Setenv("WEBAUTHN_ENABLED", "true")
	t.Setenv("WEBAUTHN_RP_ID", "localhost")
	t.Setenv("WEBAUTHN_RP_DISPLAY_NAME", "SuperAPI Test")
	t.Setenv("WEBAUTHN_RP_ORIGINS", "http://localhost")

	users := authtest.NewUserRepository()
	store := tenancytest.NewStore(users)
	repo := &memWebAuthn{creds: map[string][]goauth.WebAuthnCredential{}}
	var provider *tenancy.Provider
	engine, _ := authtest.NewEngineWith(t, users, auth.Features{}, func(base *auth.StoreUserProvider) goauth.UserProvider {
		provider = tenancy.NewProvider(base.WithWebAuthnRepository(repo), store)
		return provider
	}, tenancy.EnableMultiTenant)

	userA := createTestAccount(t, engine, "tenant-a", "gina@example.com")
	ctxA := tenancy.WithRequestTenant(context.Background(), "tenant-a")
	ctxB := tenancy.WithRequestTenant(context.Background(), "tenant-b")

	credID := []byte("credential-1")
	if err := provider.AddWebAuthnCredential(ctxA, userA, goauth.WebAuthnCredential{CredentialID: credID, PublicKey: []byte("pk")}); err != nil {
		t.Fatalf("add credential: %v", err)
	}

	if creds, err := engine.ListWebAuthnCredentials(ctxA, userA); err != nil || len(creds) != 1 {
		t.Fatalf("own tenant list = %d creds, err=%v", len(creds), err)
	}

	if creds, err := engine.ListWebAuthnCredentials(ctxB, userA); err == nil || len(creds) != 0 {
		t.Fatalf("list across tenants must be rejected and leak nothing: %d creds, err=%v", len(creds), err)
	}
	if err := engine.RemoveWebAuthnCredential(ctxB, userA, credID); err == nil {
		t.Fatal("remove across tenants must be rejected")
	}

	// Tenant A's credential survived the attempt and can be removed by its owner.
	if creds, _ := engine.ListWebAuthnCredentials(ctxA, userA); len(creds) != 1 {
		t.Fatalf("tenant A's credential was disturbed: %d left", len(creds))
	}
	if err := engine.RemoveWebAuthnCredential(ctxA, userA, credID); err != nil {
		t.Fatalf("owner remove: %v", err)
	}
	if creds, _ := engine.ListWebAuthnCredentials(ctxA, userA); len(creds) != 0 {
		t.Fatalf("credential not removed: %d left", len(creds))
	}
}
