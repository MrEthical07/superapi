package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	goauth "github.com/MrEthical07/goAuth"
)

// StoreUserProvider is the DB-backed UserProvider for goAuth.
// It depends on a domain repository, not backend query objects.
type StoreUserProvider struct {
	repo UserRepository
}

const defaultLookupTimeout = 3 * time.Second

// NewStoreUserProvider creates a store-backed user provider.
func NewStoreUserProvider(repo UserRepository) *StoreUserProvider {
	return &StoreUserProvider{repo: repo}
}

// GetUserByIdentifier looks up a user by login identifier.
func (p *StoreUserProvider) GetUserByIdentifier(identifier string) (goauth.UserRecord, error) {
	if p == nil || p.repo == nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}

	ctx, cancel := lookupContext()
	defer cancel()

	row, err := p.repo.GetByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, ErrAuthUserNotFound) {
			return goauth.UserRecord{}, goauth.ErrUserNotFound
		}
		return goauth.UserRecord{}, fmt.Errorf("get user by identifier: %w", err)
	}
	return mapUserToRecord(row), nil
}

// GetUserByID looks up a user by canonical user id.
func (p *StoreUserProvider) GetUserByID(userID string) (goauth.UserRecord, error) {
	if p == nil || p.repo == nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}

	ctx, cancel := lookupContext()
	defer cancel()

	row, err := p.repo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrAuthUserNotFound) {
			return goauth.UserRecord{}, goauth.ErrUserNotFound
		}
		return goauth.UserRecord{}, fmt.Errorf("get user by id: %w", err)
	}
	return mapUserToRecord(row), nil
}

// UpdatePasswordHash persists a new password hash for the given user.
func (p *StoreUserProvider) UpdatePasswordHash(userID string, newHash string) error {
	if p == nil || p.repo == nil {
		return goauth.ErrUserNotFound
	}

	ctx, cancel := lookupContext()
	defer cancel()

	if err := p.repo.UpdatePasswordHash(ctx, userID, newHash); err != nil {
		if errors.Is(err, ErrAuthUserNotFound) {
			return goauth.ErrUserNotFound
		}
		return fmt.Errorf("update password hash: %w", err)
	}
	return nil
}

func lookupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultLookupTimeout)
}

// CreateUser inserts a new auth user record.
func (p *StoreUserProvider) CreateUser(ctx context.Context, input goauth.CreateUserInput) (goauth.UserRecord, error) {
	if p == nil || p.repo == nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}

	row, err := p.repo.Create(ctx, CreateStoredUserInput{
		Identifier:   input.Identifier,
		PasswordHash: input.PasswordHash,
		Role:         input.Role,
		Status:       mapAccountStatusToString(input.Status),
	})
	if err != nil {
		return goauth.UserRecord{}, fmt.Errorf("create user: %w", err)
	}
	return mapUserToRecord(row), nil
}

// UpdateAccountStatus updates account status and returns latest user record.
func (p *StoreUserProvider) UpdateAccountStatus(ctx context.Context, userID string, status goauth.AccountStatus) (goauth.UserRecord, error) {
	if p == nil || p.repo == nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}

	row, err := p.repo.UpdateStatus(ctx, userID, mapAccountStatusToString(status))
	if err != nil {
		if errors.Is(err, ErrAuthUserNotFound) {
			return goauth.UserRecord{}, goauth.ErrUserNotFound
		}
		return goauth.UserRecord{}, fmt.Errorf("update account status: %w", err)
	}
	return mapUserToRecord(row), nil
}

// TOTP stubs — implement when MFA is needed.
func (p *StoreUserProvider) GetTOTPSecret(_ context.Context, _ string) (*goauth.TOTPRecord, error) {
	return nil, goauth.ErrUnauthorized
}

// EnableTOTP is a stub until MFA persistence is implemented.
func (p *StoreUserProvider) EnableTOTP(_ context.Context, _ string, _ []byte) error {
	return goauth.ErrUnauthorized
}

// DisableTOTP is a stub until MFA persistence is implemented.
func (p *StoreUserProvider) DisableTOTP(_ context.Context, _ string) error {
	return goauth.ErrUnauthorized
}

// MarkTOTPVerified is a stub until MFA persistence is implemented.
func (p *StoreUserProvider) MarkTOTPVerified(_ context.Context, _ string) error {
	return goauth.ErrUnauthorized
}

// UpdateTOTPLastUsedCounter is a stub until MFA persistence is implemented.
func (p *StoreUserProvider) UpdateTOTPLastUsedCounter(_ context.Context, _ string, _ int64) error {
	return goauth.ErrUnauthorized
}

// Backup code stubs — implement when MFA is needed.
func (p *StoreUserProvider) GetBackupCodes(_ context.Context, _ string) ([]goauth.BackupCodeRecord, error) {
	return nil, goauth.ErrUnauthorized
}

// ReplaceBackupCodes is a stub until backup-code persistence is implemented.
func (p *StoreUserProvider) ReplaceBackupCodes(_ context.Context, _ string, _ []goauth.BackupCodeRecord) error {
	return goauth.ErrUnauthorized
}

// ConsumeBackupCode is a stub until backup-code persistence is implemented.
func (p *StoreUserProvider) ConsumeBackupCode(_ context.Context, _ string, _ [32]byte) (bool, error) {
	return false, goauth.ErrUnauthorized
}

// --- Mapping helpers ---

func mapUserToRecord(row StoredUser) goauth.UserRecord {
	return goauth.UserRecord{
		UserID:       row.ID,
		Identifier:   row.Email,
		PasswordHash: row.PasswordHash,
		Role:         row.Role,
		Status:       parseAccountStatus(row.Status),
	}
}

func mapAccountStatusToString(s goauth.AccountStatus) string {
	switch s {
	case goauth.AccountActive:
		return "active"
	case goauth.AccountPendingVerification:
		return "pending_verification"
	case goauth.AccountDisabled:
		return "disabled"
	case goauth.AccountLocked:
		return "locked"
	case goauth.AccountDeleted:
		return "deleted"
	default:
		return "active"
	}
}

func parseAccountStatus(s string) goauth.AccountStatus {
	switch s {
	case "active":
		return goauth.AccountActive
	case "pending_verification":
		return goauth.AccountPendingVerification
	case "disabled":
		return goauth.AccountDisabled
	case "locked":
		return goauth.AccountLocked
	case "deleted":
		return goauth.AccountDeleted
	default:
		return goauth.AccountActive
	}
}
