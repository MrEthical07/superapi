-- name: CreateAuthUser :one
INSERT INTO users (email, password_hash, role, permissions, status, tenant_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, email, password_hash, role, permissions, status, created_at, updated_at, tenant_id, account_version, totp_enabled;

-- name: GetAuthUserByID :one
SELECT id, email, password_hash, role, permissions, status, created_at, updated_at, tenant_id, account_version, totp_enabled
FROM users
WHERE id = $1;

-- name: GetAuthUserByLogin :one
SELECT id, email, password_hash, role, permissions, status, created_at, updated_at, tenant_id, account_version, totp_enabled
FROM users
WHERE email = $1;

-- name: GetAuthUserByIDInTenant :one
-- Tenant-scoped lookup for goauth.TenantAwareUserProvider. The tenant
-- predicate is enforced in SQL; a user in another tenant is not found.
SELECT id, email, password_hash, role, permissions, status, created_at, updated_at, tenant_id, account_version, totp_enabled
FROM users
WHERE tenant_id = $1 AND id = $2;

-- name: GetAuthUserByLoginInTenant :one
-- Tenant-scoped lookup for goauth.TenantAwareUserProvider. The tenant
-- predicate is enforced in SQL; an identifier in another tenant is not found.
SELECT id, email, password_hash, role, permissions, status, created_at, updated_at, tenant_id, account_version, totp_enabled
FROM users
WHERE tenant_id = $1 AND email = $2;

-- name: UpdateAuthUserPasswordHash :one
UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1
RETURNING id;

-- name: UpdateAuthUserStatus :one
-- Keyed by the globally unique user id. goAuth resolves the user through the
-- tenant-scoped lookup before any status transition, and email-verification
-- confirm deliberately runs under the challenge's tenant rather than the
-- request's, so this update must not re-scope by the request tenant.
-- goAuth requires account_version to advance on every status transition.
UPDATE users SET status = $2, account_version = account_version + 1, updated_at = NOW() WHERE id = $1
RETURNING id, email, password_hash, role, permissions, status, created_at, updated_at, tenant_id, account_version, totp_enabled;
