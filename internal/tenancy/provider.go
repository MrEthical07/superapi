package tenancy

import (
	"context"
	"errors"
	"fmt"
	"time"

	goauth "github.com/MrEthical07/goAuth"

	"github.com/MrEthical07/superapi/internal/core/auth"
)

// Provider decorates the core auth.StoreUserProvider for goAuth's multi-tenant
// mode. Embedding the core provider promotes every method and every optional
// goAuth interface it implements (WebAuthn, TOTP, backup codes); Provider adds
// goauth.TenantAwareUserProvider and takes over the methods whose result or
// scope depends on the tenant:
//
//   - tenant-blind lookups and CreateUser / UpdateAccountStatus, so the
//     returned goauth.UserRecord carries the stored tenant id and new users are
//     created in the request tenant;
//   - the tenant-scoped lookups goAuth uses in multi-tenant mode;
//   - the id-keyed calls goAuth can reach without first resolving the user in
//     the request tenant (see docs/multi-tenancy.md, "What goAuth enforces"):
//     ConsumeBackupCode here and the WebAuthn list/remove calls in
//     provider_webauthn.go. They are restricted to users of the request
//     tenant, so a user id from another tenant is not found.
//
// goAuth detects capabilities by type assertion on the provider it is given, so
// every interface the core provider satisfies is asserted below: a wrapper that
// dropped one would silently disable that feature.
type Provider struct {
	*auth.StoreUserProvider
	store UserStore
}

var (
	_ goauth.UserProvider            = (*Provider)(nil)
	_ goauth.TenantAwareUserProvider = (*Provider)(nil)
)

const defaultLookupTimeout = 3 * time.Second

// NewProvider wraps base. store supplies the tenant-aware persistence.
func NewProvider(base *auth.StoreUserProvider, store UserStore) *Provider {
	return &Provider{StoreUserProvider: base, store: store}
}

func lookupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultLookupTimeout)
}

// boundedContext applies the default lookup timeout to a caller context,
// keeping any shorter deadline the caller already set.
func boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, defaultLookupTimeout)
}

// record maps a stored user to goAuth's record with its tenant.
func record(u TenantUser) goauth.UserRecord {
	rec := auth.RecordFromStored(u.StoredUser)
	rec.TenantID = u.TenantID
	return rec
}

func mapNotFound(err error, op string) error {
	if errors.Is(err, auth.ErrAuthUserNotFound) {
		return goauth.ErrUserNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}

// GetUserByIdentifier looks up a user by login identifier across tenants.
func (p *Provider) GetUserByIdentifier(identifier string) (goauth.UserRecord, error) {
	ctx, cancel := lookupContext()
	defer cancel()
	u, err := p.store.GetByIdentifier(ctx, identifier)
	if err != nil {
		return goauth.UserRecord{}, mapNotFound(err, "get user by identifier")
	}
	return record(u), nil
}

// GetUserByID looks up a user by canonical user id across tenants.
func (p *Provider) GetUserByID(userID string) (goauth.UserRecord, error) {
	ctx, cancel := lookupContext()
	defer cancel()
	u, err := p.store.GetByID(ctx, userID)
	if err != nil {
		return goauth.UserRecord{}, mapNotFound(err, "get user by id")
	}
	return record(u), nil
}

// GetUserByIdentifierInTenant resolves an identifier within tenantID only
// (goauth.TenantAwareUserProvider). The tenant predicate is applied in SQL; an
// identifier that exists only in another tenant, or an empty tenant, is
// reported as not found.
func (p *Provider) GetUserByIdentifierInTenant(ctx context.Context, tenantID, identifier string) (goauth.UserRecord, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	u, err := p.store.GetByIdentifierInTenant(ctx, tenantID, identifier)
	if err != nil {
		return goauth.UserRecord{}, mapNotFound(err, "get user by identifier in tenant")
	}
	return record(u), nil
}

// GetUserByIDInTenant resolves a user id within tenantID only
// (goauth.TenantAwareUserProvider). A user in another tenant, or an empty
// tenant, is reported as not found.
func (p *Provider) GetUserByIDInTenant(ctx context.Context, tenantID, userID string) (goauth.UserRecord, error) {
	ctx, cancel := boundedContext(ctx)
	defer cancel()
	u, err := p.store.GetByIDInTenant(ctx, tenantID, userID)
	if err != nil {
		return goauth.UserRecord{}, mapNotFound(err, "get user by id in tenant")
	}
	return record(u), nil
}

// CreateUser inserts a new user into the tenant goAuth passes (the request
// tenant), or the default tenant when it passes none.
func (p *Provider) CreateUser(ctx context.Context, input goauth.CreateUserInput) (goauth.UserRecord, error) {
	u, err := p.store.CreateInTenant(ctx, input.TenantID, auth.CreateStoredUserInput{
		Identifier:   input.Identifier,
		PasswordHash: input.PasswordHash,
		Role:         input.Role,
		Status:       auth.AccountStatusText(input.Status),
	})
	if err != nil {
		if errors.Is(err, auth.ErrAuthUserExists) {
			// goAuth maps this to ErrAccountExists after hashing the password,
			// so duplicate and fresh registrations take comparable time.
			return goauth.UserRecord{}, goauth.ErrProviderDuplicateIdentifier
		}
		return goauth.UserRecord{}, fmt.Errorf("create user: %w", err)
	}
	return record(u), nil
}

// UpdateAccountStatus updates the status through the core provider and stamps
// the user's tenant on the returned record.
func (p *Provider) UpdateAccountStatus(ctx context.Context, userID string, status goauth.AccountStatus) (goauth.UserRecord, error) {
	rec, err := p.StoreUserProvider.UpdateAccountStatus(ctx, userID, status)
	if err != nil {
		return goauth.UserRecord{}, err
	}
	tenantID, err := p.store.TenantOf(ctx, userID)
	if err != nil {
		return goauth.UserRecord{}, mapNotFound(err, "update account status")
	}
	rec.TenantID = tenantID
	return rec, nil
}

// ConsumeBackupCode marks a matching unused code as used, for a user of the
// request tenant only. goAuth can reach it (Engine.VerifyBackupCode) without
// resolving the user first, so the scope is enforced here; another tenant's
// user id behaves like a wrong code.
func (p *Provider) ConsumeBackupCode(ctx context.Context, userID string, codeHash [32]byte) (bool, error) {
	inScope, err := p.userInScope(ctx, userID)
	if err != nil || !inScope {
		return false, err
	}
	return p.StoreUserProvider.ConsumeBackupCode(ctx, userID, codeHash)
}

// userInScope reports whether userID belongs to the request tenant.
func (p *Provider) userInScope(ctx context.Context, userID string) (bool, error) {
	return p.store.InTenant(ctx, scopeTenant(ctx), userID)
}
