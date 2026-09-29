DROP INDEX IF EXISTS users_tenant_email_lower_idx;

ALTER TABLE users DROP COLUMN IF EXISTS tenant_id;

DROP TABLE IF EXISTS tenants;
