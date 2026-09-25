# Removing Tenancy

Tenancy in this template is optional and gated behind a single config flag. This
page covers the two levels of "off": disabling it at runtime, and deleting it
from the codebase entirely.

## 1. Disable at runtime (no code changes)

Leave `TENANCY_ENABLED` unset or set it to `false` (this is the default):

```
TENANCY_ENABLED=false
```

With tenancy disabled:

- Preset policy chains do not default to tenant scoping/keying. Authenticated
  cache reads vary by user id instead of tenant id, which satisfies the
  "authenticated cache routes must vary by user or tenant" safety rule.
- The route validator treats a `{tenant_id}` path parameter as an ordinary
  parameter; it does not force `TenantRequired` / `TenantMatchFromPath` onto the
  route.
- goAuth's `MultiTenant.Enabled` is set to `false`: user lookups are
  tenant-blind and every token carries goAuth's default tenant `"0"`.
- The tenant resolution middleware is not installed, so no tenant header is
  required and the `tenants` table is never read.
- Migrations `000002_tenants` and `000005_users_tenant` are still applied by
  `make migrate-up`; they are inert (every user has `tenant_id = '0'`).

The tenant policies (`policy.TenantRequired()`, `policy.TenantMatchFromPath(...)`)
remain available and enforce correctly if you attach them explicitly. The
dependency rule "`TenantMatchFromPath` requires `TenantRequired`" holds
regardless of the flag.

`TENANCY_ENFORCE_ISOLATION` is deprecated and ignored (goAuth v0.5.0 made the
underlying setting a no-op); remove it from your environment.

## 2. Delete tenancy from the codebase

<!-- template:begin init -->
On a fresh clone, `make init flags=--no-tenancy` performs steps 1–4 below
automatically and keeps the rest (they are harmless when tenancy is off).
<!-- template:end init -->

If your project will never be multi-tenant, tenancy is a bounded, greppable
deletion. Remove, in this order:

1. **HTTP tenant resolution:** `internal/core/tenant/resolver.go` and
   `directory.go` (+ tests), the `tenantResolver` function and the
   `httpx.WithTenantResolver(...)` option in `internal/core/app/app.go`.
2. **Config:** the `TENANCY_*` loads, fields, resolver constants and lint block
   in `internal/core/config/config.go` (keep `TenancyConfig.Enabled` if you keep
   steps 5–7, it stays `false`), plus the tenancy sections of `.env.example` and
   `docs/environment-variables.md`.
3. **Tenants table:** `db/migrations/000002_tenants.*`, `db/schema/tenants.sql`,
   `db/queries/tenants.sql`, then `make sqlc-generate` (delete the stale
   `internal/core/db/sqlcgen/tenants.sql.go`). Only delete migrations on a
   database that never applied them.
4. **Tooling:** the `--tenant`/`--create-tenant` handling in
   `cmd/createuser` (`tenant.go` and the marked blocks in `main.go`).
5. **Policies:** `internal/core/policy/tenant.go` — the `TenantRequired` /
   `TenantMatchFromPath` policies, the `TenantRead` / `TenantWrite` presets, and
   the package tenancy flag (`SetTenancyEnabled` / `TenancyEnabled`); the tenant
   plumbing in `internal/core/policy/options.go` (`tenantMatchParam`, the tenant
   branch in `defaultPresetConfig`) and `validator_rules.go`
   (`patternContainsTenantID` and its use in `validateTenantRules`), the tenant
   `PolicyType*` constants in `metadata.go`, the token/tenant check in
   `authRequiredWithEngine` (`internal/core/policy/auth.go`), and the matching
   support in `internal/tools/validator`.
6. **Tenant primitives:** `internal/core/tenant/` and
   `internal/core/auth/tenant_context.go` (`WithRequestTenant`,
   `RequestTenantFromContext`).
7. **goAuth provider:** `TenancySettings` in `internal/core/auth/config.go` and
   `goauth_provider.go` and the call site in `internal/core/app/deps.go`
   (`policy.SetTenancyEnabled(...)`, `WithTenancy(...)`, the `TenancySettings`
   argument); the `GetUserByIdentifierInTenant` / `GetUserByIDInTenant` provider
   and repository methods and their queries in `db/queries/auth_users.sql`; the
   tenant scoping (`scopeTenant`, the `tenant_id` argument) of the MFA queries.
   The `users.tenant_id` column (migration 000005) can then be dropped with a
   new migration.
8. **Cache and rate-limit keying (optional):** `cache.CacheVaryBy.TenantID`, tag
   `TenantID`, `ratelimit.ScopeTenant`, `KeyByTenant`, and the tenant branch of
   `KeyByUserOrTenantOrTokenHash`. These degrade gracefully and are safe to
   leave.

After deletion, run the gates:

```
go build ./...
go test ./...
go run ./cmd/superapi-verify ./...
```

## Related

- docs/policies.md — tenant policy reference and the optionality note
- docs/environment-variables.md — `TENANCY_*` variables
<!-- template:begin tenancy -->
- docs/multi-tenancy.md — what tenancy does when enabled
<!-- template:end tenancy -->
