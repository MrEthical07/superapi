-- name: CreateAuthUser :one
INSERT INTO users (email, password_hash, role, permissions, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAuthUserByID :one
SELECT * FROM users
WHERE id = $1;

-- name: GetAuthUserByLogin :one
-- Identifiers are compared case-insensitively (lower(email), backed by
-- users_email_lower_unique_idx). Callers pass a NormalizeIdentifier value;
-- lower($1) keeps the match correct for any caller.
SELECT * FROM users
WHERE lower(email) = lower(sqlc.arg(email));

-- name: UpdateAuthUserPasswordHash :one
UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1
RETURNING id;

-- name: UpdateAuthUserStatus :one
-- Keyed by the globally unique user id. goAuth requires account_version to
-- advance on every status transition.
UPDATE users SET status = $2, account_version = account_version + 1, updated_at = NOW() WHERE id = $1
RETURNING *;
