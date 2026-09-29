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
// It also implements goauth.WebAuthnCredentialProvider (provider_webauthn.go).
// The provider is keyed by the user ids goAuth hands it and never looks at a
// request scope: an optional feature that partitions users wraps it
// with a decorator installed through app.Hooks.UserProvider, which then owns
// the extra goAuth interfaces that scoping needs.
type StoreUserProvider struct {
	repo UserRepository
	// template:begin webauthn
	webauthnRepo WebAuthnCredentialRepository
	// template:end webauthn
	mfaRepo MFARepository
	cipher  SecretCipher
	tx      TxRunner
}

// TxRunner runs fn in one database transaction: repository calls made with the
// context passed to fn share it, and an error from fn rolls it back.
// *storage.Postgres implements it.
type TxRunner interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

var _ goauth.UserProvider = (*StoreUserProvider)(nil)

const defaultLookupTimeout = 3 * time.Second

// NewStoreUserProvider creates a store-backed user provider.
func NewStoreUserProvider(repo UserRepository) *StoreUserProvider {
	return &StoreUserProvider{repo: repo}
}

// WithMFA attaches TOTP/backup-code persistence and the cipher that encrypts
// TOTP secrets at rest. Needed only when AUTH_TOTP_ENABLED=true.
func (p *StoreUserProvider) WithMFA(repo MFARepository, cipher SecretCipher) *StoreUserProvider {
	if p != nil {
		p.mfaRepo = repo
		p.cipher = cipher
	}
	return p
}

// WithTx gives the provider the transaction boundary it needs for goAuth-driven
// writes that span several statements (replacing backup codes). goAuth calls
// the provider directly, with no service in between, so the provider is the
// service boundary for those writes and may call storage.Postgres.WithTx. Not
// needed (and safe to leave nil) for in-memory repositories.
func (p *StoreUserProvider) WithTx(tx TxRunner) *StoreUserProvider {
	if p != nil {
		p.tx = tx
	}
	return p
}

// inTx runs fn in a transaction when a runner is configured, otherwise
// directly (in-memory repositories have nothing to roll back).
func (p *StoreUserProvider) inTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if p.tx == nil {
		return fn(ctx)
	}
	return p.tx.WithTx(ctx, fn)
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
	return RecordFromStored(row), nil
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
	return RecordFromStored(row), nil
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
		Status:       AccountStatusText(input.Status),
	})
	if err != nil {
		if errors.Is(err, ErrAuthUserExists) {
			// goAuth maps this to ErrAccountExists after hashing the password,
			// so duplicate and fresh registrations take comparable time.
			return goauth.UserRecord{}, goauth.ErrProviderDuplicateIdentifier
		}
		return goauth.UserRecord{}, fmt.Errorf("create user: %w", err)
	}
	return RecordFromStored(row), nil
}

// UpdateAccountStatus updates account status and returns latest user record.
func (p *StoreUserProvider) UpdateAccountStatus(ctx context.Context, userID string, status goauth.AccountStatus) (goauth.UserRecord, error) {
	if p == nil || p.repo == nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}

	row, err := p.repo.UpdateStatus(ctx, userID, AccountStatusText(status))
	if err != nil {
		if errors.Is(err, ErrAuthUserNotFound) {
			return goauth.UserRecord{}, goauth.ErrUserNotFound
		}
		return goauth.UserRecord{}, fmt.Errorf("update account status: %w", err)
	}
	return RecordFromStored(row), nil
}

// --- TOTP and backup codes ---
//
// These delegate to the MFA repository. TOTP secrets are encrypted with the
// configured SecretCipher before they reach the database (goAuth hands the
// provider the raw secret). With no MFA repository wired (AUTH_TOTP_ENABLED=false)
// the store reports "no TOTP configured" and refuses mutations; goAuth never
// invokes them while TOTP is disabled.

// errMFAUnavailable is returned by MFA mutations when no MFA repository is wired.
var errMFAUnavailable = errors.New("mfa persistence is not configured")

// GetTOTPSecret returns the decrypted TOTP state. A user without TOTP gets an
// empty record (not an error), matching goAuth's provider contract.
func (p *StoreUserProvider) GetTOTPSecret(ctx context.Context, userID string) (*goauth.TOTPRecord, error) {
	if !p.mfaReady() {
		return &goauth.TOTPRecord{}, nil
	}
	state, found, err := p.mfaRepo.GetTOTP(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !found {
		return &goauth.TOTPRecord{}, nil
	}
	secret, err := p.cipher.Open(userID, state.SecretCiphertext)
	if err != nil {
		return nil, err
	}
	p.rotateSecretLazily(ctx, userID, state.SecretCiphertext, secret)
	return &goauth.TOTPRecord{
		Secret:          secret,
		Enabled:         state.Enabled,
		Verified:        state.Verified,
		LastUsedCounter: state.LastUsedCounter,
	}, nil
}

// rotateSecretLazily re-encrypts a secret that was opened with an old key (or
// in the v1 format) under the active key. It is best effort: the write-back is a
// compare-and-swap that changes nothing else, any error is ignored, and the
// request never fails because of it. Rows this misses are moved by the
// rotatetotpkey command.
func (p *StoreUserProvider) rotateSecretLazily(ctx context.Context, userID string, current, secret []byte) {
	rc, ok := p.cipher.(RotatableCipher)
	if !ok || !rc.NeedsRotation(current) {
		return
	}
	next, err := rc.Seal(userID, secret)
	if err != nil {
		return
	}
	_, _ = p.mfaRepo.RotateTOTPSecret(ctx, userID, current, next)
}

// EnableTOTP stores the (encrypted) secret and sets TOTP enabled to the
// verified flag: a fresh setup stays disabled until MarkTOTPVerified, then the
// confirm step's EnableTOTP call turns it on and advances the account version.
func (p *StoreUserProvider) EnableTOTP(ctx context.Context, userID string, secret []byte) error {
	if !p.mfaReady() {
		return errMFAUnavailable
	}
	if len(secret) == 0 {
		return errors.New("enable totp: empty secret")
	}
	ciphertext, err := p.cipher.Seal(userID, secret)
	if err != nil {
		return err
	}
	return p.mfaErr(p.mfaRepo.UpsertTOTPSecret(ctx, userID, ciphertext))
}

// DisableTOTP removes the TOTP secret and all backup codes.
func (p *StoreUserProvider) DisableTOTP(ctx context.Context, userID string) error {
	if !p.mfaReady() {
		return errMFAUnavailable
	}
	return p.mfaErr(p.mfaRepo.DisableTOTP(ctx, userID))
}

// MarkTOTPVerified flags the stored secret as verified.
func (p *StoreUserProvider) MarkTOTPVerified(ctx context.Context, userID string) error {
	if !p.mfaReady() {
		return errMFAUnavailable
	}
	return p.mfaErr(p.mfaRepo.MarkTOTPVerified(ctx, userID))
}

// UpdateTOTPLastUsedCounter records the last accepted TOTP counter. The update
// only moves forward, so a code replayed concurrently is rejected here even if
// both requests passed goAuth's in-memory counter check.
func (p *StoreUserProvider) UpdateTOTPLastUsedCounter(ctx context.Context, userID string, counter int64) error {
	if !p.mfaReady() {
		return errMFAUnavailable
	}
	return p.mfaErr(p.mfaRepo.AdvanceTOTPCounter(ctx, userID, counter))
}

// GetBackupCodes returns the hashes of the user's unused backup codes.
func (p *StoreUserProvider) GetBackupCodes(ctx context.Context, userID string) ([]goauth.BackupCodeRecord, error) {
	if !p.mfaReady() {
		return []goauth.BackupCodeRecord{}, nil
	}
	hashes, err := p.mfaRepo.ListUnusedBackupCodes(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]goauth.BackupCodeRecord, len(hashes))
	for i, h := range hashes {
		out[i] = goauth.BackupCodeRecord{Hash: h}
	}
	return out, nil
}

// ReplaceBackupCodes replaces every backup code for the user in one
// transaction, so a failure never leaves the user with a partial or empty set.
func (p *StoreUserProvider) ReplaceBackupCodes(ctx context.Context, userID string, codes []goauth.BackupCodeRecord) error {
	if !p.mfaReady() {
		return errMFAUnavailable
	}
	hashes := make([][32]byte, len(codes))
	for i, c := range codes {
		hashes[i] = c.Hash
	}
	return p.mfaErr(p.inTx(ctx, func(ctx context.Context) error {
		return p.mfaRepo.ReplaceBackupCodes(ctx, userID, hashes)
	}))
}

// ConsumeBackupCode marks a matching unused code as used and reports whether
// one was consumed. Single use holds under concurrency (one SQL statement).
func (p *StoreUserProvider) ConsumeBackupCode(ctx context.Context, userID string, codeHash [32]byte) (bool, error) {
	if !p.mfaReady() {
		return false, nil
	}
	return p.mfaRepo.ConsumeBackupCode(ctx, userID, codeHash)
}

func (p *StoreUserProvider) mfaReady() bool {
	return p != nil && p.mfaRepo != nil && p.cipher != nil
}

func (p *StoreUserProvider) mfaErr(err error) error {
	if errors.Is(err, ErrAuthUserNotFound) {
		return goauth.ErrUserNotFound
	}
	return err
}

// --- Mapping helpers ---

// RecordFromStored maps a stored user to goAuth's record. It leaves any
// feature-owned goauth.UserRecord field for the feature's
// provider wrapper to fill.
func RecordFromStored(row StoredUser) goauth.UserRecord {
	return goauth.UserRecord{
		UserID:         row.ID,
		Identifier:     row.Email,
		PasswordHash:   row.PasswordHash,
		Role:           row.Role,
		Status:         parseAccountStatus(row.Status),
		TOTPEnabled:    row.TOTPEnabled,
		AccountVersion: row.AccountVersion,
	}
}

// AccountStatusText is the stored text of a goAuth account status.
func AccountStatusText(s goauth.AccountStatus) string {
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
