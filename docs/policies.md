# Policy Reference

Policies are per-route middleware functions applied during route registration. They execute in declaration order (first listed = outermost) and can short-circuit the request by writing a response without calling `next`.

**Type:** `type Policy func(http.Handler) http.Handler`

**File:** `internal/core/policy/policy.go`

## Strict guarantees (fail-fast)

SuperAPI enforces policy invariants at registration time.

- Every `r.Handle(...)` call is validated via `policy.MustValidateRoute(...)`.
- Invalid policy order/dependencies panic immediately with `invalid route config: ...`.
- No warning-only mode and no compatibility fallback paths.
- `go run ./cmd/superapi-verify ./...` (or `make verify`) applies the same checks statically.

---

## 1. Policy chaining order

The `policy.Chain()` function wraps the handler with policies. For policies `[P1, P2, P3]`:

- **Request path:** P1 → P2 → P3 → handler
- **Response path:** handler → P3 → P2 → P1

If P2 short-circuits (writes a response without calling next), P3 and handler never execute.

### Recommended declaration order

```go
r.Handle(method, pattern, handler,
    // 1. Authentication (outermost — reject unauthenticated early, add auth context for downstream policies)
    policy.AuthRequired(authEngine, mode),

    // 2. Isolation (optional — only when an optional feature provides one, for
    //    example a hypothetical orgs.OrgRequired(); after auth — needs AuthContext)
    orgs.OrgRequired(),

    // 3. RBAC (after isolation — needs AuthContext)
    policy.RequirePerm("project.write"),
    // or: policy.RequireAnyPerm("project.write", "project.admin"),

    // 4. Rate limit (after auth — so the user scope is available for keying)
    policy.RateLimit(limiter, rule),

    // 5. Cache (innermost — closest to handler)
    policy.CacheRead(cacheMgr, cacheConfig),
    // or for writes:
    policy.CacheInvalidate(cacheMgr, invalidateConfig),

    // 6. Browser/proxy cache directives (optional)
    policy.CacheControl(policy.CacheControlConfig{Public: true, MaxAge: 60 * time.Second}),
)
```

---

## 2. Auth policies

File: `internal/core/policy/auth.go`

### AuthRequired(engine, mode)

Extracts Bearer token from `Authorization` header, validates it using goAuth middleware guard, and injects `AuthContext` into the request context.

```go
policy.AuthRequired(m.authEngine, m.authMode)
```

**Behavior:**
- Missing/empty `Authorization` header → 401 `unauthorized`
- Invalid or non-`Bearer` format → 401 `unauthorized`
- goAuth validation failure → 401 `unauthorized`
- Success: `auth.AuthContext` injected into context via `auth.WithContext()`

**Auth modes** (passed to goAuth guard):

| Mode | Constant | Behavior |
|---|---|---|
| JWT-only | `auth.ModeJWTOnly` | Validates JWT signature and claims only. No Redis session check. Fastest, but cannot detect revoked tokens. |
| Hybrid | `auth.ModeHybrid` | Validates JWT first; if Redis is available, also checks session. Falls back to JWT-only if Redis is down. |
| Strict | `auth.ModeStrict` | Requires both valid JWT and active Redis session. Fails closed if Redis is unavailable. Most secure. |

**Hybrid guarantee (goAuth v0.4.0+).** A per-route mode wins over the engine's
default. `AuthRequired(engine, ModeHybrid)` passes `ModeInherit` to the guard, so
the route validates per the engine's configured `ValidationMode`; an explicit
`ModeJWTOnly` or `ModeStrict` on the route overrides it for that route only.

> **Security caveat — `ModeJWTOnly` is a downgrade.** A route that opts into
> `auth.ModeJWTOnly` validates the JWT signature and claims only and **skips the
> Redis session check**. That bypasses revocation, token-version, device-binding,
> and account-status checks — a logged-out or disabled user's un-expired access
> token will still pass. Only downgrade routes that are safe to serve from the
> JWT alone (short-TTL, low-sensitivity reads). Never use `ModeJWTOnly` for
> logout-sensitive, account-state-sensitive, or mutating routes. The logout
> endpoint deliberately uses `LogoutByAccessToken`, which accepts an
> expired-but-authentic token so a user can always end a session.

**Injected AuthContext:**

```go
type AuthContext struct {
    UserID      string           // Always present on success
    Role        string           // "user", "admin", etc.
    Permissions []string         // e.g., ["system.whoami", "project.write"]
    Attributes  []auth.Attribute // Feature-owned values (see AuthExtension)
}
```

`principal.Attribute(key)` returns the value of one attribute, or `""` when no
feature set it.

Reading it in handlers/services:

```go
principal, ok := auth.FromContext(r.Context())
if !ok {
    // Not authenticated (should not happen after AuthRequired policy)
}
```

### AuthExtension (optional features)

An optional feature takes part in every `AuthRequired` policy through a
`policy.AuthExtension`, so core never learns the feature exists:

```go
type AuthExtension struct {
    // Adds one feature-owned principal attribute from goAuth's result
    // (return an empty key to add none).
    Attribute func(result *goauth.AuthResult) (key, value string)
    // Runs after goAuth accepted the token. A non-nil error rejects the
    // request with the same 401 as an invalid token, so a client cannot tell
    // why it was rejected.
    Check func(r *http.Request, result *goauth.AuthResult) error
}
```

Features hand their extension to the app through `app.Hooks.AuthExtension`, and
the app registers them once at startup with
`policy.UseAuthExtensions(engine, extensions...)`. When no feature registers
one, `AuthRequired` behaves exactly as described above.

### RequirePerm(perms...)

Checks that the authenticated user has **all** of the specified permissions.

```go
policy.RequirePerm("project.read", "project.write")
```

**Behavior:**
- No AuthContext → 401 `unauthorized`
- Missing any required permission → 403 `forbidden`
- All permissions present → passes through

### RequireAnyPerm(perms...)

Checks that the authenticated user has **at least one** of the specified permissions.

```go
policy.RequireAnyPerm("project.write", "project.admin")
```

**Behavior:**
- No AuthContext → 401 `unauthorized`
- No matching permission → 403 `forbidden`
- Any permission matches → passes through
- Empty perms list → startup panic (`invalid route config`)

---

## 3. Optional-feature policies and route rules

Core ships no isolation policy. An optional feature that needs one (per
organization, per region, per plan) defines it in its own package and plugs it
into core through the generic mechanisms below. See
[architecture.md](architecture.md#13-optional-features) for how features are
registered, and the feature's own `docs/removing-*.md` guide if it has one.

**Stages.** Every policy has a stage that fixes where it may appear; the route
validator rejects a policy that appears after one of a later stage:

| Stage | Constant | Built-in policies |
|---|---|---|
| 1 | `policy.StageAuth` | `AuthRequired` |
| 2 | `policy.StageIsolation` | none in core; feature isolation policies go here (after auth, before RBAC) |
| 3 | `policy.StageRBAC` | `RequirePerm`, `RequireAnyPerm` |
| 4 | `policy.StageRateLimit` | `RateLimit`, `RateLimitWithKeyer` |
| 5 | `policy.StageCache` | `CacheRead`, `CacheInvalidate` |
| 6 | `policy.StageCacheControl` | `CacheControl` |

**Annotation.** A feature policy registers its validator-visible metadata with
`policy.Annotate(p, policy.Metadata{Type, Name, Stage, Data})`. `Stage` places
it in the order above; `Data` carries feature-owned annotations (for example the
path parameter the policy enforces) for route rules to read.

```go
func OrgRequired() policy.Policy {
    p := func(next http.Handler) http.Handler { /* reject when the principal has no org */ }
    return policy.Annotate(p, policy.Metadata{
        Type:  "org_required",
        Name:  "OrgRequired",
        Stage: policy.StageIsolation,
    })
}
```

**Route rules.** A `policy.RouteRule`
(`func(method, pattern string, metas []policy.Metadata) error`) is an extra
check the route validator runs after the built-in ones, for example "a route
with an `{org_id}` path segment must carry the org path-match policy".
Features contribute rules through `app.Hooks.RouteRules`, and the router
applies them to every route (`policy.MustValidateRouteWith`). For static
verification a feature also calls `validator.RegisterExtension(...)` (policy
parsers, identity cache-part names, rules, hints) from a file of its own, so
`superapi-verify` enforces the same rules.

**Principal attributes.** The values an isolation policy compares (an org id, for
example) reach it as `AuthContext.Attributes`, added by the feature's
`policy.AuthExtension` (see section 2).

**Isolation mismatch strategy.** Prefer returning 404 (not 403) when a
principal asks for a scope it does not belong to. With 403, an attacker could
enumerate which scope ids exist by comparing 403 with 404; returning 404 for
both "does not exist" and "exists but is not yours" closes that leak.

**Presets.** Core ships `policy.PublicRead(...)`. A feature builds its own
validated presets from `policy.ResolvePreset(opts...)`,
`PresetSettings.Require(...)` and `policy.MustValidatePreset(...)`; the options
are the same (`policy.WithAuthEngine`, `WithLimiter`, `WithCacheManager`,
`WithCache`, `WithCacheVaryBy`, ...).

---

## 4. Rate limit policy

File: `internal/core/policy/ratelimit.go`

### RateLimit(limiter, rule)

Basic rate limiting with automatic scope resolution.

```go
policy.RateLimit(limiter, ratelimit.Rule{
    Limit:  100,
    Window: time.Minute,
    Scope:  ratelimit.ScopeUser,
})
```

### RateLimitWithKeyer(limiter, name, rule, keyer)

Rate limiting with a custom key function.

```go
policy.RateLimitWithKeyer(limiter, "projects.list", rule, ratelimit.KeyByUser())
```

### Scopes and keying strategies

| Scope | Constant | Key based on | When to use |
|---|---|---|---|
| Auto | `ScopeAuto` | User → Token hash → Anonymous | Default. Tries the most specific scope available. |
| Anon | `ScopeAnon` | Static "anonymous" | Public endpoints, no identity available |
| IP | `ScopeIP` | Resolved client IP (trusted proxy headers when configured) | Public endpoints where IP is meaningful |
| User | `ScopeUser` | `AuthContext.UserID` | Authenticated endpoints, per-user limits |
| Token | `ScopeToken` | SHA-256 hash prefix of Bearer token | When you want per-token limits (e.g., API keys) |

**Built-in keyers:**

- `ratelimit.KeyByIP()` — key by resolved client IP
- `ratelimit.KeyByUser()` — key by user ID from auth context
- `ratelimit.KeyByTokenHash(prefixLen)` — key by token hash prefix
- `ratelimit.KeyByUserOrTokenHash(prefixLen)` — cascading: user → token → anon
- `ratelimit.KeyByAnonymous()` — static "anonymous" key

**Custom keyers** can be provided as `func(r *http.Request) (Scope, string)`. An
optional feature uses this to add a scope of its own (a shared per-organization
limit, for example) by exporting its own `Scope` constant and keyer.

### Key format

```
rl:{env}:{route_pattern}:{scope}:{identifier}
```

Example: `rl:prod:/api/v1/projects:user:usr_abc123`

Client IP note:

- IP scoping trusts `Forwarded` / `X-Forwarded-For` only when `HTTP_TRUSTED_PROXIES` is configured. Otherwise `RemoteAddr` is used.

### Fail-open / fail-closed behavior

Controlled by `RATELIMIT_FAIL_OPEN` (default: `true` in non-prod, `false` in prod).

In prod, startup lint rejects `RATELIMIT_FAIL_OPEN=true` when rate limiting is enabled.

- **Fail-open:** When Redis is unavailable, requests are allowed through. The decision outcome is recorded as `fail_open`.
- **Fail-closed:** When Redis is unavailable, the rate limiter returns an error and the policy responds with 500.

### Retry-After header

When a request is rate-limited (429), the `Retry-After` header is set with the number of seconds until the window resets.

### Production notes

- Rate limit keys use low-cardinality values. Route patterns (not raw URLs), scopes, and sanitized identifiers.
- Bearer tokens are never stored in keys — only a SHA-256 hash prefix (16 hex chars by default).
- `RateLimit(...)` and `RateLimitWithKeyer(...)` require a non-nil limiter and a valid rule; invalid config panics at registration.

---

## 5. Cache policies

File: `internal/core/policy/cache.go`

### CacheRead(manager, config)

Serves cached responses for matching requests and stores responses on cache miss.

```go
policy.CacheRead(cacheMgr, cache.CacheReadConfig{
    TTL:  30 * time.Second,
    TagSpecs: []cache.CacheTagSpec{
        {Name: "project", PathParams: []string{"id"}},
    },
    VaryBy: cache.CacheVaryBy{
        UserID:      true,
        PathParams:  []string{"id"},
        QueryParams: []string{"limit", "cursor"},
    },
})
```

**CacheReadConfig fields:**

| Field | Type | Default | Description |
|---|---|---|---|
| `Key` | `string` | route pattern | Optional custom cache key prefix when you want tighter control than the route pattern |
| `TTL` | `time.Duration` | (required) | Cache entry time-to-live |
| `MaxBytes` | `int` | `CACHE_DEFAULT_MAX_BYTES` (256 KiB) | Max response body size to cache |
| `TagSpecs` | `[]CacheTagSpec` | — | Dynamic invalidation scopes included in key (version-bumped on write) |
| `Methods` | `[]string` | `["GET", "HEAD"]` | HTTP methods eligible for caching |
| `CacheStatuses` | `[]int` | `[200]` | HTTP status codes to cache |
| `VaryBy` | `CacheVaryBy` | — | Dimensions that differentiate cache entries |
| `FailOpen` | `*bool` | Global `CACHE_FAIL_OPEN` | Per-route fail-open override |
| `AllowAuthenticated` | `bool` | `false` | Enables authenticated caching behavior in the cache layer; does not override validator safety rules |

**CacheTagSpec fields:**

| Field | Type | Description |
|---|---|---|
| `Name` | `string` | Base tag family name |
| `PathParams` | `[]string` | Path params appended to tag scope |
| `UserID` | `bool` | Include auth user id in tag scope |
| `Parts` | `[]cache.KeyPart` | Feature-contributed dimensions (see [cache-guide.md](cache-guide.md#key-parts)); resolving a tag whose part is empty is an error |
| `Literals` | `[]CacheTagLiteral` | Constant key/value dimensions for scope splits |

**CacheVaryBy fields:**

| Field | Type | Description |
|---|---|---|
| `Method` | `bool` | Include HTTP method in key |
| `UserID` | `bool` | Include user ID from AuthContext |
| `Role` | `bool` | Include role from AuthContext |
| `Parts` | `[]cache.KeyPart` | Feature-contributed dimensions, each rendered `name=value` in the key (see [cache-guide.md](cache-guide.md#key-parts)) |
| `PathParams` | `[]string` | Include named path parameters |
| `QueryParams` | `[]string` | Include specific query parameters (hash of values) |
| `Headers` | `[]string` | Include specific request headers |

**Behavior flow:**

1. Check if HTTP method is allowed (default: GET/HEAD only)
2. Enforce authenticated cache safety rules (see below)
3. Resolve tag names from TagSpecs, fetch their versions, and build cache key
4. Attempt cache GET
   - **Hit:** Serve cached response directly, return
   - **Miss:** Continue to handler
5. Capture handler response
6. If response is cacheable, store in Redis with TTL

**Authenticated caching safety (strict):**

For authenticated routes (those with `AuthRequired`), `CacheRead` must include at least one identity boundary:

- `VaryBy.UserID = true`, or
- a `VaryBy.Parts` entry whose `cache.KeyPart` has `Identity: true` (a part contributed by an optional feature that names who the response is for)

If neither is set, validation fails and route registration panics. `AllowAuthenticated` does not bypass this requirement.

**Not cached:**
- Streaming responses (flushed or hijacked)
- Responses larger than MaxBytes
- Responses with `Set-Cookie` header
- Non-matching status codes

### CacheInvalidate(manager, config)

Bumps versions for resolved TagSpecs after a successful write operation, causing matching cached entries to miss on next read.

```go
policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
    TagSpecs: []cache.CacheTagSpec{
        {Name: "project", PathParams: []string{"id"}},
        {Name: "project-list", UserID: true},
    },
})
```

**Behavior:**
1. Passes request to handler
2. If handler returns a 2xx status code, resolves tag names from request/auth context
3. Bumps all resolved tag versions
4. If handler returns non-2xx, no invalidation occurs

**Production notes:**
- Invalidation uses `INCR` on tag version keys (`cver:{env}:{tag}`), which is O(1)
- This is NOT mass key deletion — it's cheap and fast
- Multiple scoped tags can be invalidated in a single Redis pipeline
- `CacheInvalidate(...)` requires a non-nil manager and at least one tag spec; invalid config panics at registration

### Safe defaults summary

| Default | Value | Why |
|---|---|---|
| Cache methods | `GET`, `HEAD` | Prevent caching side effects from write methods |
| Cache statuses | `200` | Avoid caching error responses by default |
| Set-Cookie handling | Skip responses with `Set-Cookie` | Prevent session and identity leakage |
| Max body guard | `CACHE_DEFAULT_MAX_BYTES` | Avoid unbounded Redis memory usage |
| Authenticated key isolation | Require `VaryBy.UserID` or an identity-bearing `VaryBy.Parts` entry | Prevent cross-user cache data leaks |
| Redis error handling | Fail-open in non-prod, fail-closed in prod by default | Balance availability in dev/test with safer prod posture |

---

## 6. Cache-Control policy (browser/proxy cache)

File: `internal/core/policy/cachecontrol.go`

Use this policy to attach explicit `Cache-Control` and optional `Vary` headers to a route.

```go
policy.CacheControl(policy.CacheControlConfig{
    Public:       true,
    MaxAge:       60 * time.Second,
    SharedMaxAge: 120 * time.Second,
    Immutable:    true,
    Vary:         []string{"Accept-Encoding"},
})
```

### Supported directives

| Field | Header directive |
|---|---|
| `Public` | `public` |
| `Private` | `private` |
| `NoStore` | `no-store` |
| `NoCache` | `no-cache` |
| `MustRevalidate` | `must-revalidate` |
| `Immutable` | `immutable` |
| `MaxAge` | `max-age=<seconds>` |
| `SharedMaxAge` | `s-maxage=<seconds>` |
| `StaleWhileRevalidate` | `stale-while-revalidate=<seconds>` |
| `StaleIfError` | `stale-if-error=<seconds>` |

### Validation rules

- Durations must be `>= 0`.
- `Public` and `Private` cannot both be set.
- `NoStore` cannot be combined with max-age/s-maxage/stale/immutable directives.
- Policy must set at least one cache directive or one `Vary` value.

### Placement guidance

- Place `CacheControl(...)` after auth/isolation/rbac/rate-limit/cache policies so it applies consistently to both fresh and cached responses.
- Use conservative values for authenticated routes; avoid `public` unless the response is intentionally shared.

---

## 7. Utility policies

### RequireJSON()

Ensures `Content-Type: application/json` on requests with bodies (POST/PUT/PATCH).

```go
policy.RequireJSON()
```

**Behavior:**
- GET/HEAD/DELETE without body → passes through
- POST/PUT/PATCH without `application/json` Content-Type → 415 Unsupported Media Type (standard error envelope)
- Correct Content-Type → passes through

### WithHeader(key, value)

Adds a response header.

```go
policy.WithHeader("X-Custom", "value")
```

### Noop()

Does nothing. Useful as a placeholder.

```go
policy.Noop()
```

---

## 8. Validator and preset usage

### Runtime validator rules

The strict validator enforces:

- Policy order by stage: auth -> isolation -> RBAC -> rate-limit -> cache -> cache-control (see section 3).
- Auth dependency: RBAC policies require `AuthRequired`.
- Cache safety: authenticated routes using `CacheRead` must vary by `UserID` or an identity-bearing `VaryBy.Parts` entry.
- Feature rules: every `policy.RouteRule` a feature registered runs after the checks above (for example, a path-parameter isolation policy required whenever the route has a matching path segment).

### Static verification

```bash
go run ./cmd/superapi-verify ./...
# or
make verify
```

### Presets

Use built-in validated presets when possible:

- `policy.PublicRead(...)`
- feature presets, when an optional feature ships them (built from `policy.ResolvePreset`, see section 3)

Example:

```go
r.Handle(http.MethodGet, "/api/v1/public/resource", handler,
    policy.PublicRead(
        policy.WithLimiter(limiter),
        policy.WithCacheManager(cacheMgr),
        policy.WithCache(30*time.Second, cache.CacheTagSpec{Name: "resource"}),
    )...,
)
```

---

## 9. Recommended policy stacks by endpoint type

### Public route (no auth)

Example: `GET /api/v1/status`

```go
r.Handle(http.MethodGet, "/api/v1/status", handler,
    policy.RateLimitWithKeyer(limiter, "status", ratelimit.Rule{
        Limit: 60, Window: time.Minute, Scope: ratelimit.ScopeIP,
    }, ratelimit.KeyByIP()),
    policy.CacheRead(cacheMgr, cache.CacheReadConfig{
        TTL: 10 * time.Second,
    }),
)
```

Policies:
- Rate limit by IP (no auth context available)
- CacheRead if safe (no user-specific data)
- No auth or isolation policies

### Authenticated route

Example: `GET /api/v1/auth/whoami`

```go
r.Handle(http.MethodGet, "/api/v1/auth/whoami", handler,
    policy.AuthRequired(authEngine, mode),
    policy.RateLimitWithKeyer(limiter, "whoami", ratelimit.Rule{
        Limit: 30, Window: time.Minute, Scope: ratelimit.ScopeUser,
    }, ratelimit.KeyByUserOrTokenHash(16)),
)
```

Policies:
- AuthRequired (hybrid or strict)
- Rate limit by user/token (auth context available after AuthRequired)
- CacheRead only when `VaryBy.UserID` (or an identity-bearing `VaryBy.Parts` entry) is set

### Authenticated, cached read route

Example: `GET /api/v1/projects/{id}`

```go
r.Handle(http.MethodGet, "/api/v1/projects/{id}", handler,
    policy.AuthRequired(authEngine, auth.ModeStrict),
    policy.RateLimitWithKeyer(limiter, "projects.get", rule, ratelimit.KeyByUser()),
    policy.CacheRead(cacheMgr, cache.CacheReadConfig{
        TTL: 30 * time.Second,
        TagSpecs: []cache.CacheTagSpec{
            {Name: "project", PathParams: []string{"id"}},
        },
        VaryBy: cache.CacheVaryBy{
            UserID:     true,
            PathParams: []string{"id"},
        },
    }),
)
```

Policies:
- AuthRequired strict (recommended for user data)
- Rate limit by user
- CacheRead with user + path param vary

### Authenticated write route

Example: `POST /api/v1/projects`

```go
r.Handle(http.MethodPost, "/api/v1/projects", handler,
    policy.AuthRequired(authEngine, auth.ModeStrict),
    policy.RequirePerm("project.write"),
    policy.RateLimitWithKeyer(limiter, "projects.create", rule, ratelimit.KeyByUser()),
    policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
        TagSpecs: []cache.CacheTagSpec{
            {Name: "project-list", UserID: true},
        },
    }),
)
```

Policies:
- AuthRequired strict
- RequirePerm for write permission
- Rate limit by user
- CacheInvalidate to bump project-list scope

### Authenticated delete route

Example: `DELETE /api/v1/projects/{id}`

```go
r.Handle(http.MethodDelete, "/api/v1/projects/{id}", handler,
    policy.AuthRequired(authEngine, auth.ModeStrict),
    policy.RequirePerm("project.delete"),
    policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
        TagSpecs: []cache.CacheTagSpec{
            {Name: "project", PathParams: []string{"id"}},
            {Name: "project-list", UserID: true},
        },
    }),
)
```

### With an optional isolation feature

A feature that scopes data adds its isolation policy right after
`AuthRequired`, and its cache key part to the `VaryBy` and tag specs. With the
hypothetical `orgs` feature used in [cache-guide.md](cache-guide.md#key-parts):

```go
r.Handle(http.MethodGet, "/api/v1/projects/{id}", handler,
    policy.AuthRequired(authEngine, auth.ModeStrict),
    orgs.OrgRequired(),
    policy.RateLimitWithKeyer(limiter, "projects.get", rule, orgs.KeyByOrg()),
    policy.CacheRead(cacheMgr, cache.CacheReadConfig{
        TTL:      30 * time.Second,
        TagSpecs: []cache.CacheTagSpec{{Name: "project", PathParams: []string{"id"}}},
        VaryBy:   cache.CacheVaryBy{Parts: []cache.KeyPart{orgs.CacheVary()}, PathParams: []string{"id"}},
    }),
)
```

---

## 10. Common mistakes and how to avoid them

### Putting CacheRead before AuthRequired

```go
// BAD — cache is checked before auth, could serve cached data to unauthenticated users
r.Handle(method, pattern, handler,
    policy.CacheRead(cacheMgr, cfg),
    policy.AuthRequired(authEngine, mode),
)
```

**Fix:** Always put AuthRequired before CacheRead.

### Caching authenticated responses without vary-by

```go
// BAD — all authenticated users share the same cache entry
policy.CacheRead(cacheMgr, cache.CacheReadConfig{
    TTL: 30 * time.Second,
    // No VaryBy.UserID or identity-bearing VaryBy.Parts entry!
})
```

This is now a fail-fast configuration error. Authenticated routes require `VaryBy.UserID` or an identity-bearing `VaryBy.Parts` entry.

### Rate limiting before auth on user-scoped routes

```go
// BAD — rate limit uses anon scope because auth hasn't run yet
r.Handle(method, pattern, handler,
    policy.RateLimit(limiter, ratelimit.Rule{Scope: ratelimit.ScopeUser}),
    policy.AuthRequired(authEngine, mode),
)
```

**Fix:** Auth must come first so the rate limiter can key by user.

### Forgetting CacheInvalidate on write routes

If you cache `GET /api/v1/projects` with `TagSpecs: [{Name:"project-list", UserID:true}]` but forget to add matching `CacheInvalidate` tag specs on writes, list cache stays stale until TTL expires.

### Naming the wrong path parameter in a path-match policy

```go
// Route: /api/v1/orgs/{id}
orgs.OrgMatchFromPath("org_id")  // WRONG — param is "id", not "org_id"
```

**Fix:** Match the chi path parameter name exactly. This applies to any feature policy that reads a path parameter.

### Empty permission lists

```go
policy.RequirePerm()
policy.RequireAnyPerm()
```

Both constructors require at least one non-empty permission and panic on invalid input.


## 11. Required configuration by policy

### 11.1 Auth / RBAC

Required environment:

- `AUTH_ENABLED=true`
- `AUTH_MODE=jwt_only|hybrid|strict`
- `REDIS_ENABLED=true`
- `POSTGRES_ENABLED=true`

If auth is disabled, routes with `AuthRequired` will always return `401`.

### 11.2 Rate limit

Required environment:

- `RATELIMIT_ENABLED=true`
- `REDIS_ENABLED=true`

Optional tuning:

- `RATELIMIT_FAIL_OPEN` (default `true` in non-prod, `false` in prod)
- `RATELIMIT_DEFAULT_LIMIT`
- `RATELIMIT_DEFAULT_WINDOW`

### 11.3 Cache

Required environment:

- `CACHE_ENABLED=true`
- `REDIS_ENABLED=true`

Optional tuning:

- `CACHE_FAIL_OPEN` (default `true` in non-prod, `false` in prod)
- `CACHE_DEFAULT_MAX_BYTES`

## 12. Extensibility guidelines

When adding a new policy:

1. Keep it stateless and constructor-injected.
2. Use centralized envelope responses via `response.Error`.
3. Use typed app error codes from `internal/core/errors/errors.go`.
4. Add focused tests under `internal/core/policy/*_test.go`.
5. Document required env/config and exact failure behavior in this file.
6. A policy that an optional feature owns lives in the feature's package, is registered with `policy.Annotate` (with a `Stage`), and is documented in the feature's own guide instead of here.

This keeps the template copy-paste friendly and production-safe by default.
