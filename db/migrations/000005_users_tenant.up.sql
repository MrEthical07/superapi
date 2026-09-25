-- Always applied by `make migrate-up`; inert until TENANCY_ENABLED=true.
--
-- Adds the tenant column goAuth v0.5.0 requires for tenant-scoped user lookup
-- (TenantAwareUserProvider). Existing rows land in goAuth's default tenant "0",
-- which is also the tenant goAuth uses when no tenant is attached to the
-- request context, so single-tenant deployments are unaffected.
--
-- Identifier uniqueness stays GLOBAL (users_email_unique_idx is kept). This
-- matches goAuth's default Account.AllowDuplicateIdentifierAcrossTenants=false.
-- To switch to per-tenant uniqueness see docs/multi-tenancy.md.
--
-- No foreign key to tenants(id): the default tenant "0" has no tenants row and
-- single-tenant deployments never create one. Tenant existence is validated at
-- the HTTP edge (TENANCY_VALIDATE) instead.

ALTER TABLE users ADD COLUMN IF NOT EXISTS tenant_id TEXT NOT NULL DEFAULT '0';

CREATE INDEX IF NOT EXISTS users_tenant_email_idx ON users (tenant_id, email);
