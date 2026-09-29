-- Restores the previous indexes. Emails stay lower-cased: the original casing
-- is not recoverable, and lower-case values are valid under the old schema.
DROP INDEX IF EXISTS users_tenant_email_lower_idx;
CREATE INDEX IF NOT EXISTS users_tenant_email_idx ON users (tenant_id, email);

DROP INDEX IF EXISTS users_email_lower_unique_idx;
