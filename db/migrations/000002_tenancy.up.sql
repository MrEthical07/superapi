-- Multi-tenancy: the tenants directory and the tenant column on users.
-- Inert until TENANCY_ENABLED=true. If you do not need tenancy, delete this
-- file and its .down.sql together with internal/tenancy (make init
-- --no-tenancy does exactly that). Like 000001 it is yours to edit until your
-- first deployment.
--
-- users.tenant_id is what goAuth's tenant-scoped lookup
-- (TenantAwareUserProvider) filters on. Rows land in goAuth's default tenant
-- "0", which is also the tenant goAuth uses when a request carries none, so a
-- single-tenant deployment is unaffected.
--
-- Email uniqueness stays GLOBAL: users_email_lower_unique_idx (000001) allows
-- one account per address across all tenants, matching goAuth's default
-- Account.AllowDuplicateIdentifierAcrossTenants=false. To allow the same
-- address in different tenants, replace that index with a unique index on
-- (tenant_id, lower(email)) and enable the goAuth flag; see
-- docs/multi-tenancy.md. The (tenant_id, lower(email)) index below serves
-- tenant-scoped lookups, which filter on lower(email).
--
-- No foreign key from users.tenant_id to tenants(id): the default tenant "0"
-- has no tenants row and single-tenant deployments never create one. Tenant
-- existence is validated at the HTTP edge (TENANCY_VALIDATE) instead.

CREATE TABLE tenants (
    id         TEXT PRIMARY KEY,
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX tenants_created_at_idx ON tenants (created_at);

ALTER TABLE users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '0';

CREATE INDEX users_tenant_email_lower_idx ON users (tenant_id, lower(email));
