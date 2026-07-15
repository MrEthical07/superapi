package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	goauth "github.com/MrEthical07/goAuth"
	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

// ErrWebAuthnCredentialNotFound is returned when a credential expected to exist
// is absent (for example when removing an unknown credential).
var ErrWebAuthnCredentialNotFound = errors.New("webauthn credential not found")

// WebAuthnCredentialRepository defines domain-level persistence for WebAuthn
// credentials. It mirrors the goauth.WebAuthnCredentialProvider capability but
// keeps sqlc/pgx types out of its contract.
type WebAuthnCredentialRepository interface {
	ListByUser(ctx context.Context, userID string) ([]goauth.WebAuthnCredential, error)
	Add(ctx context.Context, userID string, cred goauth.WebAuthnCredential) error
	UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint32) error
	Delete(ctx context.Context, userID string, credentialID []byte) error
}

type sqlcWebAuthnRepository struct {
	pg *storage.Postgres
}

// NewWebAuthnCredentialRepository creates a WebAuthn credential repository
// backed by sqlc queries over the relational Postgres boundary.
func NewWebAuthnCredentialRepository(pg *storage.Postgres) WebAuthnCredentialRepository {
	if pg == nil {
		return nil
	}
	return &sqlcWebAuthnRepository{pg: pg}
}

func (r *sqlcWebAuthnRepository) ListByUser(ctx context.Context, userID string) ([]goauth.WebAuthnCredential, error) {
	id, err := parseUserID(userID)
	if err != nil {
		// An unresolvable user id simply has no credentials.
		return nil, nil
	}

	rows, err := r.pg.Queries(ctx).ListWebAuthnCredentialsByUser(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list webauthn credentials: %w", err)
	}

	creds := make([]goauth.WebAuthnCredential, 0, len(rows))
	for _, row := range rows {
		creds = append(creds, mapWebAuthnRow(row))
	}
	return creds, nil
}

func (r *sqlcWebAuthnRepository) Add(ctx context.Context, userID string, cred goauth.WebAuthnCredential) error {
	id, err := parseUserID(userID)
	if err != nil {
		return fmt.Errorf("add webauthn credential: %w", err)
	}

	if err := r.pg.Queries(ctx).CreateWebAuthnCredential(ctx, sqlcgen.CreateWebAuthnCredentialParams{
		CredentialID:    cred.CredentialID,
		UserID:          id,
		PublicKey:       cred.PublicKey,
		AttestationType: cred.AttestationType,
		Transports:      append([]string(nil), cred.Transports...),
		UserPresent:     cred.UserPresent,
		UserVerified:    cred.UserVerified,
		BackupEligible:  cred.BackupEligible,
		BackupState:     cred.BackupState,
		Aaguid:          cred.AAGUID,
		SignCount:       int64(cred.SignCount),
		Attachment:      cred.Attachment,
		CreatedAt:       timestamptz(cred.CreatedAt),
		LastUsedAt:      timestamptz(cred.LastUsedAt),
	}); err != nil {
		return fmt.Errorf("add webauthn credential: %w", err)
	}
	return nil
}

func (r *sqlcWebAuthnRepository) UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint32) error {
	if _, err := r.pg.Queries(ctx).UpdateWebAuthnCredentialSignCount(ctx, sqlcgen.UpdateWebAuthnCredentialSignCountParams{
		CredentialID: credentialID,
		SignCount:    int64(signCount),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWebAuthnCredentialNotFound
		}
		return fmt.Errorf("update webauthn sign count: %w", err)
	}
	return nil
}

func (r *sqlcWebAuthnRepository) Delete(ctx context.Context, userID string, credentialID []byte) error {
	id, err := parseUserID(userID)
	if err != nil {
		return ErrWebAuthnCredentialNotFound
	}

	if _, err := r.pg.Queries(ctx).DeleteWebAuthnCredential(ctx, sqlcgen.DeleteWebAuthnCredentialParams{
		UserID:       id,
		CredentialID: credentialID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWebAuthnCredentialNotFound
		}
		return fmt.Errorf("delete webauthn credential: %w", err)
	}
	return nil
}

func mapWebAuthnRow(row sqlcgen.WebauthnCredential) goauth.WebAuthnCredential {
	return goauth.WebAuthnCredential{
		CredentialID:    row.CredentialID,
		PublicKey:       row.PublicKey,
		AttestationType: row.AttestationType,
		Transports:      append([]string(nil), row.Transports...),
		UserPresent:     row.UserPresent,
		UserVerified:    row.UserVerified,
		BackupEligible:  row.BackupEligible,
		BackupState:     row.BackupState,
		AAGUID:          row.Aaguid,
		SignCount:       uint32(row.SignCount),
		Attachment:      row.Attachment,
		CreatedAt:       timeFromTimestamptz(row.CreatedAt),
		LastUsedAt:      timeFromTimestamptz(row.LastUsedAt),
	}
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		// Let the column default (NOW()) apply for zero timestamps.
		return pgtype.Timestamptz{Valid: false}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func timeFromTimestamptz(ts pgtype.Timestamptz) time.Time {
	if !ts.Valid {
		return time.Time{}
	}
	return ts.Time
}
