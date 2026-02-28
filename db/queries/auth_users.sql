-- name: CreateAuthUser :one
INSERT INTO users (email, password_hash, role, permissions, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, email, password_hash, role, permissions, status, created_at, updated_at;

-- name: GetAuthUserByID :one
SELECT id, email, password_hash, role, permissions, status, created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetAuthUserByLogin :one
SELECT id, email, password_hash, role, permissions, status, created_at, updated_at
FROM users
WHERE email = $1;

-- name: UpdateAuthUserPasswordHash :exec
UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1;

-- name: UpdateAuthUserStatus :one
UPDATE users SET status = $2, updated_at = NOW() WHERE id = $1 RETURNING id, email, password_hash, role, permissions, status, created_at, updated_at;

