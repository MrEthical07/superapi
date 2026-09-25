-- Always applied by `make migrate-up`; inert until AUTH_TOTP_ENABLED=true
-- (except account_version, which every account-status transition uses).
--
-- users.account_version: goAuth requires the account version to advance on
--   every account-status transition (email verification, disable/lock) and on
--   TOTP enable/disable, and stamps it into sessions for revocation. Existing
--   rows start at 1.
-- users.totp_enabled: denormalized flag goAuth reads on every login to decide
--   whether a second factor is required.
-- user_totp: the TOTP secret, encrypted at rest by the application
--   (AES-256-GCM, AUTH_TOTP_ENCRYPTION_KEY) — goAuth hands providers the raw
--   secret. last_used_counter backs replay protection.
-- user_backup_codes: SHA-256 hashes of backup codes; used_at marks single use.

ALTER TABLE users ADD COLUMN IF NOT EXISTS account_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS user_totp (
    user_id           UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret_ciphertext BYTEA NOT NULL,
    verified          BOOLEAN NOT NULL DEFAULT FALSE,
    last_used_counter BIGINT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_backup_codes (
    id         BIGSERIAL PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  BYTEA NOT NULL CHECK (octet_length(code_hash) = 32),
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Not unique: ReplaceBackupCodes deletes and inserts in one statement, and a
-- unique (user_id, code_hash) index would reject re-inserting a hash that the
-- same statement deletes. Codes are random, so duplicates do not occur.
CREATE INDEX IF NOT EXISTS user_backup_codes_user_id_idx ON user_backup_codes (user_id);
