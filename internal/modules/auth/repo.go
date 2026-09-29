package auth

import (
	"context"
	"errors"

	coreauth "github.com/MrEthical07/superapi/internal/core/auth"
)

// recipient is the delivery target for a reset or verification message.
type recipient struct {
	Address             string
	PendingVerification bool
}

// accountRepository reads the account facts the auth service needs beyond
// what goAuth exposes: where to deliver reset/verification messages, and
// whether TOTP is currently enabled.
type accountRepository interface {
	// FindRecipient returns the account for identifier, in the same scope
	// goAuth's own lookup uses (an optional feature that scopes users
	// decorates the repository). found=false when no account exists; callers
	// must not reveal that to the client.
	FindRecipient(ctx context.Context, identifier string) (recipient, bool, error)
	// TOTPEnabled reports whether the user has TOTP turned on.
	TOTPEnabled(ctx context.Context, userID string) (bool, error)
}

type userAccountRepository struct {
	users coreauth.UserRepository
}

func newAccountRepository(users coreauth.UserRepository) accountRepository {
	if users == nil {
		return nil
	}
	return &userAccountRepository{users: users}
}

func (r *userAccountRepository) TOTPEnabled(ctx context.Context, userID string) (bool, error) {
	row, err := r.users.GetByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return row.TOTPEnabled, nil
}

func (r *userAccountRepository) FindRecipient(ctx context.Context, identifier string) (recipient, bool, error) {
	row, err := r.users.GetByIdentifier(ctx, coreauth.NormalizeIdentifier(identifier))
	if err != nil {
		if errors.Is(err, coreauth.ErrAuthUserNotFound) {
			return recipient{}, false, nil
		}
		return recipient{}, false, err
	}
	return recipient{
		Address:             row.Email,
		PendingVerification: row.Status == "pending_verification",
	}, true, nil
}
