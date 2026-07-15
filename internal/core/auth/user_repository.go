package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

var ErrAuthUserNotFound = errors.New("auth user not found")

// StoredUser is the storage-layer projection used by the auth repository.
type StoredUser struct {
	ID           string
	Email        string
	PasswordHash string
	Role         string
	Status       string
}

// CreateStoredUserInput is the repository input model for creating auth users.
type CreateStoredUserInput struct {
	Identifier   string
	PasswordHash string
	Role         string
	Status       string
}

// UserRepository defines domain-level auth user persistence operations.
type UserRepository interface {
	GetByIdentifier(ctx context.Context, identifier string) (StoredUser, error)
	GetByID(ctx context.Context, userID string) (StoredUser, error)
	UpdatePasswordHash(ctx context.Context, userID, newHash string) error
	Create(ctx context.Context, input CreateStoredUserInput) (StoredUser, error)
	UpdateStatus(ctx context.Context, userID string, status string) (StoredUser, error)
}

type sqlcUserRepository struct {
	pg *storage.Postgres
}

// NewRelationalUserRepository creates an auth repository backed by sqlc queries
// over the relational Postgres boundary.
func NewRelationalUserRepository(pg *storage.Postgres) UserRepository {
	if pg == nil {
		return nil
	}
	return &sqlcUserRepository{pg: pg}
}

func (r *sqlcUserRepository) GetByIdentifier(ctx context.Context, identifier string) (StoredUser, error) {
	row, err := r.pg.Queries(ctx).GetAuthUserByLogin(ctx, strings.TrimSpace(identifier))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredUser{}, ErrAuthUserNotFound
		}
		return StoredUser{}, fmt.Errorf("get user by identifier: %w", err)
	}
	return mapUserRow(row), nil
}

func (r *sqlcUserRepository) GetByID(ctx context.Context, userID string) (StoredUser, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return StoredUser{}, fmt.Errorf("get user by id: %w", err)
	}

	row, err := r.pg.Queries(ctx).GetAuthUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredUser{}, ErrAuthUserNotFound
		}
		return StoredUser{}, fmt.Errorf("get user by id: %w", err)
	}
	return mapUserRow(row), nil
}

func (r *sqlcUserRepository) UpdatePasswordHash(ctx context.Context, userID, newHash string) error {
	id, err := parseUserID(userID)
	if err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}

	// The query is :one ... RETURNING id, so a missing user yields ErrNoRows
	// rather than a silent no-op. This preserves the not-found semantics the
	// prior raw-SQL implementation relied on for goAuth's password-reset path.
	if _, err := r.pg.Queries(ctx).UpdateAuthUserPasswordHash(ctx, sqlcgen.UpdateAuthUserPasswordHashParams{
		ID:           id,
		PasswordHash: newHash,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAuthUserNotFound
		}
		return fmt.Errorf("update password hash: %w", err)
	}
	return nil
}

func (r *sqlcUserRepository) Create(ctx context.Context, input CreateStoredUserInput) (StoredUser, error) {
	row, err := r.pg.Queries(ctx).CreateAuthUser(ctx, sqlcgen.CreateAuthUserParams{
		Email:        strings.TrimSpace(input.Identifier),
		PasswordHash: input.PasswordHash,
		Role:         roleToText(input.Role),
		Permissions:  0,
		Status:       strings.TrimSpace(input.Status),
	})
	if err != nil {
		return StoredUser{}, fmt.Errorf("create user: %w", err)
	}
	return mapUserRow(row), nil
}

func (r *sqlcUserRepository) UpdateStatus(ctx context.Context, userID string, status string) (StoredUser, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return StoredUser{}, fmt.Errorf("update account status: %w", err)
	}

	row, err := r.pg.Queries(ctx).UpdateAuthUserStatus(ctx, sqlcgen.UpdateAuthUserStatusParams{
		ID:     id,
		Status: strings.TrimSpace(status),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredUser{}, ErrAuthUserNotFound
		}
		return StoredUser{}, fmt.Errorf("update account status: %w", err)
	}
	return mapUserRow(row), nil
}

// --- mapping helpers ---

// mapUserRow projects a generated sqlc User row onto the storage-layer model,
// mirroring the COALESCE(role, empty) / id::text behavior of the prior raw SQL.
func mapUserRow(row sqlcgen.User) StoredUser {
	return StoredUser{
		ID:           uuidToString(row.ID),
		Email:        row.Email,
		PasswordHash: row.PasswordHash,
		Role:         textToRole(row.Role),
		Status:       row.Status,
	}
}

// parseUserID converts a canonical string user id into a pgtype.UUID. An empty
// or malformed id is treated as a not-found user, matching the prior behavior
// where such ids simply failed to match any row.
func parseUserID(userID string) (pgtype.UUID, error) {
	var id pgtype.UUID
	trimmed := strings.TrimSpace(userID)
	if trimmed == "" {
		return pgtype.UUID{}, ErrAuthUserNotFound
	}
	if err := id.Scan(trimmed); err != nil {
		return pgtype.UUID{}, ErrAuthUserNotFound
	}
	return id, nil
}

func uuidToString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	// pgtype.UUID.Value returns the canonical 8-4-4-4-12 string form.
	v, err := id.Value()
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// roleToText maps an empty role to a NULL text column (matching the old
// NULLIF on an empty string insert) and a non-empty role to a valid text value.
func roleToText(role string) pgtype.Text {
	trimmed := strings.TrimSpace(role)
	if trimmed == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: trimmed, Valid: true}
}

func textToRole(role pgtype.Text) string {
	if !role.Valid {
		return ""
	}
	return role.String
}
