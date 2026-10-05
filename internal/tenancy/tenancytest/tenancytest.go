// Package tenancytest gives tests an in-memory tenancy.UserStore and a
// multi-tenant goAuth engine built the way the feature builds it in the app
// (the provider decorator and the goAuth config mutator), without Postgres.
package tenancytest

import (
	"context"
	"strings"
	"sync"
	"testing"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/auth/authtest"
	"github.com/MrEthical07/superapi/internal/tenancy"
)

var _ tenancy.UserStore = (*Store)(nil)

// Store is an in-memory tenancy.UserStore layered over an authtest
// UserRepository, so the core provider and the tenant wrapper see the same
// users. It mirrors the SQL contract: the *InTenant lookups only match rows of
// that tenant and treat an empty tenant as not found.
type Store struct {
	users *authtest.UserRepository

	mu       sync.Mutex
	tenantOf map[string]string
}

// NewStore returns a store over users.
func NewStore(users *authtest.UserRepository) *Store {
	return &Store{users: users, tenantOf: map[string]string{}}
}

func (s *Store) tenant(userID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tenantOf[userID]; ok {
		return t
	}
	return tenancy.DefaultTenantID
}

func (s *Store) wrap(u auth.StoredUser) tenancy.TenantUser {
	return tenancy.TenantUser{StoredUser: u, TenantID: s.tenant(u.ID)}
}

func (s *Store) GetByIdentifier(ctx context.Context, identifier string) (tenancy.TenantUser, error) {
	u, err := s.users.GetByIdentifier(ctx, identifier)
	if err != nil {
		return tenancy.TenantUser{}, err
	}
	return s.wrap(u), nil
}

func (s *Store) GetByID(ctx context.Context, userID string) (tenancy.TenantUser, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return tenancy.TenantUser{}, err
	}
	return s.wrap(u), nil
}

func (s *Store) GetByIdentifierInTenant(ctx context.Context, tenantID, identifier string) (tenancy.TenantUser, error) {
	if strings.TrimSpace(tenantID) == "" {
		return tenancy.TenantUser{}, auth.ErrAuthUserNotFound
	}
	u, err := s.GetByIdentifier(ctx, identifier)
	if err != nil || u.TenantID != tenantID {
		return tenancy.TenantUser{}, auth.ErrAuthUserNotFound
	}
	return u, nil
}

func (s *Store) GetByIDInTenant(ctx context.Context, tenantID, userID string) (tenancy.TenantUser, error) {
	if strings.TrimSpace(tenantID) == "" {
		return tenancy.TenantUser{}, auth.ErrAuthUserNotFound
	}
	u, err := s.GetByID(ctx, userID)
	if err != nil || u.TenantID != tenantID {
		return tenancy.TenantUser{}, auth.ErrAuthUserNotFound
	}
	return u, nil
}

func (s *Store) CreateInTenant(ctx context.Context, tenantID string, input auth.CreateStoredUserInput) (tenancy.TenantUser, error) {
	if strings.TrimSpace(tenantID) == "" {
		tenantID = tenancy.DefaultTenantID
	}
	u, err := s.users.Create(ctx, input)
	if err != nil {
		return tenancy.TenantUser{}, err
	}
	s.mu.Lock()
	s.tenantOf[u.ID] = tenantID
	s.mu.Unlock()
	return tenancy.TenantUser{StoredUser: u, TenantID: tenantID}, nil
}

func (s *Store) TenantOf(ctx context.Context, userID string) (string, error) {
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		return "", err
	}
	return s.tenant(userID), nil
}

func (s *Store) UpdatePasswordHashInTenant(ctx context.Context, tenantID, userID, newHash string) error {
	if _, err := s.GetByIDInTenant(ctx, tenantID, userID); err != nil {
		return err
	}
	return s.users.UpdatePasswordHash(ctx, userID, newHash)
}

// Engine bundles what a multi-tenant test needs.
type Engine struct {
	*goauth.Engine
	Provider *tenancy.Provider
	Users    *authtest.UserRepository
	Store    *Store
}

// NewEngine builds a goAuth engine in multi-tenant mode over in-memory
// repositories, exactly as the tenancy feature wires it: the core provider is
// wrapped by tenancy.Provider and goAuth's config is mutated by
// tenancy.EnableMultiTenant.
func NewEngine(t testing.TB, features auth.Features) *Engine {
	t.Helper()
	return NewEngineWith(t, features, nil)
}

// NewEngineWith is NewEngine with a decorator over the tenancy provider, for
// tests that observe the calls goAuth makes (the way app.Hooks.UserProvider
// decorators stack). wrap may be nil; Engine.Provider is always the tenancy
// provider itself.
func NewEngineWith(t testing.TB, features auth.Features, wrap func(*tenancy.Provider) goauth.UserProvider) *Engine {
	t.Helper()
	users := authtest.NewUserRepository()
	store := NewStore(users)
	var provider *tenancy.Provider
	engine, _ := authtest.NewEngineWith(t, users, features, func(base *auth.StoreUserProvider) goauth.UserProvider {
		provider = tenancy.NewProvider(base, store)
		if wrap != nil {
			return wrap(provider)
		}
		return provider
	}, tenancy.EnableMultiTenant)
	return &Engine{Engine: engine, Provider: provider, Users: users, Store: store}
}
