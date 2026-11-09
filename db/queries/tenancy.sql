-- Tenant directory.

-- name: CreateTenant :one
INSERT INTO tenants (id, slug, name, status)
VALUES ($1, $2, $3, $4)
RETURNING id, slug, name, status, created_at, updated_at;

-- name: GetTenantByID :one
SELECT id, slug, name, status, created_at, updated_at
FROM tenants
WHERE id = $1;

-- name: GetTenantBySlug :one
-- Slugs are stored lower-case (the subdomain resolver lower-cases the host
-- label); callers pass a normalized slug. tenants.slug is UNIQUE.
SELECT id, slug, name, status, created_at, updated_at
FROM tenants
WHERE slug = $1;

-- name: ListTenants :many
SELECT id, slug, name, status, created_at, updated_at
FROM tenants
ORDER BY created_at ASC, id ASC
LIMIT $1;

-- Tenant-scoped user access, used by the tenancy feature's goAuth provider
-- wrapper (goauth.TenantAwareUserProvider). The core auth queries never
-- mention tenant_id; the column's default covers inserts made without it.

-- name: GetAuthUserByIDInTenant :one
-- The tenant predicate is enforced in SQL; a user in another tenant is not
-- found.
SELECT * FROM users
WHERE tenant_id = $1 AND id = $2;

-- name: GetAuthUserByLoginInTenant :one
-- Identifiers are compared case-insensitively, like GetAuthUserByLogin. The
-- tenant predicate is enforced in SQL; an identifier in another tenant is not
-- found.
SELECT * FROM users
WHERE tenant_id = $1 AND lower(email) = lower(sqlc.arg(email));

-- name: CreateAuthUserInTenant :one
INSERT INTO users (email, password_hash, role, permissions, status, tenant_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAuthUserTenant :one
SELECT tenant_id FROM users WHERE id = $1;

-- name: UpdateAuthUserPasswordHashInTenant :one
-- Tenant-scoped password write (goauth.TenantAwarePasswordUpdater). The tenant
-- predicate is enforced in SQL; a user in another tenant matches no row, so
-- the write is a not-found rather than a silent update.
UPDATE users SET password_hash = $3, updated_at = NOW()
WHERE id = $1 AND tenant_id = $2
RETURNING id;

-- name: CompareAndSwapAuthUserPasswordHashInTenant :execrows
-- Tenant-scoped compare-and-swap password write (goauth.PasswordHashCASUpdater).
-- Both the tenant and the expected hash are enforced in SQL: a user of another
-- tenant, or a hash that changed, matches no row and nothing is written.
UPDATE users SET password_hash = sqlc.arg(new_hash), updated_at = NOW()
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id) AND password_hash = sqlc.arg(expected_hash);

-- name: UpdateAuthUserStatusIfInTenant :execrows
-- Tenant-scoped compare-and-swap status transition
-- (goauth.AccountStatusCASUpdater), with the side effects of
-- UpdateAuthUserStatus. A user of another tenant, or a status that differs,
-- matches no row.
UPDATE users SET status = sqlc.arg(next_status), account_version = account_version + 1, updated_at = NOW()
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id) AND status = sqlc.arg(expected_status);
