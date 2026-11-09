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

-- name: CompareAndSwapAuthUserPasswordHash :execrows
-- Compare-and-swap for goAuth's rehash-on-login (goauth.PasswordHashCASUpdater):
-- the hash is replaced only while it is still the one the login verified, so a
-- password change that landed in between is never overwritten. Same side
-- effects as UpdateAuthUserPasswordHash (hash and updated_at, nothing else).
-- The affected-row count is the answer: 1 applied, 0 hash changed or no user.
UPDATE users SET password_hash = sqlc.arg(new_hash), updated_at = NOW()
WHERE id = sqlc.arg(id) AND password_hash = sqlc.arg(expected_hash);

-- name: UpdateAuthUserStatusIf :execrows
-- Compare-and-swap status transition (goauth.AccountStatusCASUpdater): applies
-- only from the expected status. Same side effects as UpdateAuthUserStatus
-- (status, account_version + 1, updated_at). 1 row applied, 0 status differed
-- or no user.
UPDATE users SET status = sqlc.arg(next_status), account_version = account_version + 1, updated_at = NOW()
WHERE id = sqlc.arg(id) AND status = sqlc.arg(expected_status);
