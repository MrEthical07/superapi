-- TOTP and backup-code persistence for goAuth's UserProvider MFA methods.
--
-- Every query takes an optional tenant_id. NULL (tenancy disabled) scopes by
-- user id only; a value restricts the operation to users in that tenant, so a
-- user id from another tenant matches nothing. Each operation is a single
-- statement, so it is atomic without an explicit transaction, except
-- ReplaceBackupCodes, which is a DeleteBackupCodes + InsertBackupCodes pair
-- that the caller must run inside one transaction (the repository refuses to
-- run outside one).

-- name: GetUserTOTP :one
SELECT t.secret_ciphertext, t.verified, t.last_used_counter, u.totp_enabled
FROM user_totp t
JOIN users u ON u.id = t.user_id
WHERE t.user_id = sqlc.arg(user_id)
  AND (sqlc.narg(tenant_id)::text IS NULL OR u.tenant_id = sqlc.narg(tenant_id)::text);

-- name: UpsertUserTOTPSecret :one
-- Stores the (encrypted) secret and syncs users.totp_enabled to the verified
-- flag, advancing account_version when the enabled state flips. Mirrors
-- goAuth's EnableTOTP contract: setup stores an unverified secret (TOTP stays
-- off); after MarkUserTOTPVerified the same call turns TOTP on.
WITH target AS (
    SELECT users.id AS target_id FROM users
    WHERE users.id = sqlc.arg(user_id)
      AND (sqlc.narg(tenant_id)::text IS NULL OR users.tenant_id = sqlc.narg(tenant_id)::text)
), upsert AS (
    INSERT INTO user_totp (user_id, secret_ciphertext)
    SELECT target_id, sqlc.arg(secret_ciphertext)::bytea FROM target
    ON CONFLICT (user_id) DO UPDATE
        SET secret_ciphertext = EXCLUDED.secret_ciphertext, updated_at = NOW()
    RETURNING user_id, verified
)
UPDATE users u
SET totp_enabled = upsert.verified,
    account_version = u.account_version + CASE WHEN u.totp_enabled <> upsert.verified THEN 1 ELSE 0 END,
    updated_at = NOW()
FROM upsert
WHERE u.id = upsert.user_id
RETURNING u.id;

-- name: MarkUserTOTPVerified :one
UPDATE user_totp t
SET verified = TRUE, updated_at = NOW()
FROM users u
WHERE u.id = t.user_id
  AND t.user_id = sqlc.arg(user_id)
  AND (sqlc.narg(tenant_id)::text IS NULL OR u.tenant_id = sqlc.narg(tenant_id)::text)
RETURNING t.user_id;

-- name: AdvanceUserTOTPCounter :one
-- Only moves the counter forward, so two concurrent verifications of the same
-- code cannot both succeed (replay protection holds under races).
UPDATE user_totp t
SET last_used_counter = sqlc.arg(counter), updated_at = NOW()
FROM users u
WHERE u.id = t.user_id
  AND t.user_id = sqlc.arg(user_id)
  AND t.last_used_counter < sqlc.arg(counter)
  AND (sqlc.narg(tenant_id)::text IS NULL OR u.tenant_id = sqlc.narg(tenant_id)::text)
RETURNING t.user_id;

-- name: DisableUserTOTP :one
-- Removes the TOTP secret and all backup codes, clears users.totp_enabled and
-- advances account_version when TOTP was enabled.
WITH target AS (
    SELECT users.id AS target_id FROM users
    WHERE users.id = sqlc.arg(user_id)
      AND (sqlc.narg(tenant_id)::text IS NULL OR users.tenant_id = sqlc.narg(tenant_id)::text)
), deleted_totp AS (
    DELETE FROM user_totp WHERE user_id IN (SELECT target_id FROM target)
), deleted_codes AS (
    DELETE FROM user_backup_codes WHERE user_id IN (SELECT target_id FROM target)
)
UPDATE users u
SET totp_enabled = FALSE,
    account_version = u.account_version + CASE WHEN u.totp_enabled THEN 1 ELSE 0 END,
    updated_at = NOW()
FROM target
WHERE u.id = target.target_id
RETURNING u.id;

-- name: ListUnusedBackupCodes :many
SELECT b.code_hash
FROM user_backup_codes b
JOIN users u ON u.id = b.user_id
WHERE b.user_id = sqlc.arg(user_id)
  AND b.used_at IS NULL
  AND (sqlc.narg(tenant_id)::text IS NULL OR u.tenant_id = sqlc.narg(tenant_id)::text)
ORDER BY b.id;

-- name: DeleteBackupCodes :execrows
-- First half of replacing a user's backup codes. Run it and InsertBackupCodes
-- in one transaction: a single statement cannot do both because the unique
-- (user_id, code_hash) constraint rejects re-inserting a hash the same
-- statement deletes.
DELETE FROM user_backup_codes
WHERE user_id IN (
    SELECT users.id FROM users
    WHERE users.id = sqlc.arg(user_id)
      AND (sqlc.narg(tenant_id)::text IS NULL OR users.tenant_id = sqlc.narg(tenant_id)::text)
);

-- name: InsertBackupCodes :execrows
-- Second half of replacing a user's backup codes. Inserts nothing when the user
-- is not in scope (unknown id, or another tenant).
INSERT INTO user_backup_codes (user_id, code_hash)
SELECT users.id, code_hash
FROM users, unnest(sqlc.arg(code_hashes)::bytea[]) AS code_hash
WHERE users.id = sqlc.arg(user_id)
  AND (sqlc.narg(tenant_id)::text IS NULL OR users.tenant_id = sqlc.narg(tenant_id)::text);

-- name: ConsumeBackupCode :execrows
-- Atomic single use: the row lock plus the used_at IS NULL predicate mean two
-- concurrent consumes of the same code cannot both report success.
UPDATE user_backup_codes b
SET used_at = NOW()
FROM users u
WHERE u.id = b.user_id
  AND b.user_id = sqlc.arg(user_id)
  AND b.code_hash = sqlc.arg(code_hash)
  AND b.used_at IS NULL
  AND (sqlc.narg(tenant_id)::text IS NULL OR u.tenant_id = sqlc.narg(tenant_id)::text);

-- name: RotateUserTOTPSecret :execrows
-- Compare-and-swap for key rotation: replaces the stored ciphertext only if it
-- still equals prev_ciphertext, so a concurrent enable/disable/re-enroll is
-- never overwritten with a stale re-encryption. Touches no other state (the
-- verified flag, users.totp_enabled and account_version are unchanged).
UPDATE user_totp t
SET secret_ciphertext = sqlc.arg(next_ciphertext)::bytea, updated_at = NOW()
FROM users u
WHERE u.id = t.user_id
  AND t.user_id = sqlc.arg(user_id)
  AND t.secret_ciphertext = sqlc.arg(prev_ciphertext)::bytea
  AND (sqlc.narg(tenant_id)::text IS NULL OR u.tenant_id = sqlc.narg(tenant_id)::text);

-- name: ListUserTOTPSecrets :many
-- Keyset-paginated scan of every stored ciphertext, across tenants, for the
-- rotatetotpkey command. Pass a NULL after_user_id for the first page.
SELECT t.user_id, t.secret_ciphertext
FROM user_totp t
WHERE sqlc.narg(after_user_id)::uuid IS NULL OR t.user_id > sqlc.narg(after_user_id)::uuid
ORDER BY t.user_id
LIMIT sqlc.arg(row_limit);
