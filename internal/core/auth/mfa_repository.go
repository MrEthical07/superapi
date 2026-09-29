package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

// ErrTOTPCounterNotAdvanced is returned when a TOTP counter update would not
// move the stored counter forward (a replayed or concurrently used code).
var ErrTOTPCounterNotAdvanced = errors.New("totp counter not advanced")

// ErrTransactionRequired is returned by an operation that is only correct as
// several statements in one transaction when it is called outside one.
var ErrTransactionRequired = errors.New("operation must run inside a transaction")

// TOTPSecretRow is one stored TOTP ciphertext, as scanned by key rotation.
type TOTPSecretRow struct {
	UserID     string
	Ciphertext []byte
}

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
// Every method is keyed by the globally unique user id and is a single SQL
// statement, therefore atomic on its own, except ReplaceBackupCodes, which
// needs the caller's transaction. Callers reach it only with the id of a user
// goAuth already resolved.
type MFARepository interface {
	// GetTOTP returns the user's TOTP state, or found=false when none exists.
	GetTOTP(ctx context.Context, userID string) (state TOTPState, found bool, err error)
	// UpsertTOTPSecret stores the encrypted secret and syncs the enabled flag
	// to the verified flag. ErrAuthUserNotFound when the user does not exist.
	UpsertTOTPSecret(ctx context.Context, userID string, ciphertext []byte) error
	// MarkTOTPVerified flags the stored secret as verified.
	MarkTOTPVerified(ctx context.Context, userID string) error
	// AdvanceTOTPCounter moves last_used_counter forward only; otherwise it
	// returns ErrTOTPCounterNotAdvanced.
	AdvanceTOTPCounter(ctx context.Context, userID string, counter int64) error
	// DisableTOTP removes the secret and all backup codes.
	DisableTOTP(ctx context.Context, userID string) error
	// ListUnusedBackupCodes returns the hashes of backup codes not yet used.
	ListUnusedBackupCodes(ctx context.Context, userID string) ([][32]byte, error)
	// RotateTOTPSecret swaps the stored ciphertext for next only if it still
	// equals prev (compare-and-swap), reporting whether it did. It is how a
	// secret is re-encrypted under a new key without ever overwriting a
	// concurrent change.
	RotateTOTPSecret(ctx context.Context, userID string, prev, next []byte) (swapped bool, err error)
	// ListTOTPSecrets returns up to limit stored ciphertexts with a user id
	// greater than afterUserID ("" for the first page), in
	// user id order. It is for the rotatetotpkey command.
	ListTOTPSecrets(ctx context.Context, afterUserID string, limit int) ([]TOTPSecretRow, error)
	// ReplaceBackupCodes replaces every backup code for the user. It is a
	// delete followed by an insert, so the caller must run it inside one
	// transaction (storage.Postgres.WithTx); the relational implementation
	// returns ErrTransactionRequired otherwise. The StoreUserProvider does
	// this for every goAuth-driven replacement.
	ReplaceBackupCodes(ctx context.Context, userID string, hashes [][32]byte) error
	// ConsumeBackupCode marks a matching unused code as used, reporting
	// whether a code was consumed. Concurrent consumes of one code cannot both
	// succeed.
	ConsumeBackupCode(ctx context.Context, userID string, hash [32]byte) (bool, error)
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

func (r *sqlcMFARepository) GetTOTP(ctx context.Context, userID string) (TOTPState, bool, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return TOTPState{}, false, nil
	}
	row, err := r.pg.Queries(ctx).GetUserTOTP(ctx, id)
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

func (r *sqlcMFARepository) UpsertTOTPSecret(ctx context.Context, userID string, ciphertext []byte) error {
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}
	if _, err := r.pg.Queries(ctx).UpsertUserTOTPSecret(ctx, sqlcgen.UpsertUserTOTPSecretParams{
		UserID:           id,
		SecretCiphertext: ciphertext,
	}); err != nil {
		return notFoundOr(err, "upsert totp secret")
	}
	return nil
}

func (r *sqlcMFARepository) MarkTOTPVerified(ctx context.Context, userID string) error {
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}
	if _, err := r.pg.Queries(ctx).MarkUserTOTPVerified(ctx, id); err != nil {
		return notFoundOr(err, "mark totp verified")
	}
	return nil
}

func (r *sqlcMFARepository) AdvanceTOTPCounter(ctx context.Context, userID string, counter int64) error {
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}
	if _, err := r.pg.Queries(ctx).AdvanceUserTOTPCounter(ctx, sqlcgen.AdvanceUserTOTPCounterParams{
		Counter: counter,
		UserID:  id,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTOTPCounterNotAdvanced
		}
		return fmt.Errorf("advance totp counter: %w", err)
	}
	return nil
}

func (r *sqlcMFARepository) RotateTOTPSecret(ctx context.Context, userID string, prev, next []byte) (bool, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return false, err
	}
	swapped, err := r.pg.Queries(ctx).RotateUserTOTPSecret(ctx, sqlcgen.RotateUserTOTPSecretParams{
		NextCiphertext: next,
		UserID:         id,
		PrevCiphertext: prev,
	})
	if err != nil {
		return false, fmt.Errorf("rotate totp secret: %w", err)
	}
	return swapped > 0, nil
}

func (r *sqlcMFARepository) ListTOTPSecrets(ctx context.Context, afterUserID string, limit int) ([]TOTPSecretRow, error) {
	if limit < 1 {
		return nil, nil
	}
	var after pgtype.UUID // invalid (NULL) selects the first page
	if afterUserID != "" {
		parsed, err := parseUserID(afterUserID)
		if err != nil {
			return nil, err
		}
		after = parsed
	}
	rows, err := r.pg.Queries(ctx).ListUserTOTPSecrets(ctx, sqlcgen.ListUserTOTPSecretsParams{
		AfterUserID: after,
		RowLimit:    int32(min(limit, 1_000_000)),
	})
	if err != nil {
		return nil, fmt.Errorf("list totp secrets: %w", err)
	}
	out := make([]TOTPSecretRow, len(rows))
	for i, row := range rows {
		out[i] = TOTPSecretRow{UserID: uuidToString(row.UserID), Ciphertext: row.SecretCiphertext}
	}
	return out, nil
}

func (r *sqlcMFARepository) DisableTOTP(ctx context.Context, userID string) error {
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}
	if _, err := r.pg.Queries(ctx).DisableUserTOTP(ctx, id); err != nil {
		return notFoundOr(err, "disable totp")
	}
	return nil
}

func (r *sqlcMFARepository) ListUnusedBackupCodes(ctx context.Context, userID string) ([][32]byte, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return nil, nil
	}
	rows, err := r.pg.Queries(ctx).ListUnusedBackupCodes(ctx, id)
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

func (r *sqlcMFARepository) ReplaceBackupCodes(ctx context.Context, userID string, hashes [][32]byte) error {
	// Two statements (delete, then insert) are only atomic in one transaction,
	// and a half-done replacement would leave the user without backup codes.
	// The repository does not own transaction boundaries, so it refuses to run
	// outside the caller's.
	if !r.pg.InTx(ctx) {
		return fmt.Errorf("replace backup codes: %w", ErrTransactionRequired)
	}
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}

	// The unique (user_id, code_hash) constraint would reject a repeated hash
	// within the new set; a code appearing twice is one code.
	seen := make(map[[32]byte]struct{}, len(hashes))
	raw := make([][]byte, 0, len(hashes))
	for _, h := range hashes {
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		raw = append(raw, append([]byte(nil), h[:]...))
	}

	q := r.pg.Queries(ctx)
	if _, err := q.DeleteBackupCodes(ctx, id); err != nil {
		return fmt.Errorf("replace backup codes: delete: %w", err)
	}
	inserted, err := q.InsertBackupCodes(ctx, sqlcgen.InsertBackupCodesParams{
		CodeHashes: raw,
		UserID:     id,
	})
	if err != nil {
		return fmt.Errorf("replace backup codes: insert: %w", err)
	}
	if inserted != int64(len(raw)) {
		// No such user (nothing inserted). The caller's transaction rolls
		// back the delete when this error is returned.
		return ErrAuthUserNotFound
	}
	return nil
}

func (r *sqlcMFARepository) ConsumeBackupCode(ctx context.Context, userID string, hash [32]byte) (bool, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return false, nil
	}
	consumed, err := r.pg.Queries(ctx).ConsumeBackupCode(ctx, sqlcgen.ConsumeBackupCodeParams{
		UserID:   id,
		CodeHash: hash[:],
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
