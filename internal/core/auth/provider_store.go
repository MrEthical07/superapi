package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	goauth "github.com/MrEthical07/goAuth"
)

// StoreUserProvider is the DB-backed UserProvider for goAuth.
// It depends on domain repositories, not backend query objects.
//
// It also implements goauth.WebAuthnCredentialProvider. When no WebAuthn
// repository is wired the credential methods behave as an empty store; goAuth
// only requires the capability when WebAuthn is enabled (WEBAUTHN_ENABLED), so
// providing these methods unconditionally is safe.
type StoreUserProvider struct {
	repo         UserRepository
	webauthnRepo WebAuthnCredentialRepository
}

const defaultLookupTimeout = 3 * time.Second

// NewStoreUserProvider creates a store-backed user provider.
func NewStoreUserProvider(repo UserRepository) *StoreUserProvider {
	return &StoreUserProvider{repo: repo}
}

// WithWebAuthnRepository attaches a WebAuthn credential repository so the
// provider can satisfy goAuth's WebAuthn ceremonies. Optional; only needed when
// WebAuthn is enabled.
func (p *StoreUserProvider) WithWebAuthnRepository(repo WebAuthnCredentialRepository) *StoreUserProvider {
	if p != nil {
		p.webauthnRepo = repo
	}
	return p
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
