-- Always applied by `make migrate-up`; inert until WEBAUTHN_ENABLED=true.
--
-- Stores WebAuthn credentials for goAuth's WebAuthnCredentialProvider. Nothing
-- reads or writes this table while WebAuthn is disabled. See
-- docs/enabling-webauthn.md.

CREATE TABLE IF NOT EXISTS webauthn_credentials (
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

CREATE INDEX IF NOT EXISTS webauthn_credentials_user_id_idx ON webauthn_credentials (user_id);
