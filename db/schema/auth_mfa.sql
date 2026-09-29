-- Hand-authored mirror of the second-factor tables in migration 000001_init.

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
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- A code appears at most once per user.
    CONSTRAINT user_backup_codes_user_code_unique UNIQUE (user_id, code_hash)
);
