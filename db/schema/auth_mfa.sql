-- Hand-authored mirror of the tables in migration 000006_auth_mfa.

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
