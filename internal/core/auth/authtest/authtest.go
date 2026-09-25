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
		ID:           hex.EncodeToString(b[:]),
		TenantID:     tenantOrDefault(input.TenantID),
		Email:        strings.TrimSpace(input.Identifier),
		PasswordHash: input.PasswordHash,
		Role:         input.Role,
		Status:       input.Status,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.users[userID] = u
	return u, nil
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
	provider := auth.NewStoreUserProvider(repo).WithTenancy(tenancy)
	engine, closeFn, err := auth.NewGoAuthEngine(NewRedis(t), auth.ModeStrict, auth.TenancySettings{Enabled: tenancy}, provider)
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
