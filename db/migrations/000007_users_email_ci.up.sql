-- Always applied by `make migrate-up`.
--
-- Makes login identifiers (emails) case-insensitive. Until now
-- "Alice@Example.com" and "alice@example.com" could be registered as two
-- accounts. The application now lower-cases every identifier before it reaches
-- goAuth (auth.NormalizeIdentifier); this migration makes the database agree.
--
-- 1. Refuse to run if two existing rows differ only by case. Which of them
--    should survive is a decision for a human (the accounts may belong to
--    different people, or hold different data), so nothing is merged or
--    deleted automatically. The error lists the colliding addresses. To
--    resolve one group, for example:
--        -- keep the account you want, then either delete the other
--        DELETE FROM users WHERE id = '<duplicate id>';
--        -- or give it a different, unique identifier
--        UPDATE users SET email = 'alice+old@example.com' WHERE id = '<duplicate id>';
--    then clear the dirty flag the failed run leaves and run it again:
--        POSTGRES_ENABLED=true go run ./cmd/migrate force --version=6
--        make migrate-up
--    The migration is one transaction, so a failure changes no data.
-- 2. Lower-case the stored emails.
-- 3. Add a unique index on lower(email). Uniqueness stays GLOBAL, matching
--    goAuth's default Account.AllowDuplicateIdentifierAcrossTenants=false. For
--    per-tenant uniqueness see docs/multi-tenancy.md.
-- 4. Replace the (tenant_id, email) lookup index with (tenant_id, lower(email))
--    because tenant-scoped lookups now filter on lower(email).

DO $$
DECLARE
    dup RECORD;
    report TEXT := '';
    groups INTEGER := 0;
BEGIN
    FOR dup IN
        SELECT lower(email) AS normalized,
               count(*) AS accounts,
               string_agg(email || ' (id ' || id::text || ', tenant ' || tenant_id || ')', '; ' ORDER BY created_at, id) AS members
        FROM users
        GROUP BY lower(email)
        HAVING count(*) > 1
        ORDER BY lower(email)
        LIMIT 20
    LOOP
        groups := groups + 1;
        report := report || E'\n  ' || dup.normalized || ' -> ' || dup.members;
    END LOOP;

    IF groups > 0 THEN
        RAISE EXCEPTION 'migration 000007_users_email_ci: existing accounts differ only by email case; resolve them before migrating (first % group(s) shown):%', groups, report
            USING HINT = 'Delete or rename one account in each group (see the comment at the top of db/migrations/000007_users_email_ci.up.sql), then run migrate-up again. Nothing was changed.';
    END IF;
END
$$;

UPDATE users SET email = lower(email) WHERE email <> lower(email);

CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_unique_idx ON users (lower(email));

DROP INDEX IF EXISTS users_tenant_email_idx;
CREATE INDEX IF NOT EXISTS users_tenant_email_lower_idx ON users (tenant_id, lower(email));
