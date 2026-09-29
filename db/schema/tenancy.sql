-- Hand-authored mirror of migration 000002_tenancy: the tenants directory and
-- the tenant column on users. sqlc reads schema files in name order, so this
-- file runs after auth_users.sql, which creates users.

CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'inactive')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS tenants_created_at_idx ON tenants (created_at);

-- goAuth's tenant id; "0" is goAuth's default tenant.
ALTER TABLE users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '0';

CREATE INDEX IF NOT EXISTS users_tenant_email_lower_idx ON users (tenant_id, lower(email));
