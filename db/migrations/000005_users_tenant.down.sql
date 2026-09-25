DROP INDEX IF EXISTS users_tenant_email_idx;

ALTER TABLE users DROP COLUMN IF EXISTS tenant_id;
