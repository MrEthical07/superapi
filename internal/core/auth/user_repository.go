package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/storage"
	"github.com/jackc/pgx/v5"
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

type relationalUserRepository struct {
	store storage.RelationalStore
}

// NewRelationalUserRepository creates an auth repository over a relational store.
func NewRelationalUserRepository(store storage.RelationalStore) UserRepository {
	if store == nil {
		return nil
	}
	return &relationalUserRepository{store: store}
}

const (
	queryAuthUserByIdentifier = `
SELECT
	id::text,
	email,
	password_hash,
	COALESCE(role, ''),
	status
FROM users
WHERE email = $1
`

	queryAuthUserByID = `
SELECT
	id::text,
	email,
	password_hash,
	COALESCE(role, ''),
	status
FROM users
WHERE id = $1::uuid
`

	queryUpdatePasswordHash = `
UPDATE users
SET password_hash = $2, updated_at = NOW()
WHERE id = $1::uuid
RETURNING id::text
`

	queryCreateAuthUser = `
INSERT INTO users (email, password_hash, role, permissions, status)
VALUES ($1, $2, NULLIF($3, ''), 0, $4)
RETURNING
	id::text,
	email,
	password_hash,
	COALESCE(role, ''),
	status
`

	queryUpdateStatus = `
UPDATE users
SET status = $2, updated_at = NOW()
WHERE id = $1::uuid
RETURNING
	id::text,
	email,
	password_hash,
	COALESCE(role, ''),
	status
`
)

func (r *relationalUserRepository) GetByIdentifier(ctx context.Context, identifier string) (StoredUser, error) {
	var user StoredUser
	err := r.store.Execute(ctx, storage.RelationalQueryOne(queryAuthUserByIdentifier,
		func(row storage.RowScanner) error {
			return row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Status)
		},
		strings.TrimSpace(identifier),
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredUser{}, ErrAuthUserNotFound
		}
		return StoredUser{}, fmt.Errorf("get user by identifier: %w", err)
	}
	return user, nil
}

func (r *relationalUserRepository) GetByID(ctx context.Context, userID string) (StoredUser, error) {
	var user StoredUser
	err := r.store.Execute(ctx, storage.RelationalQueryOne(queryAuthUserByID,
		func(row storage.RowScanner) error {
			return row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Status)
		},
		strings.TrimSpace(userID),
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredUser{}, ErrAuthUserNotFound
		}
		return StoredUser{}, fmt.Errorf("get user by id: %w", err)
	}
	return user, nil
}

func (r *relationalUserRepository) UpdatePasswordHash(ctx context.Context, userID, newHash string) error {
	var touchedID string
	err := r.store.Execute(ctx, storage.RelationalQueryOne(queryUpdatePasswordHash,
		func(row storage.RowScanner) error {
			return row.Scan(&touchedID)
		},
		strings.TrimSpace(userID),
		newHash,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAuthUserNotFound
		}
		return fmt.Errorf("update password hash: %w", err)
	}
	return nil
}

func (r *relationalUserRepository) Create(ctx context.Context, input CreateStoredUserInput) (StoredUser, error) {
	var user StoredUser
	err := r.store.Execute(ctx, storage.RelationalQueryOne(queryCreateAuthUser,
		func(row storage.RowScanner) error {
			return row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Status)
		},
		strings.TrimSpace(input.Identifier),
		input.PasswordHash,
		strings.TrimSpace(input.Role),
		strings.TrimSpace(input.Status),
	))
	if err != nil {
		return StoredUser{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (r *relationalUserRepository) UpdateStatus(ctx context.Context, userID string, status string) (StoredUser, error) {
	var user StoredUser
	err := r.store.Execute(ctx, storage.RelationalQueryOne(queryUpdateStatus,
		func(row storage.RowScanner) error {
			return row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Status)
		},
		strings.TrimSpace(userID),
		strings.TrimSpace(status),
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredUser{}, ErrAuthUserNotFound
		}
		return StoredUser{}, fmt.Errorf("update account status: %w", err)
	}
	return user, nil
}
