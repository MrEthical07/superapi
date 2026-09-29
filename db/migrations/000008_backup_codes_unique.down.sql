-- Restores the 000006 shape. Rows removed by the up migration's de-duplication
-- are not recoverable (they were duplicates).
CREATE INDEX IF NOT EXISTS user_backup_codes_user_id_idx ON user_backup_codes (user_id);

ALTER TABLE user_backup_codes DROP CONSTRAINT IF EXISTS user_backup_codes_user_code_unique;
