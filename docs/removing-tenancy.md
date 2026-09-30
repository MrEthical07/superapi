# Removing Tenancy

Tenancy is an optional feature (see [architecture.md](architecture.md), section
"Optional features"): all of its behavior lives in one directory,
`internal/tenancy/`, and it is registered with one line. This page covers the
two levels of "off": switching it off at runtime, and deleting it.

## 1. Switch it off at runtime (no code changes)

Leave `TENANCY_ENABLED` unset or `false` (the default). With tenancy off:

- no tenant middleware runs, so no tenant header is required and the `tenants`
  table is never read;
- goAuth's `MultiTenant.Enabled` stays `false`: user lookups are tenant-blind
  and every token carries goAuth's default tenant `"0"` (so `whoami` still
  reports `tenant_id: "0"`);
- the tenant presets (`tenancy.TenantRead` / `TenantWrite`) vary cached
  responses by user id instead of tenant id;
- a `{tenant_id}` path parameter is an ordinary parameter and forces no policy,
  although `TenantMatchFromPath` still requires `TenantRequired` wherever they
  are used;
- migration `000002_tenancy` is still applied by `make migrate-up`; it is inert
  (every user has `tenant_id = '0'`).

`TENANCY_ENFORCE_ISOLATION` is deprecated and ignored; remove it from your
environment.

## 2. Delete it from the codebase

On a fresh clone, **`make init flags=--no-tenancy` does exactly the steps below**
and regenerates sqlc for you; the template's CI proves the result builds, vets,
lints, verifies, passes its tests and migrations, and that
`git grep -n -i -E 'tenan(t|cy)' -- . ':!CHANGELOG.md'` finds nothing. That
pattern matches `tenant`, `tenants`, `tenant_id`, `TenantID`, `tenancy` and
`TENANCY_`, and not unrelated words that merely contain the letters (such as
`ListenAndServe`). The same CI run checks that the pattern does find tenancy in
the unpruned template, so the check cannot go blind.

If you did not use `make init`, six steps:

1. **Delete the package and its registration.** Remove `internal/tenancy/` and
   `internal/features/tenancy.go`, and delete the `tenancyFeature(),` line (with
   the two template marker comments around it) from
   `internal/features/features.go`.
2. **Delete the files named after tenancy:**
   `internal/tools/validator/tenancy_rules.go`,
   `internal/devx/modulegen/tenancy_extension.go` and its test,
   `docs/multi-tenancy.md`, `docs/removing-tenancy.md`,
   `.github/workflows/tenancy.yml` (the workflow that runs the suite with
   tenancy on). Template-only files (`.github/workflows/template-*`,
   `.github/template-*`) are not part of your project; delete them too if you
   did not start from `make init`.
3. **Delete the SQL:** `db/migrations/000002_tenancy.up.sql` and `.down.sql`,
   `db/schema/tenancy.sql`, `db/queries/tenancy.sql` and
   `internal/core/db/sqlcgen/tenancy.sql.go`, then run `make sqlc-generate`
   (it drops the tenants model and `users.TenantID`). Only delete the migration
   on a database that never applied it; otherwise add a new migration that drops
   `users.tenant_id` and the `tenants` table (see section 3).
4. **Delete the settings block:** the `TENANCY_*` block from `.env.example` and
   from `docs/environment-variables.md` (each is wrapped in markers).
5. **Tidy:** `go mod tidy`, and remove any leftover tenancy template marker
   comments.
6. **Check:** `go build ./... && go vet ./... && go test ./... &&
   go run ./cmd/superapi-verify ./...`.

Core needs no change: nothing in `internal/core` knows tenancy exists, and the
`make user flags=...` and `make module flags=...` passthroughs simply lose the
`--tenant` options.

## 3. After you have deployed with tenancy

Once a database has applied `000002_tenancy`, migrations are append-only. To
remove tenancy from a deployed project, do steps 1, 2, 4, 5 and 6, keep
`000002_tenancy` in your history, delete only the sqlc mirror lines for the
tenant column and table (`db/schema/tenancy.sql`, `db/queries/tenancy.sql`),
and add a new migration:

```sql
DROP INDEX IF EXISTS users_tenant_email_lower_idx;
ALTER TABLE users DROP COLUMN IF EXISTS tenant_id;
DROP TABLE IF EXISTS tenants;
```

## Related

- [environment-variables.md](environment-variables.md) — the `TENANCY_*` variables
- [multi-tenancy.md](multi-tenancy.md) — what tenancy does when enabled
