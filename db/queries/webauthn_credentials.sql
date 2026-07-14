-- name: ListWebAuthnCredentialsByUser :many
SELECT credential_id, user_id, public_key, attestation_type, transports,
       user_present, user_verified, backup_eligible, backup_state, aaguid,
       sign_count, attachment, created_at, last_used_at
FROM webauthn_credentials
WHERE user_id = $1
ORDER BY created_at ASC;

-- name: CreateWebAuthnCredential :exec
INSERT INTO webauthn_credentials (
    credential_id, user_id, public_key, attestation_type, transports,
    user_present, user_verified, backup_eligible, backup_state, aaguid,
    sign_count, attachment, created_at, last_used_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
);

-- name: UpdateWebAuthnCredentialSignCount :one
UPDATE webauthn_credentials
SET sign_count = $2, last_used_at = NOW()
WHERE credential_id = $1
RETURNING credential_id;

-- name: DeleteWebAuthnCredential :one
DELETE FROM webauthn_credentials
WHERE user_id = $1 AND credential_id = $2
RETURNING credential_id;
