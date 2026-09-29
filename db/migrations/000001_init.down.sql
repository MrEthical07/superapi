-- template:begin webauthn
DROP TABLE IF EXISTS webauthn_credentials;
-- template:end webauthn

-- template:begin totp
DROP TABLE IF EXISTS user_backup_codes;
DROP TABLE IF EXISTS user_totp;
-- template:end totp

DROP TABLE IF EXISTS users;
