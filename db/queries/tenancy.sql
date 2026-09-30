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
