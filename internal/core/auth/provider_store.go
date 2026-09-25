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
// It implements goauth.TenantAwareUserProvider so goAuth v0.5.0 can build with
// MultiTenant.Enabled. The tenant-scoped lookups constrain the query to the
// tenant in SQL. When tenancy is disabled (the default) the provider leaves
// UserRecord.TenantID empty, exactly as v0.8.0 did, so goAuth keeps using its
// default tenant and single-tenant output is unchanged.
type StoreUserProvider struct {
	repo UserRepository
	// template:begin webauthn
	webauthnRepo WebAuthnCredentialRepository
	// template:end webauthn
	mfaRepo        MFARepository
	cipher         SecretCipher
	tenancyEnabled bool
}

var (
	_ goauth.UserProvider            = (*StoreUserProvider)(nil)
	_ goauth.TenantAwareUserProvider = (*StoreUserProvider)(nil)
)

const defaultLookupTimeout = 3 * time.Second

// NewStoreUserProvider creates a store-backed user provider.
func NewStoreUserProvider(repo UserRepository) *StoreUserProvider {
	return &StoreUserProvider{repo: repo}
}

// WithTenancy records whether multi-tenancy (TENANCY_ENABLED) is on. When on,
// returned records carry their stored tenant id; when off, TenantID is left
// empty to preserve single-tenant behavior.
func (p *StoreUserProvider) WithTenancy(enabled bool) *StoreUserProvider {
	if p != nil {
		p.tenancyEnabled = enabled
	}
	return p
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

// GetUserByIdentifier looks up a user by login identifier. It is tenant-blind,
// which is goAuth's contract when multi-tenancy is disabled.
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
	return p.toRecord(row), nil
}

// GetUserByID looks up a user by canonical user id (tenant-blind).
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
	return p.toRecord(row), nil
}

// GetUserByIdentifierInTenant resolves an identifier within tenantID only
// (goauth.TenantAwareUserProvider). The tenant predicate is applied in SQL; an
// identifier that exists only in another tenant, or an empty tenant, is
// reported as not found.
func (p *StoreUserProvider) GetUserByIdentifierInTenant(ctx context.Context, tenantID, identifier string) (goauth.UserRecord, error) {
	if p == nil || p.repo == nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}

	ctx, cancel := boundedContext(ctx)
	defer cancel()

	row, err := p.repo.GetByIdentifierInTenant(ctx, tenantID, identifier)
	if err != nil {
		if errors.Is(err, ErrAuthUserNotFound) {
			return goauth.UserRecord{}, goauth.ErrUserNotFound
		}
		return goauth.UserRecord{}, fmt.Errorf("get user by identifier in tenant: %w", err)
	}
	return p.toRecord(row), nil
}

// GetUserByIDInTenant resolves a user id within tenantID only
// (goauth.TenantAwareUserProvider). A user in another tenant, or an empty
// tenant, is reported as not found.
func (p *StoreUserProvider) GetUserByIDInTenant(ctx context.Context, tenantID, userID string) (goauth.UserRecord, error) {
	if p == nil || p.repo == nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}

	ctx, cancel := boundedContext(ctx)
	defer cancel()

	row, err := p.repo.GetByIDInTenant(ctx, tenantID, userID)
	if err != nil {
		if errors.Is(err, ErrAuthUserNotFound) {
			return goauth.UserRecord{}, goauth.ErrUserNotFound
		}
		return goauth.UserRecord{}, fmt.Errorf("get user by id in tenant: %w", err)
	}
	return p.toRecord(row), nil
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

// boundedContext applies the default lookup timeout to a caller context,
// keeping any shorter deadline the caller already set.
func boundedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, defaultLookupTimeout)
}

// CreateUser inserts a new auth user record.
func (p *StoreUserProvider) CreateUser(ctx context.Context, input goauth.CreateUserInput) (goauth.UserRecord, error) {
	if p == nil || p.repo == nil {
		return goauth.UserRecord{}, goauth.ErrUserNotFound
	}

	row, err := p.repo.Create(ctx, CreateStoredUserInput{
		TenantID:     p.createTenant(input.TenantID),
		Identifier:   input.Identifier,
		PasswordHash: input.PasswordHash,
		Role:         input.Role,
		Status:       mapAccountStatusToString(input.Status),
	})
	if err != nil {
		if errors.Is(err, ErrAuthUserExists) {
			// goAuth maps this to ErrAccountExists after hashing the password,
			// so duplicate and fresh registrations take comparable time.
			return goauth.UserRecord{}, goauth.ErrProviderDuplicateIdentifier
		}
		return goauth.UserRecord{}, fmt.Errorf("create user: %w", err)
	}
	return p.toRecord(row), nil
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
	return p.toRecord(row), nil
}

// --- TOTP and backup codes ---
//
// These delegate to the MFA repository. TOTP secrets are encrypted with the
// configured SecretCipher before they reach the database (goAuth hands the
// provider the raw secret). Every call is scoped to the request tenant when
// tenancy is enabled. With no MFA repository wired (AUTH_TOTP_ENABLED=false)
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
	state, found, err := p.mfaRepo.GetTOTP(ctx, p.scopeTenant(ctx), userID)
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
	return &goauth.TOTPRecord{
		Secret:          secret,
		Enabled:         state.Enabled,
		Verified:        state.Verified,
		LastUsedCounter: state.LastUsedCounter,
	}, nil
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
	return p.mfaErr(p.mfaRepo.UpsertTOTPSecret(ctx, p.scopeTenant(ctx), userID, ciphertext))
}

// DisableTOTP removes the TOTP secret and all backup codes.
func (p *StoreUserProvider) DisableTOTP(ctx context.Context, userID string) error {
	if !p.mfaReady() {
		return errMFAUnavailable
	}
	return p.mfaErr(p.mfaRepo.DisableTOTP(ctx, p.scopeTenant(ctx), userID))
}

// MarkTOTPVerified flags the stored secret as verified.
func (p *StoreUserProvider) MarkTOTPVerified(ctx context.Context, userID string) error {
	if !p.mfaReady() {
		return errMFAUnavailable
	}
	return p.mfaErr(p.mfaRepo.MarkTOTPVerified(ctx, p.scopeTenant(ctx), userID))
}

// UpdateTOTPLastUsedCounter records the last accepted TOTP counter. The update
// only moves forward, so a code replayed concurrently is rejected here even if
// both requests passed goAuth's in-memory counter check.
func (p *StoreUserProvider) UpdateTOTPLastUsedCounter(ctx context.Context, userID string, counter int64) error {
	if !p.mfaReady() {
		return errMFAUnavailable
	}
	return p.mfaErr(p.mfaRepo.AdvanceTOTPCounter(ctx, p.scopeTenant(ctx), userID, counter))
}

// GetBackupCodes returns the hashes of the user's unused backup codes.
func (p *StoreUserProvider) GetBackupCodes(ctx context.Context, userID string) ([]goauth.BackupCodeRecord, error) {
	if !p.mfaReady() {
		return []goauth.BackupCodeRecord{}, nil
	}
	hashes, err := p.mfaRepo.ListUnusedBackupCodes(ctx, p.scopeTenant(ctx), userID)
	if err != nil {
		return nil, err
	}
	out := make([]goauth.BackupCodeRecord, len(hashes))
	for i, h := range hashes {
		out[i] = goauth.BackupCodeRecord{Hash: h}
	}
	return out, nil
}

// ReplaceBackupCodes atomically replaces every backup code for the user.
func (p *StoreUserProvider) ReplaceBackupCodes(ctx context.Context, userID string, codes []goauth.BackupCodeRecord) error {
	if !p.mfaReady() {
		return errMFAUnavailable
	}
	hashes := make([][32]byte, len(codes))
	for i, c := range codes {
		hashes[i] = c.Hash
	}
	return p.mfaErr(p.mfaRepo.ReplaceBackupCodes(ctx, p.scopeTenant(ctx), userID, hashes))
}

// ConsumeBackupCode marks a matching unused code as used and reports whether
// one was consumed. Single use holds under concurrency (one SQL statement).
func (p *StoreUserProvider) ConsumeBackupCode(ctx context.Context, userID string, codeHash [32]byte) (bool, error) {
	if !p.mfaReady() {
		return false, nil
	}
	return p.mfaRepo.ConsumeBackupCode(ctx, p.scopeTenant(ctx), userID, codeHash)
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

// scopeTenant returns the tenant MFA queries are restricted to: empty (no
// restriction) with tenancy off; the request tenant, or goAuth's default
// tenant when none is attached, with tenancy on. goAuth resolves the same
// tenant from the context before calling any id-keyed provider method.
func (p *StoreUserProvider) scopeTenant(ctx context.Context) string {
	if p == nil || !p.tenancyEnabled {
		return ""
	}
	if tenantID, ok := RequestTenantFromContext(ctx); ok {
		return tenantID
	}
	return DefaultTenantID
}

// --- Mapping helpers ---

// toRecord maps a stored user to goAuth's record. TenantID is only populated
// when tenancy is enabled; with tenancy off it stays empty (v0.8.0 behavior).
func (p *StoreUserProvider) toRecord(row StoredUser) goauth.UserRecord {
	record := mapUserToRecord(row)
	if p != nil && p.tenancyEnabled {
		record.TenantID = row.TenantID
	}
	return record
}

// createTenant picks the tenant a new user is stored under. With tenancy on,
// goAuth passes the request's tenant; with it off every user belongs to the
// default tenant regardless of input.
func (p *StoreUserProvider) createTenant(inputTenant string) string {
	if p == nil || !p.tenancyEnabled {
		return DefaultTenantID
	}
	return tenantOrDefault(inputTenant)
}

func mapUserToRecord(row StoredUser) goauth.UserRecord {
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
