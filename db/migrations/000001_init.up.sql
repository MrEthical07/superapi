-- SuperAPI baseline schema: accounts and second factors for goAuth.
--
-- This file and the optional second migration are yours to edit freely until
-- your first deployment (reshape users, add columns, delete what you do not
-- need). After the first deployment migrations are append-only: add a new
-- numbered file with `make migrate-create NAME=...`. See docs/workflows.md.
--
-- users
--   email            login identifier. The application lower-cases it
--                    (auth.NormalizeIdentifier) and the CHECK below makes the
--                    database agree, so the unique index on lower(email)
--                    enforces one account per address.
--   account_version  goAuth requires it to advance on every account-status
--                    transition (email verification, disable/lock) and on TOTP
--                    enable/disable, and stamps it into sessions for
--                    revocation.
--   totp_enabled     denormalized flag goAuth reads on every login to decide
--                    whether a second factor is required.

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL,
    password_hash   TEXT NOT NULL,
    role            TEXT,
    permissions     BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    account_version INTEGER NOT NULL DEFAULT 1,
    totp_enabled    BOOLEAN NOT NULL DEFAULT FALSE,
    CONSTRAINT users_email_lowercase_check CHECK (email = lower(email))
);

CREATE UNIQUE INDEX users_email_lower_unique_idx ON users (lower(email));

CREATE INDEX users_status_idx ON users (status);

CREATE INDEX users_created_at_idx ON users (created_at);

-- template:begin totp
-- Inert until AUTH_TOTP_ENABLED=true.
--
-- user_totp: the TOTP secret, encrypted at rest by the application
--   (AES-256-GCM, AUTH_TOTP_ENCRYPTION_KEY) because goAuth hands providers the
--   raw secret. last_used_counter backs replay protection.
-- user_backup_codes: SHA-256 hashes of backup codes; used_at marks single use.
--   The unique constraint holds because ReplaceBackupCodes deletes the old set
--   and inserts the new one as two statements in one transaction.

CREATE TABLE user_totp (
    user_id           UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret_ciphertext BYTEA NOT NULL,
    verified          BOOLEAN NOT NULL DEFAULT FALSE,
    last_used_counter BIGINT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE user_backup_codes (
    id         BIGSERIAL PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  BYTEA NOT NULL CHECK (octet_length(code_hash) = 32),
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_backup_codes_user_code_unique UNIQUE (user_id, code_hash)
);
-- template:end totp

-- template:begin webauthn
-- Inert until WEBAUTHN_ENABLED=true. Stores credentials for goAuth's
-- WebAuthnCredentialProvider; nothing reads or writes it while WebAuthn is
-- disabled. See docs/enabling-webauthn.md.

CREATE TABLE webauthn_credentials (
    credential_id    BYTEA PRIMARY KEY,
    user_id          UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    public_key       BYTEA NOT NULL,
    attestation_type TEXT NOT NULL DEFAULT '',
    transports       TEXT[] NOT NULL DEFAULT '{}',
    user_present     BOOLEAN NOT NULL DEFAULT FALSE,
    user_verified    BOOLEAN NOT NULL DEFAULT FALSE,
    backup_eligible  BOOLEAN NOT NULL DEFAULT FALSE,
    backup_state     BOOLEAN NOT NULL DEFAULT FALSE,
    aaguid           BYTEA NOT NULL DEFAULT '\x',
    sign_count       BIGINT NOT NULL DEFAULT 0,
    attachment       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX webauthn_credentials_user_id_idx ON webauthn_credentials (user_id);
-- template:end webauthn
