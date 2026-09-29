package tenancy

import (
	"context"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

// userRepository decorates the core auth.UserRepository that modules receive as
// Dependencies.AuthUsers: with a request tenant attached, the plain lookups are
// restricted to that tenant (the scope goAuth applies to its own lookups);
// without one they fall through to the core repository.
type userRepository struct {
	auth.UserRepository
	store UserStore
}

// WrapUserRepository returns base with tenant-scoped lookups.
func WrapUserRepository(base auth.UserRepository, store UserStore) auth.UserRepository {
	return &userRepository{UserRepository: base, store: store}
}

func (r *userRepository) GetByIdentifier(ctx context.Context, identifier string) (auth.StoredUser, error) {
	if tenantID, ok := RequestTenantFromContext(ctx); ok {
		u, err := r.store.GetByIdentifierInTenant(ctx, tenantID, identifier)
		return u.StoredUser, err
	}
	return r.UserRepository.GetByIdentifier(ctx, identifier)
}

func (r *userRepository) GetByID(ctx context.Context, userID string) (auth.StoredUser, error) {
	if tenantID, ok := RequestTenantFromContext(ctx); ok {
		u, err := r.store.GetByIDInTenant(ctx, tenantID, userID)
		return u.StoredUser, err
	}
	return r.UserRepository.GetByID(ctx, userID)
}
