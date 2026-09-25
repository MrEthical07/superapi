# Multi-Tenancy

Tenancy is **off by default** (`TENANCY_ENABLED=false`) and costs nothing when
off. Turning it on makes the tenant a first-class request property: every
request is resolved to a validated tenant, goAuth scopes every user lookup,
session and reset/verification record to it, and tokens only work in the
tenant that issued them.

Requires goAuth **v0.5.0** (pinned in `go.mod`). goAuth's own contract is
documented in its [multi_tenancy.md](https://github.com/MrEthical07/goAuth/blob/v0.5.0/docs/multi_tenancy.md).

## 1. Tenant model

- A tenant is a row in `tenants` (`id TEXT PRIMARY KEY`, `slug`, `name`,
  `status IN ('active','inactive')`), migration `000002_tenants`.
- Users belong to exactly one tenant: `users.tenant_id` (migration
  `000005_users_tenant`, default `'0'`). `"0"` is goAuth's default tenant, the
  one it uses when no tenant is attached, so every pre-existing and
  single-tenant user lives there.
- There is no foreign key from `users.tenant_id` to `tenants.id`: the default
  tenant `"0"` has no `tenants` row. Existence is validated at the HTTP edge
  instead (`TENANCY_VALIDATE`).
- Tenant ids are 1–64 characters of `[A-Za-z0-9._-]`, starting alphanumeric.
  They end up in Redis keys, so the format is enforced before any lookup.

## 2. Request flow

```
request -> RequestID -> ClientIP -> ... -> AccessLog
        -> tenant middleware (internal/core/tenant.Middleware)
             resolve (header | subdomain) -> validate format
             -> validate tenants row active (cached) -> auth.WithRequestTenant
        -> router -> policies (AuthRequired checks token tenant == request tenant)
        -> handler -> service -> goAuth (uses the context tenant)
```

`auth.WithRequestTenant` calls `goauth.WithTenantID` (goAuth never reads
headers) and stores the same value under a SuperAPI key, readable with
`auth.RequestTenantFromContext`.

## 3. Configuration

| Env var | Default | Meaning |
|---|---|---|
| `TENANCY_ENABLED` | false | master switch |
| `TENANCY_RESOLVER` | header | `header` or `subdomain` |
| `TENANCY_HEADER` | X-Tenant-ID | header for the header resolver; a repeated header is rejected |
| `TENANCY_BASE_DOMAIN` | — | subdomain resolver: `acme.example.com` -> `acme` (single label only) |
| `TENANCY_VALIDATE` | true | tenant must exist and be `active` (needs Postgres) |
| `TENANCY_VALIDATE_CACHE_TTL` | 30s | in-process cache of validation results, positive and negative (bounded) |
| `TENANCY_EXEMPT_PATHS` | /healthz,/readyz,/metrics | exact paths that skip resolution (metrics path always exempt) |

A path-segment resolver is intentionally not provided: a global pre-routing
resolver would force every route, including `/api/v1/auth/*`, under a tenant
prefix. For tenant ids in resource paths use `policy.TenantMatchFromPath`.

## 4. Responses when the tenant is missing or wrong

| Case | Status | Body `error` |
|---|---|---|
| no tenant resolvable | 400 | `bad_request` / `tenant required` |
| malformed tenant id | 400 | `bad_request` / `tenant invalid` |
| unknown **or** inactive tenant | 404 | `not_found` / `tenant not found` (same response, so inactive tenants are not distinguishable) |
| validation cannot reach Postgres | 503 | `dependency_unavailable` |
| token issued in another tenant | 401 | `unauthorized` / `authentication required` |
| `TenantRequired` with principal/request mismatch | 404 | `not_found` |

## 5. What goAuth v0.5.0 enforces with tenancy on

The engine resolves users through `TenantAwareUserProvider`
(`GetUserByIdentifierInTenant`, `GetUserByIDInTenant`), implemented by
`StoreUserProvider` with the tenant predicate in SQL. goAuth fails `Build()` if
the provider lacks it.

| Path | Cross-tenant result |
|---|---|
| Login | same generic invalid-credentials error as a wrong password, same dummy hash timing |
| MFA confirm | challenge destroyed, generic MFA-invalid |
| Password-reset request | enumeration-safe 202, no record written |
| Email-verification request | enumeration-safe 202, no record written |
| Change password, account status, backup codes, TOTP, WebAuthn | user not found |
| Refresh / logout | session key built from the token tenant; misses in another tenant |

**Security fixes in goAuth v0.5.0 (they only apply with `TENANCY_ENABLED=true`):**
cross-tenant account takeover via password reset, cross-tenant login, and
cross-tenant email verification. Single-tenant deployments were never affected.

SuperAPI adds:

- **Token binding.** `AuthRequired` rejects a token whose tenant differs from
  the resolved request tenant, in every validation mode (jwt_only/hybrid routes
  never load the tenant-keyed session, so this is the only check there).
- **MFA storage scoping.** TOTP and backup-code queries also filter by the
  request tenant.

## 6. Identifier uniqueness (a schema decision)

By default `users.email` is **globally unique** (`users_email_unique_idx` plus
the column's `UNIQUE` constraint). This matches goAuth's default
`Account.AllowDuplicateIdentifierAcrossTenants = false`. goAuth never queries
across tenants, so it **cannot** enforce either rule; only the schema can.

To allow the same email in different tenants:

```sql
-- new migration, e.g. 000007_users_email_per_tenant.up.sql
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
DROP INDEX IF EXISTS users_email_unique_idx;
CREATE UNIQUE INDEX users_tenant_email_unique_idx ON users (tenant_id, email);
```

Then mirror it in `db/schema/auth_users.sql`, and in
`internal/core/auth/config.go` set
`cfg.Account.AllowDuplicateIdentifierAcrossTenants = true` to document the
contract. After that, the tenant-blind lookups (`GetUserByIdentifier`, used
only when tenancy is off) are ambiguous, so keep tenancy on.

## 7. Adopting tenancy

1. `make migrate-up` (000002 and 000005 are always applied; they are inert
   while tenancy is off).
2. Set `TENANCY_ENABLED=true` (plus resolver settings) in `.env`.
3. Create tenants and their users:
   `make user email=owner@acme.test tenant=acme create_tenant=1` creates the
   tenant row if missing (or insert into `tenants` yourself).
4. Move existing users out of the default tenant if needed:
   `UPDATE users SET tenant_id = 'acme' WHERE …;`
5. Restart; clients send `X-Tenant-ID` (or use tenant subdomains) on every
   request.

**In-flight links:** turning tenancy on changes where goAuth stores reset and
verification records (the request tenant instead of the user's). Links issued
before the switch may stop resolving; drain them first or let users
re-request.

## 8. Rules for module authors

- Never read the tenant from headers yourself; use
  `auth.RequestTenantFromContext` or the principal's `TenantID`.
- Tenant-scoped routes: `policy.AuthRequired` -> `policy.TenantRequired()` (->
  `policy.TenantMatchFromPath("tenant_id")` for `{tenant_id}` paths). The static
  verifier enforces this ordering.
- Scope every tenant-owned query by `tenant_id` in SQL.
- Cache and rate-limit authenticated tenant routes by tenant or user (presets do
  this when tenancy is on).
- **Do not expose goAuth audit events to tenant users or tenant admins.** The
  audit stream records `tenant_mismatch` and similar reasons the HTTP responses
  deliberately hide; showing it reintroduces an enumeration oracle.

## 9. Removing tenancy

<!-- template:begin init -->
On a fresh clone: `make init flags=--no-tenancy`. Otherwise:
<!-- template:end init -->
Follow the manual steps in [removing-tenancy.md](removing-tenancy.md).
