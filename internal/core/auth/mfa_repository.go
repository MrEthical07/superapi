package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

// ErrTOTPCounterNotAdvanced is returned when a TOTP counter update would not
// move the stored counter forward (a replayed or concurrently used code).
var ErrTOTPCounterNotAdvanced = errors.New("totp counter not advanced")

// TOTPState is the stored TOTP state for a user. SecretCiphertext is the
// encrypted secret exactly as persisted; the provider decrypts it.
type TOTPState struct {
	SecretCiphertext []byte
	Verified         bool
	Enabled          bool
	LastUsedCounter  int64
}

// MFARepository persists TOTP secrets and backup codes.
//
// tenantID scopes every operation: empty means tenant-blind (tenancy
// disabled); a value restricts the operation to users in that tenant, so a
// user id from another tenant behaves as not found. Each method is a single
// SQL statement and therefore atomic without a service-owned transaction.
type MFARepository interface {
	// GetTOTP returns the user's TOTP state, or found=false when none exists.
	GetTOTP(ctx context.Context, tenantID, userID string) (state TOTPState, found bool, err error)
	// UpsertTOTPSecret stores the encrypted secret and syncs the enabled flag
	// to the verified flag. ErrAuthUserNotFound when the user is not in scope.
	UpsertTOTPSecret(ctx context.Context, tenantID, userID string, ciphertext []byte) error
	// MarkTOTPVerified flags the stored secret as verified.
	MarkTOTPVerified(ctx context.Context, tenantID, userID string) error
	// AdvanceTOTPCounter moves last_used_counter forward only; otherwise it
	// returns ErrTOTPCounterNotAdvanced.
	AdvanceTOTPCounter(ctx context.Context, tenantID, userID string, counter int64) error
	// DisableTOTP removes the secret and all backup codes.
	DisableTOTP(ctx context.Context, tenantID, userID string) error
	// ListUnusedBackupCodes returns the hashes of backup codes not yet used.
	ListUnusedBackupCodes(ctx context.Context, tenantID, userID string) ([][32]byte, error)
	// ReplaceBackupCodes atomically replaces every backup code for the user.
	ReplaceBackupCodes(ctx context.Context, tenantID, userID string, hashes [][32]byte) error
	// ConsumeBackupCode marks a matching unused code as used, reporting
	// whether a code was consumed. Concurrent consumes of one code cannot both
	// succeed.
	ConsumeBackupCode(ctx context.Context, tenantID, userID string, hash [32]byte) (bool, error)
}

type sqlcMFARepository struct {
	pg *storage.Postgres
}

// NewMFARepository creates an MFA repository backed by sqlc queries.
func NewMFARepository(pg *storage.Postgres) MFARepository {
	if pg == nil {
		return nil
	}
	return &sqlcMFARepository{pg: pg}
}

func (r *sqlcMFARepository) GetTOTP(ctx context.Context, tenantID, userID string) (TOTPState, bool, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return TOTPState{}, false, nil
	}
	row, err := r.pg.Queries(ctx).GetUserTOTP(ctx, sqlcgen.GetUserTOTPParams{UserID: id, TenantID: optionalText(tenantID)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TOTPState{}, false, nil
		}
		return TOTPState{}, false, fmt.Errorf("get totp: %w", err)
	}
	return TOTPState{
		SecretCiphertext: row.SecretCiphertext,
		Verified:         row.Verified,
		Enabled:          row.TotpEnabled,
		LastUsedCounter:  row.LastUsedCounter,
	}, true, nil
}

func (r *sqlcMFARepository) UpsertTOTPSecret(ctx context.Context, tenantID, userID string, ciphertext []byte) error {
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}
	if _, err := r.pg.Queries(ctx).UpsertUserTOTPSecret(ctx, sqlcgen.UpsertUserTOTPSecretParams{
		UserID:           id,
		TenantID:         optionalText(tenantID),
		SecretCiphertext: ciphertext,
	}); err != nil {
		return notFoundOr(err, "upsert totp secret")
	}
	return nil
}

func (r *sqlcMFARepository) MarkTOTPVerified(ctx context.Context, tenantID, userID string) error {
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}
	if _, err := r.pg.Queries(ctx).MarkUserTOTPVerified(ctx, sqlcgen.MarkUserTOTPVerifiedParams{UserID: id, TenantID: optionalText(tenantID)}); err != nil {
		return notFoundOr(err, "mark totp verified")
	}
	return nil
}

func (r *sqlcMFARepository) AdvanceTOTPCounter(ctx context.Context, tenantID, userID string, counter int64) error {
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}
	if _, err := r.pg.Queries(ctx).AdvanceUserTOTPCounter(ctx, sqlcgen.AdvanceUserTOTPCounterParams{
		Counter:  counter,
		UserID:   id,
		TenantID: optionalText(tenantID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTOTPCounterNotAdvanced
		}
		return fmt.Errorf("advance totp counter: %w", err)
	}
	return nil
}

func (r *sqlcMFARepository) DisableTOTP(ctx context.Context, tenantID, userID string) error {
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}
	if _, err := r.pg.Queries(ctx).DisableUserTOTP(ctx, sqlcgen.DisableUserTOTPParams{UserID: id, TenantID: optionalText(tenantID)}); err != nil {
		return notFoundOr(err, "disable totp")
	}
	return nil
}

func (r *sqlcMFARepository) ListUnusedBackupCodes(ctx context.Context, tenantID, userID string) ([][32]byte, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return nil, nil
	}
	rows, err := r.pg.Queries(ctx).ListUnusedBackupCodes(ctx, sqlcgen.ListUnusedBackupCodesParams{UserID: id, TenantID: optionalText(tenantID)})
	if err != nil {
		return nil, fmt.Errorf("list backup codes: %w", err)
	}
	out := make([][32]byte, 0, len(rows))
	for _, raw := range rows {
		if len(raw) != 32 {
			continue
		}
		var h [32]byte
		copy(h[:], raw)
		out = append(out, h)
	}
	return out, nil
}

func (r *sqlcMFARepository) ReplaceBackupCodes(ctx context.Context, tenantID, userID string, hashes [][32]byte) error {
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}
	raw := make([][]byte, len(hashes))
	for i := range hashes {
		raw[i] = append([]byte(nil), hashes[i][:]...)
	}
	inserted, err := r.pg.Queries(ctx).ReplaceBackupCodes(ctx, sqlcgen.ReplaceBackupCodesParams{
		CodeHashes: raw,
		UserID:     id,
		TenantID:   optionalText(tenantID),
	})
	if err != nil {
		return fmt.Errorf("replace backup codes: %w", err)
	}
	if inserted != int64(len(hashes)) {
		// No user in scope (nothing inserted) or a partial insert.
		return ErrAuthUserNotFound
	}
	return nil
}

func (r *sqlcMFARepository) ConsumeBackupCode(ctx context.Context, tenantID, userID string, hash [32]byte) (bool, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return false, nil
	}
	consumed, err := r.pg.Queries(ctx).ConsumeBackupCode(ctx, sqlcgen.ConsumeBackupCodeParams{
		UserID:   id,
		CodeHash: hash[:],
		TenantID: optionalText(tenantID),
	})
	if err != nil {
		return false, fmt.Errorf("consume backup code: %w", err)
	}
	return consumed > 0, nil
}

func notFoundOr(err error, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthUserNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}
