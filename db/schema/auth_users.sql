-- Hand-authored mirror of migrations 000003_auth_users, 000005_users_tenant
-- and the users columns from 000006_auth_mfa.

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT,
    permissions BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 000005_users_tenant: goAuth tenant id; "0" is goAuth's default tenant.
    tenant_id TEXT NOT NULL DEFAULT '0',
    -- 000006_auth_mfa: advanced on every status/TOTP transition (goAuth).
    account_version INTEGER NOT NULL DEFAULT 1,
    -- 000006_auth_mfa: whether login requires a TOTP second factor.
    totp_enabled BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique_idx ON users (email);

CREATE INDEX IF NOT EXISTS users_status_idx ON users (status);

CREATE INDEX IF NOT EXISTS users_created_at_idx ON users (created_at);

CREATE INDEX IF NOT EXISTS users_tenant_email_idx ON users (tenant_id, email);
