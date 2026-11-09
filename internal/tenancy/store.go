package tenancy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MrEthical07/superapi/internal/core/auth"
	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
	"github.com/MrEthical07/superapi/internal/core/storage"
)

// TenantUser is a stored user together with the tenant it belongs to.
type TenantUser struct {
	auth.StoredUser
	TenantID string
}

// UserStore is the tenant-aware user persistence the feature adds on top of the
// core auth repository. Its relational implementation runs the sqlc queries in
// db/queries/tenancy.sql; tests use an in-memory one (tenancytest).
//
// The *InTenant methods constrain the query to one tenant and return
// auth.ErrAuthUserNotFound for a record that exists only in another tenant; an
// empty tenant is never treated as "any tenant".
type UserStore interface {
	// GetByIdentifier and GetByID are tenant-blind lookups that report the
	// tenant the user belongs to.
	GetByIdentifier(ctx context.Context, identifier string) (TenantUser, error)
	GetByID(ctx context.Context, userID string) (TenantUser, error)
	GetByIdentifierInTenant(ctx context.Context, tenantID, identifier string) (TenantUser, error)
	GetByIDInTenant(ctx context.Context, tenantID, userID string) (TenantUser, error)
	// CreateInTenant inserts the user into the tenant. It returns
	// auth.ErrAuthUserExists when the identifier is already taken.
	CreateInTenant(ctx context.Context, tenantID string, input auth.CreateStoredUserInput) (TenantUser, error)
	// TenantOf returns the tenant the user belongs to, or
	// auth.ErrAuthUserNotFound.
	TenantOf(ctx context.Context, userID string) (string, error)
	// UpdatePasswordHashInTenant stores a new password hash for a user of the
	// tenant. A user that exists only in another tenant, or an empty tenant,
	// writes nothing and yields auth.ErrAuthUserNotFound.
	UpdatePasswordHashInTenant(ctx context.Context, tenantID, userID, newHash string) error
	// CompareAndSwapPasswordHashInTenant stores newHash only while the stored
	// hash is expectedOldHash and the user belongs to the tenant. A changed
	// hash, a user of another tenant, an empty tenant, an unknown user and a
	// malformed id all write nothing and yield (false, nil).
	CompareAndSwapPasswordHashInTenant(ctx context.Context, tenantID, userID, expectedOldHash, newHash string) (applied bool, err error)
	// UpdateStatusIfInTenant moves the account from expected to next only while
	// it is in expected and belongs to the tenant, advancing the account
	// version like a plain status update. Anything else is (false, nil).
	UpdateStatusIfInTenant(ctx context.Context, tenantID, userID, expected, next string) (applied bool, err error)
}

// pgUniqueViolation is the Postgres SQLSTATE for unique_violation.
const pgUniqueViolation = "23505"

type sqlcUserStore struct {
	pg *storage.Postgres
}

// NewUserStore returns the relational UserStore over the storage boundary, or
// nil when pg is nil.
func NewUserStore(pg *storage.Postgres) UserStore {
	if pg == nil {
		return nil
	}
	return &sqlcUserStore{pg: pg}
}

func (s *sqlcUserStore) GetByIdentifier(ctx context.Context, identifier string) (TenantUser, error) {
	row, err := s.pg.Queries(ctx).GetAuthUserByLogin(ctx, auth.NormalizeIdentifier(identifier))
	if err != nil {
		return TenantUser{}, notFoundOr(err, "get user by identifier")
	}
	return mapUser(row), nil
}

func (s *sqlcUserStore) GetByID(ctx context.Context, userID string) (TenantUser, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return TenantUser{}, err
	}
	row, err := s.pg.Queries(ctx).GetAuthUserByID(ctx, id)
	if err != nil {
		return TenantUser{}, notFoundOr(err, "get user by id")
	}
	return mapUser(row), nil
}

func (s *sqlcUserStore) GetByIdentifierInTenant(ctx context.Context, tenantID, identifier string) (TenantUser, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return TenantUser{}, auth.ErrAuthUserNotFound
	}
	row, err := s.pg.Queries(ctx).GetAuthUserByLoginInTenant(ctx, sqlcgen.GetAuthUserByLoginInTenantParams{
		TenantID: tenantID,
		Email:    auth.NormalizeIdentifier(identifier),
	})
	if err != nil {
		return TenantUser{}, notFoundOr(err, "get user by identifier in tenant")
	}
	return mapUser(row), nil
}

func (s *sqlcUserStore) GetByIDInTenant(ctx context.Context, tenantID, userID string) (TenantUser, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return TenantUser{}, auth.ErrAuthUserNotFound
	}
	id, err := parseUserID(userID)
	if err != nil {
		return TenantUser{}, err
	}
	row, err := s.pg.Queries(ctx).GetAuthUserByIDInTenant(ctx, sqlcgen.GetAuthUserByIDInTenantParams{TenantID: tenantID, ID: id})
	if err != nil {
		return TenantUser{}, notFoundOr(err, "get user by id in tenant")
	}
	return mapUser(row), nil
}

func (s *sqlcUserStore) CreateInTenant(ctx context.Context, tenantID string, input auth.CreateStoredUserInput) (TenantUser, error) {
	row, err := s.pg.Queries(ctx).CreateAuthUserInTenant(ctx, sqlcgen.CreateAuthUserInTenantParams{
		Email:        auth.NormalizeIdentifier(input.Identifier),
		PasswordHash: input.PasswordHash,
		Role:         optionalText(input.Role),
		Permissions:  0,
		Status:       strings.TrimSpace(input.Status),
		TenantID:     tenantOrDefault(tenantID),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return TenantUser{}, auth.ErrAuthUserExists
		}
		return TenantUser{}, fmt.Errorf("create user in tenant: %w", err)
	}
	return mapUser(row), nil
}

func (s *sqlcUserStore) TenantOf(ctx context.Context, userID string) (string, error) {
	id, err := parseUserID(userID)
	if err != nil {
		return "", err
	}
	tenantID, err := s.pg.Queries(ctx).GetAuthUserTenant(ctx, id)
	if err != nil {
		return "", notFoundOr(err, "get user tenant")
	}
	return tenantID, nil
}

func (s *sqlcUserStore) UpdatePasswordHashInTenant(ctx context.Context, tenantID, userID, newHash string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return auth.ErrAuthUserNotFound
	}
	id, err := parseUserID(userID)
	if err != nil {
		return err
	}
	// :one ... RETURNING id, so a user outside the tenant yields ErrNoRows
	// instead of a silent no-op.
	if _, err := s.pg.Queries(ctx).UpdateAuthUserPasswordHashInTenant(ctx, sqlcgen.UpdateAuthUserPasswordHashInTenantParams{
		ID:           id,
		TenantID:     tenantID,
		PasswordHash: newHash,
	}); err != nil {
		return notFoundOr(err, "update password hash in tenant")
	}
	return nil
}

func (s *sqlcUserStore) CompareAndSwapPasswordHashInTenant(ctx context.Context, tenantID, userID, expectedOldHash, newHash string) (bool, error) {
	tenantID = strings.TrimSpace(tenantID)
	id, err := parseUserID(userID)
	if tenantID == "" || err != nil {
		return false, nil
	}
	n, err := s.pg.Queries(ctx).CompareAndSwapAuthUserPasswordHashInTenant(ctx, sqlcgen.CompareAndSwapAuthUserPasswordHashInTenantParams{
		ID:           id,
		TenantID:     tenantID,
		ExpectedHash: expectedOldHash,
		NewHash:      newHash,
	})
	if err != nil {
		return false, fmt.Errorf("compare and swap password hash in tenant: %w", err)
	}
	return n == 1, nil
}

func (s *sqlcUserStore) UpdateStatusIfInTenant(ctx context.Context, tenantID, userID, expected, next string) (bool, error) {
	tenantID = strings.TrimSpace(tenantID)
	id, err := parseUserID(userID)
	if tenantID == "" || err != nil {
		return false, nil
	}
	n, err := s.pg.Queries(ctx).UpdateAuthUserStatusIfInTenant(ctx, sqlcgen.UpdateAuthUserStatusIfInTenantParams{
		ID:             id,
		TenantID:       tenantID,
		ExpectedStatus: strings.TrimSpace(expected),
		NextStatus:     strings.TrimSpace(next),
	})
	if err != nil {
		return false, fmt.Errorf("conditional account status update in tenant: %w", err)
	}
	return n == 1, nil
}

// --- mapping helpers (sqlc row -> domain) ---

func mapUser(row sqlcgen.User) TenantUser {
	role := ""
	if row.Role.Valid {
		role = row.Role.String
	}
	version := uint32(0)
	if row.AccountVersion > 0 {
		version = uint32(row.AccountVersion)
	}
	return TenantUser{
		StoredUser: auth.StoredUser{
			ID:             uuidToString(row.ID),
			Email:          row.Email,
			PasswordHash:   row.PasswordHash,
			Role:           role,
			Status:         row.Status,
			AccountVersion: version,
			TOTPEnabled:    row.TotpEnabled,
		},
		TenantID: row.TenantID,
	}
}

func notFoundOr(err error, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrAuthUserNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}

// parseUserID converts a canonical string user id into a pgtype.UUID. An empty
// or malformed id is treated as a not-found user.
func parseUserID(userID string) (pgtype.UUID, error) {
	var id pgtype.UUID
	trimmed := strings.TrimSpace(userID)
	if trimmed == "" {
		return pgtype.UUID{}, auth.ErrAuthUserNotFound
	}
	if err := id.Scan(trimmed); err != nil {
		return pgtype.UUID{}, auth.ErrAuthUserNotFound
	}
	return id, nil
}

func uuidToString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	v, err := id.Value()
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func optionalText(value string) pgtype.Text {
	trimmed := strings.TrimSpace(value)
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
