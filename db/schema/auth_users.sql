-- Hand-authored mirror of migration 000001_init (the users table).

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT,
    permissions BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Advanced on every status/TOTP transition (goAuth).
    account_version INTEGER NOT NULL DEFAULT 1,
    -- Whether login requires a TOTP second factor.
    totp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    CONSTRAINT users_email_lowercase_check CHECK (email = lower(email))
);

-- Identifiers are case-insensitive and unique.
CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_unique_idx ON users (lower(email));

CREATE INDEX IF NOT EXISTS users_status_idx ON users (status);

CREATE INDEX IF NOT EXISTS users_created_at_idx ON users (created_at);
