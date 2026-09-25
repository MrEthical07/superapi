// Package authtest provides in-memory auth repositories and an engine builder
// for tests. The repositories mirror the SQL contract of the relational
// implementations (tenant scoping, not-found semantics) so tests exercise the
// real auth.StoreUserProvider and goAuth engine without Postgres.
package authtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"testing"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

var (
	_ auth.UserRepository = (*UserRepository)(nil)
	_ auth.MFARepository  = (*MFARepository)(nil)
)

// UserRepository is an in-memory UserRepository that mirrors the SQL
// contract: *InTenant lookups only match rows in that tenant and treat an
// empty tenant as not found.
type UserRepository struct {
	mu    sync.Mutex
	users map[string]auth.StoredUser
}

func NewUserRepository() *UserRepository {
	return &UserRepository{users: make(map[string]auth.StoredUser)}
}

func (r *UserRepository) GetByIdentifier(_ context.Context, identifier string) (auth.StoredUser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.Email == strings.TrimSpace(identifier) {
			return u, nil
		}
	}
	return auth.StoredUser{}, auth.ErrAuthUserNotFound
}

func (r *UserRepository) GetByID(_ context.Context, userID string) (auth.StoredUser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[userID]; ok {
		return u, nil
	}
	return auth.StoredUser{}, auth.ErrAuthUserNotFound
}

func (r *UserRepository) GetByIdentifierInTenant(_ context.Context, tenantID, identifier string) (auth.StoredUser, error) {
	if tenantID == "" {
		return auth.StoredUser{}, auth.ErrAuthUserNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.TenantID == tenantID && u.Email == strings.TrimSpace(identifier) {
			return u, nil
		}
	}
	return auth.StoredUser{}, auth.ErrAuthUserNotFound
}

func (r *UserRepository) GetByIDInTenant(_ context.Context, tenantID, userID string) (auth.StoredUser, error) {
	if tenantID == "" {
		return auth.StoredUser{}, auth.ErrAuthUserNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[userID]; ok && u.TenantID == tenantID {
		return u, nil
	}
	return auth.StoredUser{}, auth.ErrAuthUserNotFound
}

func (r *UserRepository) UpdatePasswordHash(_ context.Context, userID, newHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[userID]
	if !ok {
		return auth.ErrAuthUserNotFound
	}
	u.PasswordHash = newHash
	r.users[userID] = u
	return nil
}

func (r *UserRepository) Create(_ context.Context, input auth.CreateStoredUserInput) (auth.StoredUser, error) {
	var b [16]byte
	_, _ = rand.Read(b[:])
	u := auth.StoredUser{
		ID:             hex.EncodeToString(b[:]),
		TenantID:       tenantOrDefault(input.TenantID),
		Email:          strings.TrimSpace(input.Identifier),
		PasswordHash:   input.PasswordHash,
		Role:           input.Role,
		Status:         input.Status,
		AccountVersion: 1,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.users {
		// Global identifier uniqueness, like users_email_unique_idx.
		if existing.Email == u.Email {
			return auth.StoredUser{}, auth.ErrAuthUserExists
		}
	}
	r.users[u.ID] = u
	return u, nil
}

func (r *UserRepository) UpdateStatus(_ context.Context, userID string, status string) (auth.StoredUser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[userID]
	if !ok {
		return auth.StoredUser{}, auth.ErrAuthUserNotFound
	}
	u.Status = status
	u.AccountVersion++
	r.users[userID] = u
	return u, nil
}

// MFARepository is an in-memory auth.MFARepository sharing user state with a
// UserRepository (so totp_enabled and account_version stay consistent, as the
// SQL statements keep them).
type MFARepository struct {
	users *UserRepository
	totp  map[string]auth.TOTPState
	codes map[string]map[[32]byte]bool // hash -> used
}

// NewMFARepository returns an MFA repository over repo, which must be a
// *UserRepository from this package.
func NewMFARepository(repo auth.UserRepository) *MFARepository {
	users, _ := repo.(*UserRepository)
	return &MFARepository{users: users, totp: map[string]auth.TOTPState{}, codes: map[string]map[[32]byte]bool{}}
}

// inScope reports whether userID exists (in tenantID when non-empty). Callers
// hold the users lock.
func (m *MFARepository) inScope(tenantID, userID string) (auth.StoredUser, bool) {
	u, ok := m.users.users[userID]
	if !ok || (tenantID != "" && u.TenantID != tenantID) {
		return auth.StoredUser{}, false
	}
	return u, true
}

func (m *MFARepository) GetTOTP(_ context.Context, tenantID, userID string) (auth.TOTPState, bool, error) {
	m.users.mu.Lock()
	defer m.users.mu.Unlock()
	u, ok := m.inScope(tenantID, userID)
	if !ok {
		return auth.TOTPState{}, false, nil
	}
	st, ok := m.totp[userID]
	if !ok {
		return auth.TOTPState{}, false, nil
	}
	st.Enabled = u.TOTPEnabled
	return st, true, nil
}

func (m *MFARepository) setEnabled(u auth.StoredUser, enabled bool) {
	if u.TOTPEnabled != enabled {
		u.AccountVersion++
	}
	u.TOTPEnabled = enabled
	m.users.users[u.ID] = u
}

func (m *MFARepository) UpsertTOTPSecret(_ context.Context, tenantID, userID string, ciphertext []byte) error {
	m.users.mu.Lock()
	defer m.users.mu.Unlock()
	u, ok := m.inScope(tenantID, userID)
	if !ok {
		return auth.ErrAuthUserNotFound
	}
	st := m.totp[userID]
	st.SecretCiphertext = append([]byte(nil), ciphertext...)
	m.totp[userID] = st
	m.setEnabled(u, st.Verified)
	return nil
}

func (m *MFARepository) MarkTOTPVerified(_ context.Context, tenantID, userID string) error {
	m.users.mu.Lock()
	defer m.users.mu.Unlock()
	if _, ok := m.inScope(tenantID, userID); !ok {
		return auth.ErrAuthUserNotFound
	}
	st, ok := m.totp[userID]
	if !ok {
		return auth.ErrAuthUserNotFound
	}
	st.Verified = true
	m.totp[userID] = st
	return nil
}

func (m *MFARepository) AdvanceTOTPCounter(_ context.Context, tenantID, userID string, counter int64) error {
	m.users.mu.Lock()
	defer m.users.mu.Unlock()
	st, ok := m.totp[userID]
	if _, in := m.inScope(tenantID, userID); !in || !ok || st.LastUsedCounter >= counter {
		return auth.ErrTOTPCounterNotAdvanced
	}
	st.LastUsedCounter = counter
	m.totp[userID] = st
	return nil
}

func (m *MFARepository) DisableTOTP(_ context.Context, tenantID, userID string) error {
	m.users.mu.Lock()
	defer m.users.mu.Unlock()
	u, ok := m.inScope(tenantID, userID)
	if !ok {
		return auth.ErrAuthUserNotFound
	}
	delete(m.totp, userID)
	delete(m.codes, userID)
	m.setEnabled(u, false)
	return nil
}

func (m *MFARepository) ListUnusedBackupCodes(_ context.Context, tenantID, userID string) ([][32]byte, error) {
	m.users.mu.Lock()
	defer m.users.mu.Unlock()
	if _, ok := m.inScope(tenantID, userID); !ok {
		return nil, nil
	}
	var out [][32]byte
	for h, used := range m.codes[userID] {
		if !used {
			out = append(out, h)
		}
	}
	return out, nil
}

func (m *MFARepository) ReplaceBackupCodes(_ context.Context, tenantID, userID string, hashes [][32]byte) error {
	m.users.mu.Lock()
	defer m.users.mu.Unlock()
	if _, ok := m.inScope(tenantID, userID); !ok {
		return auth.ErrAuthUserNotFound
	}
	next := make(map[[32]byte]bool, len(hashes))
	for _, h := range hashes {
		next[h] = false
	}
	m.codes[userID] = next
	return nil
}

func (m *MFARepository) ConsumeBackupCode(_ context.Context, tenantID, userID string, hash [32]byte) (bool, error) {
	m.users.mu.Lock()
	defer m.users.mu.Unlock()
	if _, ok := m.inScope(tenantID, userID); !ok {
		return false, nil
	}
	used, ok := m.codes[userID][hash]
	if !ok || used {
		return false, nil
	}
	m.codes[userID][hash] = true
	return true, nil
}

// NewRedis returns a miniredis-backed client closed at test end.
func NewRedis(t testing.TB) redis.UniversalClient {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// NewEngine builds a goAuth engine through the project config over the
// in-memory repository.
func NewEngine(t testing.TB, tenancy bool, repo auth.UserRepository) (*goauth.Engine, *auth.StoreUserProvider) {
	t.Helper()
	return NewEngineWithFeatures(t, tenancy, repo, auth.Features{})
}

// NewEngineWithFeatures is NewEngine with explicit auth feature flags. When
// features.TOTP is set, an in-memory MFA repository and a random-key cipher are
// wired into the provider.
func NewEngineWithFeatures(t testing.TB, tenancy bool, repo auth.UserRepository, features auth.Features) (*goauth.Engine, *auth.StoreUserProvider) {
	t.Helper()
	provider := auth.NewStoreUserProvider(repo).WithTenancy(tenancy)
	if features.TOTP {
		var key [32]byte
		_, _ = rand.Read(key[:])
		cipher, err := auth.NewAESGCMCipher(key[:])
		if err != nil {
			t.Fatalf("cipher: %v", err)
		}
		provider = provider.WithMFA(NewMFARepository(repo), cipher)
	}
	engine, closeFn, err := auth.NewGoAuthEngine(NewRedis(t), auth.ModeStrict, auth.TenancySettings{Enabled: tenancy}, features, provider)
	if err != nil {
		t.Fatalf("build engine (tenancy=%v): %v", tenancy, err)
	}
	t.Cleanup(closeFn)
	return engine, provider
}

func tenantOrDefault(tenantID string) string {
	if trimmed := strings.TrimSpace(tenantID); trimmed != "" {
		return trimmed
	}
	return auth.DefaultTenantID
}
