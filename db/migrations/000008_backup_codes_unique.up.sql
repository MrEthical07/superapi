-- Always applied by `make migrate-up`; inert until AUTH_TOTP_ENABLED=true.
--
-- Migration 000006 created user_backup_codes without a unique index because
-- ReplaceBackupCodes deleted the old set and inserted the new one in a single
-- statement, and Postgres checks uniqueness against rows that the same
-- statement deletes. ReplaceBackupCodes is now two statements (delete, then
-- insert) that the goAuth provider runs in one transaction, so the constraint
-- can come back: a backup code can appear at most once per user.
--
-- Existing duplicates are collapsed first, keeping one row per
-- (user_id, code_hash): an unused row if there is one (so a still-valid code is
-- never turned into a spent one), otherwise the lowest id. Codes are random
-- 256-bit hashes, so duplicates should not exist in practice.
--
-- The unique constraint's index covers (user_id, ...) lookups, so the old
-- single-column index is dropped.

DELETE FROM user_backup_codes b
USING (
    SELECT id,
           row_number() OVER (
               PARTITION BY user_id, code_hash
               ORDER BY (used_at IS NULL) DESC, id
           ) AS keep_rank
    FROM user_backup_codes
) ranked
WHERE b.id = ranked.id AND ranked.keep_rank > 1;

ALTER TABLE user_backup_codes
    ADD CONSTRAINT user_backup_codes_user_code_unique UNIQUE (user_id, code_hash);

DROP INDEX IF EXISTS user_backup_codes_user_id_idx;
