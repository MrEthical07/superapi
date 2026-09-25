package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

var ErrAuthUserNotFound = errors.New("auth user not found")

// ErrAuthUserExists is returned by Create when the identifier is already taken
// (unique violation on users.email).
var ErrAuthUserExists = errors.New("auth user already exists")

// pgUniqueViolation is the Postgres SQLSTATE for unique_violation.
const pgUniqueViolation = "23505"

// StoredUser is the storage-layer projection used by the auth repository.
type StoredUser struct {
	ID             string
	TenantID       string
	Email          string
	PasswordHash   string
	Role           string
	Status         string
	AccountVersion uint32
	TOTPEnabled    bool
}

// CreateStoredUserInput is the repository input model for creating auth users.
type CreateStoredUserInput struct {
	// TenantID is the owning tenant. Empty falls back to DefaultTenantID.
	TenantID     string
	Identifier   string
	PasswordHash string
	Role         string
	Status       string
}

// UserRepository defines domain-level auth user persistence operations.
//
// The plain lookups are tenant-blind (goAuth's contract when multi-tenancy is
// off). The *InTenant lookups constrain the query to one tenant in SQL and
// return ErrAuthUserNotFound for a record that exists only in another tenant;
// an empty tenant is never treated as "any tenant".
type UserRepository interface {
	GetByIdentifier(ctx context.Context, identifier string) (StoredUser, error)
	GetByID(ctx context.Context, userID string) (StoredUser, error)
	GetByIdentifierInTenant(ctx context.Context, tenantID, identifier string) (StoredUser, error)
	GetByIDInTenant(ctx context.Context, tenantID, userID string) (StoredUser, error)
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

func (r *sqlcUserRepository) GetByIdentifierInTenant(ctx context.Context, tenantID, identifier string) (StoredUser, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return StoredUser{}, ErrAuthUserNotFound
	}

	row, err := r.pg.Queries(ctx).GetAuthUserByLoginInTenant(ctx, sqlcgen.GetAuthUserByLoginInTenantParams{
		TenantID: tenantID,
		Email:    strings.TrimSpace(identifier),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredUser{}, ErrAuthUserNotFound
		}
		return StoredUser{}, fmt.Errorf("get user by identifier in tenant: %w", err)
	}
	return mapUserRow(row), nil
}

func (r *sqlcUserRepository) GetByIDInTenant(ctx context.Context, tenantID, userID string) (StoredUser, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return StoredUser{}, ErrAuthUserNotFound
	}
	id, err := parseUserID(userID)
	if err != nil {
		return StoredUser{}, fmt.Errorf("get user by id in tenant: %w", err)
	}

	row, err := r.pg.Queries(ctx).GetAuthUserByIDInTenant(ctx, sqlcgen.GetAuthUserByIDInTenantParams{
		TenantID: tenantID,
		ID:       id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredUser{}, ErrAuthUserNotFound
		}
		return StoredUser{}, fmt.Errorf("get user by id in tenant: %w", err)
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
		TenantID:     tenantOrDefault(input.TenantID),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return StoredUser{}, ErrAuthUserExists
		}
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
		ID:             uuidToString(row.ID),
		TenantID:       row.TenantID,
		Email:          row.Email,
		PasswordHash:   row.PasswordHash,
		Role:           textToRole(row.Role),
		Status:         row.Status,
		AccountVersion: nonNegativeUint32(row.AccountVersion),
		TOTPEnabled:    row.TotpEnabled,
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

// tenantOrDefault maps an empty tenant to goAuth's default tenant.
func tenantOrDefault(tenantID string) string {
	if trimmed := strings.TrimSpace(tenantID); trimmed != "" {
		return trimmed
	}
	return DefaultTenantID
}

// optionalText maps an empty string to SQL NULL.
func optionalText(value string) pgtype.Text {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: trimmed, Valid: true}
}

func nonNegativeUint32(v int32) uint32 {
	if v < 0 {
		return 0
	}
	return uint32(v)
}

func textToRole(role pgtype.Text) string {
	if !role.Valid {
		return ""
	}
	return role.String
}
