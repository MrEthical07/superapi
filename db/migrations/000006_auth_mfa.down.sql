DROP TABLE IF EXISTS user_backup_codes;
DROP TABLE IF EXISTS user_totp;

ALTER TABLE users DROP COLUMN IF EXISTS totp_enabled;
ALTER TABLE users DROP COLUMN IF EXISTS account_version;
