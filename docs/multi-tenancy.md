# Multi-Tenancy

Tenancy is an **optional feature** (`internal/tenancy/`), compiled in but **off
by default** (`TENANCY_ENABLED=false`), and costs nothing when off. Turning it
on makes the tenant a first-class request property: every request is resolved to
a validated tenant, goAuth scopes every user lookup, session and
reset/verification record to it, and tokens only work in the tenant that issued
them.

Requires goAuth **v0.6.0** (pinned in `go.mod`). goAuth's own contract is
documented in its [multi_tenancy.md](https://github.com/MrEthical07/goAuth/blob/v0.6.0/docs/multi_tenancy.md).

Where things live: everything tenancy owns is in `internal/tenancy/` or in a
file whose name contains `tenancy` (migration `000002_tenancy`,
`db/schema/tenancy.sql`, `db/queries/tenancy.sql`, the verifier and scaffolder
extensions, these docs). Core knows nothing about it: it plugs in through the
generic hooks described in [architecture.md](architecture.md), section
"Optional features", and this feature is the worked example of that pattern.
Removing it is [removing-tenancy.md](removing-tenancy.md).

## 1. Tenant model

- A tenant is a row in `tenants` (`id TEXT PRIMARY KEY`, `slug`, `name`,
  `status IN ('active','inactive')`), created by migration `000002_tenancy`.
- Users belong to exactly one tenant: `users.tenant_id` (added by the same
  migration, default `'0'`). `"0"` is goAuth's default tenant, the one it uses
  when no tenant is attached, so every pre-existing and single-tenant user lives
  there.
- There is no foreign key from `users.tenant_id` to `tenants.id`: the default
  tenant `"0"` has no `tenants` row. Existence is validated at the HTTP edge
  instead (`TENANCY_VALIDATE`).
- Tenant ids are 1–64 characters of `[A-Za-z0-9._-]`, starting alphanumeric.
  They end up in Redis keys, so the format is enforced before any lookup.

## 2. How it plugs in

`internal/features/tenancy.go` returns `tenancy.New()`, listed in
`internal/features/features.go`. `Feature.Load` reads and lints the `TENANCY_*`
settings (through `config.EnvBool` and friends; core config has no tenancy
fields) and returns these hooks:

| Hook | With `TENANCY_ENABLED=false` | With `TENANCY_ENABLED=true` |
|---|---|---|
| `AuthExtension` | adds the principal attribute `tenant_id` (goAuth's tenant, `"0"` by default) | same, plus the token-to-tenant binding check |
| `RouteRules` | `TenantMatchFromPath` needs `TenantRequired`; `TenantRequired` needs `AuthRequired`; ordering | same, plus: a route with `{tenant_id}` in its path must carry `TenantRequired` and `TenantMatchFromPath("tenant_id")` |
| `GoAuthConfig` | — | `MultiTenant.Enabled = true` (`tenancy.EnableMultiTenant`) |
| `UserProvider` | — | `tenancy.Provider` wraps the core provider |
| `UserRepository` | — | tenant-scoped lookups for `Dependencies.AuthUsers` when a request tenant is attached |
| `Middleware` | — | the resolver middleware, innermost, just before routing |

`Enabled()` reports the switch for code that builds presets at startup.

## 3. Request flow

```
request -> RequestID -> ClientIP -> ... -> AccessLog
        -> tenant middleware (tenancy.Middleware)
             resolve (header | subdomain) -> validate format
             -> validate tenants row active (cached) -> tenancy.WithRequestTenant
        -> router -> policies (AuthRequired checks token tenant == request tenant)
        -> handler -> service -> goAuth (uses the context tenant)
```

`tenancy.WithRequestTenant` calls `goauth.WithTenantID` (goAuth never reads
headers) and stores the same value under a SuperAPI key, readable with
`tenancy.RequestTenantFromContext`.

## 4. Configuration

| Env var | Default | Meaning |
|---|---|---|
| `TENANCY_ENABLED` | false | master switch |
| `TENANCY_RESOLVER` | header | `header` or `subdomain` |
| `TENANCY_HEADER` | X-Tenant-ID | header for the header resolver; a repeated header is rejected |
| `TENANCY_BASE_DOMAIN` | — | subdomain resolver: `acme.example.com` -> slug `acme` (single label only) |
| `TENANCY_VALIDATE` | true | tenant must exist and be `active` (needs Postgres). **Required** with `TENANCY_RESOLVER=subdomain` |
| `TENANCY_VALIDATE_CACHE_TTL` | 30s | in-process cache of validation results, positive and negative. Bounded LRU: 10,000 active tenants plus a separate 1,000-entry segment for unknown/inactive ones, which live a quarter of this TTL (at least 1s). A flood of made-up tenant ids can only churn the negative segment, never evict real tenants |
| `TENANCY_EXEMPT_PATHS` | /healthz,/readyz,/metrics | exact paths that skip resolution (metrics path always exempt) |

### Header vs subdomain: id vs slug

The two resolvers read different columns of `tenants`:

| Resolver | Reads | Looks up | Attaches to the request |
|---|---|---|---|
| `header` | tenant **id** from `X-Tenant-ID` | `tenants.id` | that id |
| `subdomain` | tenant **slug** from the host label | `tenants.slug` (unique) | the tenant's **id** |

A subdomain is naturally a slug (`acme.example.com`), and the slug never
reaches goAuth, sessions, cache keys or modules: the resolver maps it to the
tenant's id, so everything downstream (`RequestTenantFromContext`, the
principal's `tenant_id` attribute, tenant-scoped SQL) sees the same id whichever
resolver is configured. Slugs follow the tenant id rules (1-64 characters of
`[a-z0-9._-]`, starting alphanumeric) and are **lower-case**: hostnames are
case-insensitive, the label is lower-cased before the lookup, and
`Repository.Create` lower-cases the slug it stores. A tenant whose stored slug
has upper-case letters can never be reached by subdomain; fix it with
`UPDATE tenants SET slug = lower(slug)`.

The validation cache is keyed by resolver and value, so id `acme` and slug
`acme` (which may be different tenants) never share an entry. Because a slug
cannot become an id without the `tenants` table, `TENANCY_RESOLVER=subdomain`
with `TENANCY_VALIDATE=false` is rejected at startup.

A path-segment resolver is intentionally not provided: a global pre-routing
resolver would force every route, including `/api/v1/auth/*`, under a tenant
prefix. For tenant ids in resource paths use `tenancy.TenantMatchFromPath`.

## 5. Responses when the tenant is missing or wrong

| Case | Status | Body `error` |
|---|---|---|
| no tenant resolvable | 400 | `bad_request` / `tenant required` |
| malformed tenant id | 400 | `bad_request` / `tenant invalid` |
| unknown **or** inactive tenant | 404 | `not_found` / `tenant not found` (same response, so inactive tenants are not distinguishable) |
| validation cannot reach Postgres | 503 | `dependency_unavailable` |
| token issued in another tenant | 401 | `unauthorized` / `authentication required` |
| `TenantRequired` with principal/request mismatch | 404 | `not_found` |

## 6. What goAuth enforces with tenancy on

The engine resolves users through `goauth.TenantAwareUserProvider`
(`GetUserByIdentifierInTenant`, `GetUserByIDInTenant`), implemented by
`tenancy.Provider` with the tenant predicate in SQL
(`db/queries/tenancy.sql`). goAuth fails `Build()` if the provider lacks it.

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
  never load the tenant-keyed session, so this is the only check there). It is
  the `Check` of the feature's `policy.AuthExtension`.
- **No tenant filter in core MFA SQL, on purpose.** TOTP and backup-code queries
  are keyed by the globally unique user id. goAuth v0.6.0 resolves the user
  through the tenant-scoped lookup (with a record-tenant backstop) before every
  id-keyed provider call SuperAPI makes: TOTP setup/confirm/verify/disable,
  backup-code generate/regenerate, WebAuthn registration and login, status and
  password changes. SuperAPI endpoints only ever pass the authenticated
  principal's own user id. Three engine entry points reach the provider **without**
  that lookup: `Engine.VerifyBackupCode`/`VerifyBackupCodeInTenant`,
  `Engine.ListWebAuthnCredentials` and `Engine.RemoveWebAuthnCredential`.
  `tenancy.Provider` therefore scopes `ConsumeBackupCode` and the WebAuthn
  list/remove calls to the request tenant itself (a user id from another tenant
  is not found). `TestCrossTenantMFAIsRejected` and
  `TestCrossTenantWebAuthnListAndRemoveAreRejected` prove a tenant A user id
  used under a tenant B request is rejected for TOTP setup/confirm/disable,
  backup-code regenerate/consume and WebAuthn list/remove.
- **Provider decorator.** `tenancy.Provider` embeds the core
  `auth.StoreUserProvider`, so every optional goAuth interface the core provider
  implements (WebAuthn, TOTP, backup codes) is preserved; each one is asserted
  at compile time.

## 7. Identifier uniqueness (a schema decision)

The baseline migration makes login identifiers case-insensitive and **globally
unique**: `users_email_lower_unique_idx` on `lower(email)`, plus
`CHECK (email = lower(email))` because the application lower-cases every
identifier (`auth.NormalizeIdentifier`). `000002_tenancy` adds
`users_tenant_email_lower_idx` on `(tenant_id, lower(email))` to serve
tenant-scoped lookups. Global uniqueness matches goAuth's default
`Account.AllowDuplicateIdentifierAcrossTenants = false`. goAuth never queries
across tenants, so it **cannot** enforce either rule; only the schema can.

To allow the same email in different tenants, edit `000001`/`000002` before your
first deployment, or add a migration afterwards:

```sql
-- new migration, e.g. 0000NN_users_email_per_tenant.up.sql
DROP INDEX IF EXISTS users_email_lower_unique_idx;
CREATE UNIQUE INDEX users_tenant_email_lower_unique_idx ON users (tenant_id, lower(email));
```

Identifiers are compared case-insensitively, so the per-tenant variant is
`UNIQUE (tenant_id, lower(email))`, never `(tenant_id, email)`. Then mirror it in
`db/schema/tenancy.sql` and `db/schema/auth_users.sql`, and in
`internal/core/auth/config.go` set
`cfg.Account.AllowDuplicateIdentifierAcrossTenants = true` to document the
contract. After that, the tenant-blind lookups (`GetUserByIdentifier`, used only
when tenancy is off) are ambiguous, so keep tenancy on.

## 8. Adopting tenancy

1. `make migrate-up` (`000002_tenancy` is always applied; it is inert while
   tenancy is off).
2. Set `TENANCY_ENABLED=true` (plus resolver settings) in `.env`.
3. Create tenants and their users:
   `make user email=owner@acme.test flags="--tenant acme --create-tenant"`
   creates the tenant row if missing (or insert into `tenants` yourself).
4. Move existing users out of the default tenant if needed:
   `UPDATE users SET tenant_id = 'acme' WHERE …;`
5. Restart; clients send `X-Tenant-ID` (or use tenant subdomains) on every
   request.

**In-flight links:** turning tenancy on changes where goAuth stores reset and
verification records (the request tenant instead of the user's). Links issued
before the switch may stop resolving; drain them first or let users
re-request.

## 9. Rules for module authors

- Never read the tenant from headers yourself; use
  `tenancy.RequestTenantFromContext`, `tenancy.TenantIDFromContext` or
  `tenancy.PrincipalTenant(principal)`.
- Tenant-scoped routes: `policy.AuthRequired` -> `tenancy.TenantRequired()` (->
  `tenancy.TenantMatchFromPath("tenant_id")` for `{tenant_id}` paths). The
  static verifier enforces this ordering.
- Scope every tenant-owned query by `tenant_id` in SQL.
- Cache and rate-limit authenticated tenant routes by tenant or user (the
  presets do this): `VaryBy: cache.CacheVaryBy{Parts: []cache.KeyPart{tenancy.CacheVary()}}`,
  tags `cache.CacheTagSpec{Name: "project", Parts: []cache.KeyPart{tenancy.CacheTag()}}`,
  rate limits `ratelimit.Rule{Scope: tenancy.ScopeTenant, Keyer: tenancy.KeyByTenant()}`.
- **Do not expose goAuth audit events to tenant users or tenant admins.** The
  audit stream records `tenant_mismatch` and similar reasons the HTTP responses
  deliberately hide; showing it reintroduces an enumeration oracle.

## 10. Policies, presets, cache and rate limits

Import the tenancy package next to the core policies:
`tenancy "github.com/<module>/internal/tenancy"`.

### Declaration order

```go
r.Handle(method, pattern, handler,
    policy.AuthRequired(authEngine, mode),

    // 2. Tenant scope (after auth: needs the principal)
    tenancy.TenantRequired(),

    // 3. Tenant path match (routes with {tenant_id} in the URL)
    tenancy.TenantMatchFromPath("tenant_id"),

    // 4. RBAC (after tenant)
    policy.RequirePerm("project.write"),

    // 5. Rate limit (after auth, so the tenant or user is available for keying)
    policy.RateLimitWithKeyer(limiter, "projects.list", rule, tenancy.KeyByTenant()),
    // 6. Cache read / invalidate, 7. CacheControl (optional)
)
```

The tenant policies sit at `policy.StageIsolation`, between `AuthRequired` and
RBAC. The principal's tenant is the attribute `tenant_id`:
`tenancy.PrincipalTenant(principal)` or `tenancy.TenantIDFromContext(ctx)`.

With tenancy **off** the tenant policies are still available and enforce
correctly when you attach them (every principal then carries goAuth's default
tenant `0`); the validator just does not force them onto `{tenant_id}` routes,
and the presets default to per-user cache keys. With `TENANCY_ENABLED=true`,
`{tenant_id}` routes must carry `TenantRequired` and
`TenantMatchFromPath("tenant_id")` (the tenancy `RouteRule`, applied by the
router and by `superapi-verify`).

### TenantRequired()

- no principal -> 401 `unauthorized`
- principal without a tenant -> 403 `forbidden` ("tenant scope required")
- a request tenant was resolved and differs from the principal's -> 404 `not_found`
- otherwise passes

### TenantMatchFromPath(param)

Compares the path parameter with the principal's tenant. Missing param -> 400,
principal without tenant -> 403, mismatch -> **404 `not_found`** (not 403: a 403
would let an attacker enumerate which tenant ids exist; 404 covers both "does
not exist" and "exists but not yours"). Match the chi parameter name exactly;
with tenancy on the route rule also requires it to be `tenant_id`. For "self"
routes like `/api/v1/tenants/self`, use `TenantRequired()` and read
`tenancy.TenantIDFromContext(r.Context())` in the handler.

### Rate limits

| Scope | Where | Key |
|---|---|---|
| `tenancy.ScopeTenant` | `tenancy.KeyByTenant()` | the principal's tenant; a shared limit across the tenant's users |
| composite | `tenancy.KeyByUserOrTenantOrTokenHash(n)` | user, then tenant, then token hash, then anonymous |

Core keeps `ratelimit.KeyByUserOrTokenHash(n)`; `ratelimit.ScopeAuto` resolves user
then token hash.

### Cache

- Vary: `cache.CacheVaryBy{Parts: []cache.KeyPart{tenancy.CacheVary()}}` adds
  `tenant=<id>` to the key (right after `method=`). It is identity-bearing, so it
  satisfies "an authenticated cached response varies by `UserID` or an
  identity-bearing part".
- Tags: `cache.CacheTagSpec{Name: "project-list", Parts: []cache.KeyPart{tenancy.CacheTag()}}`.
  A tag with no tenant is an error, so a write can never bump an unscoped tag.

```go
// read: the tenant's project list
policy.CacheRead(cacheMgr, cache.CacheReadConfig{
    TTL: 30 * time.Second,
    TagSpecs: []cache.CacheTagSpec{{Name: "project-list", Parts: []cache.KeyPart{tenancy.CacheTag()}}},
    VaryBy: cache.CacheVaryBy{
        Parts:       []cache.KeyPart{tenancy.CacheVary()},
        QueryParams: []string{"limit", "cursor"},
    },
})

// write: invalidate the detail and this tenant's list, not other tenants'
policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
    TagSpecs: []cache.CacheTagSpec{
        {Name: "project", PathParams: []string{"id"}},
        {Name: "project-list", Parts: []cache.KeyPart{tenancy.CacheTag()}},
    },
})
```

| Route | Recommended tag spec |
|---|---|
| tenant list endpoint | `Name: "project-list", Parts: [tenancy.CacheTag()]` |
| cross-entity list | `Name: "dashboard-list", Parts: [tenancy.CacheTag()], Literals: [{Key: "view", Value: "summary"}]` |

Forgetting the matching `CacheInvalidate` on a write leaves the list stale until
its TTL expires.

### Presets

`tenancy.TenantRead(opts...)` and `tenancy.TenantWrite(opts...)` build a
validated chain from the same options as the core presets
(`policy.WithAuthEngine`, `WithLimiter`, `WithCacheManager`, `WithCache`,
`WithCacheVaryBy`, `WithInvalidateTags`):

```go
r.Handle(http.MethodGet, "/api/v1/projects/{id}", handler,
    tenancy.TenantRead(
        policy.WithAuthEngine(authEngine, auth.ModeStrict),
        policy.WithLimiter(limiter),
        policy.WithCacheManager(cacheMgr),
        policy.WithCache(30*time.Second, cache.CacheTagSpec{Name: "project", PathParams: []string{"id"}}),
        policy.WithCacheVaryBy(cache.CacheVaryBy{Parts: []cache.KeyPart{tenancy.CacheVary()}, PathParams: []string{"id"}}),
    )...,
)
```

`TenantRead` is `AuthRequired`, `TenantRequired`, `RateLimit` (scope
`tenancy.ScopeTenant`, keyer `tenancy.KeyByTenant()`), `CacheRead` (varying by
tenant when tenancy is on, by user when off, unless `WithCacheVaryBy` says
otherwise). `TenantWrite` swaps the cache policy for `CacheInvalidate`.

### Recommended stacks

```go
// GET /api/v1/projects/{id}
policy.AuthRequired(authEngine, auth.ModeStrict),
tenancy.TenantRequired(),
policy.RateLimitWithKeyer(limiter, "projects.get", rule, tenancy.KeyByTenant()),
policy.CacheRead(cacheMgr, cache.CacheReadConfig{
    TTL:      30 * time.Second,
    TagSpecs: []cache.CacheTagSpec{{Name: "project", PathParams: []string{"id"}}},
    VaryBy:   cache.CacheVaryBy{Parts: []cache.KeyPart{tenancy.CacheVary()}, PathParams: []string{"id"}},
}),

// POST /api/v1/projects
policy.AuthRequired(authEngine, auth.ModeStrict),
tenancy.TenantRequired(),
policy.RequirePerm("project.write"),
policy.RateLimitWithKeyer(limiter, "projects.create", rule, tenancy.KeyByTenant()),
policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
    TagSpecs: []cache.CacheTagSpec{{Name: "project-list", Parts: []cache.KeyPart{tenancy.CacheTag()}}},
}),
```

### Scaffolding and tools

- `make module name=projects auth=1 flags=--tenant` scaffolds the tenant policy
  and tenant cache parts (`modulegen` extension in
  `internal/devx/modulegen/tenancy_extension.go`; it needs `auth=1`).
- `make user email=admin@example.com role=admin flags="--tenant acme --create-tenant"`
  (`app.UserCLI`): with `TENANCY_ENABLED=true` `--tenant` is required and
  `--create-tenant` creates the row if missing; with tenancy off `--tenant` is
  rejected.
- `superapi-verify` knows `TenantRequired`, `TenantMatchFromPath`, the identity
  parts `tenancy.CacheVary()`/`CacheTag()` and the tenancy route rule
  (`internal/tools/validator/tenancy_rules.go`).

## 11. A tenant-scoped CRUD module

The core CRUD example (docs/crud-examples.md) scopes by owner. The tenant
variant differs like this. The domain model and repository contract carry the
tenant, and every statement is scoped by `tenant_id` **in SQL**:

```go
type ProjectRepository interface {
    Create(ctx context.Context, input CreateProjectInput) (Project, error)
    GetByID(ctx context.Context, tenantID, id string) (Project, error)
    List(ctx context.Context, tenantID string, limit int32) ([]Project, error)
    Update(ctx context.Context, tenantID, id string, name string, status string) (Project, error)
    Delete(ctx context.Context, tenantID, id string) error
}
```

```sql
-- name: GetProjectByID :one
SELECT id, tenant_id, name, status FROM projects WHERE tenant_id = $1 AND id = $2;
```

```sql
CREATE TABLE projects (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX projects_tenant_id_idx ON projects (tenant_id);
```

The handler reads the tenant from the context, never from headers:

```go
func (h *Handler) Create(ctx *httpx.Context, req createProjectRequest) (projectResponse, error) {
    tenantID, ok := tenancy.TenantIDFromContext(ctx.Context())
    if !ok {
        return projectResponse{}, apperr.New(apperr.CodeForbidden, 403, "tenant scope required")
    }
    project, err := h.svc.Create(ctx.Context(), tenantID, req)
    if err != nil {
        return projectResponse{}, err
    }
    return toProjectResponse(project), nil
}
```

## 12. Auth behavior with tenancy on

- Every request except the exempt paths needs a tenant (default header
  `X-Tenant-ID`).
- `whoami` returns `{"user_id","tenant_id","role","permissions"}`: the tenant is
  reported as a feature attribute, so it appears only because this feature is
  registered (with tenancy off it is goAuth's default tenant `"0"`).
- Wrong password, unknown user and a user from another tenant all return the
  same 401 `invalid credentials`; the response table is in section 5.
- Password-reset and email-verification requests only deliver a real challenge
  when the account exists in the request tenant. The full verification challenge
  carries its own tenant (`<tenant>:<verification-id>:<code>`; `0` by default)
  and works regardless of the request tenant; the id + code form uses the
  request tenant.
- `UpdatePasswordHash` and `UpdateAccountStatus` are keyed by user UUID: goAuth
  resolves the user in-tenant first, and email-verification confirm intentionally
  runs under the challenge's tenant, so they do not re-scope by the request tenant.
- Startup fails with `MultiTenant is enabled but the user provider does not
  implement TenantAwareUserProvider` if a custom provider replaced the feature's
  wrapper without the tenant methods; a login 401 for an account that exists
  usually means the wrong `X-Tenant-ID` or password (or a locked account, 403).
