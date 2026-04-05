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

    // 2. Tenant scope (after auth — needs AuthContext)
    policy.TenantRequired(),

    // 3. Tenant path match (optional — for routes with tenant_id in URL)
    policy.TenantMatchFromPath("tenant_id"),

    // 4. RBAC (after tenant — needs AuthContext)
    policy.RequirePerm("project.write"),
    // or: policy.RequireAnyPerm("project.write", "project.admin"),

    // 5. Rate limit (after auth — so user/tenant scope is available for keying)
    policy.RateLimit(limiter, rule),

    // 6. Cache (innermost — closest to handler)
    policy.CacheRead(cacheMgr, cacheConfig),
    // or for writes:
    policy.CacheInvalidate(cacheMgr, invalidateConfig),

    // 7. Browser/proxy cache directives (optional)
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

**Injected AuthContext:**

```go
type AuthContext struct {
    UserID      string   // Always present on success
    TenantID    string   // Present if user has tenant scope
    Role        string   // "user", "admin", etc.
    Permissions []string // e.g., ["system.whoami", "project.write"]
}
```

Reading it in handlers/services:

```go
principal, ok := auth.FromContext(r.Context())
if !ok {
    // Not authenticated (should not happen after AuthRequired policy)
}
```

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

## 3. Tenant policies

File: `internal/core/policy/auth.go`

### TenantRequired()

Ensures the authenticated user has a non-empty `tenant_id` in their AuthContext.

```go
policy.TenantRequired()
```

**Behavior:**
- No AuthContext → 401 `unauthorized`
- AuthContext present but `tenant_id` is empty → 403 `forbidden` ("tenant scope required")
- Tenant present → passes through

**When to use:** For any endpoint that should only be accessible to users who belong to a tenant.

### TenantMatchFromPath(paramName)

Compares the tenant ID from the URL path parameter with the authenticated user's `tenant_id`.

```go
policy.TenantMatchFromPath("tenant_id")
```

**Behavior:**
- No AuthContext → 401 `unauthorized`
- Path param missing or empty → 400 `bad_request`
- User has no `tenant_id` → 403 `forbidden`
- User's `tenant_id` != path param value → **404 `not_found`** (intentional: prevents tenant enumeration)
- Match → passes through

**Mismatch strategy: 404 (not 403)**

Returning 404 instead of 403 on tenant mismatch is a deliberate security decision. If we returned 403, an attacker could enumerate which tenant IDs exist by checking which IDs return 403 vs 404. By returning 404 for both "doesn't exist" and "exists but not yours", we prevent this information leak.

**When to use:** For routes like `/api/v1/tenants/{tenant_id}/projects` where the tenant ID is in the URL and you need to verify the user belongs to that tenant.

### Self-routes (alternative to TenantMatchFromPath)

For routes like `/api/v1/tenants/self` where the tenant ID comes from the auth context (not the URL), use `TenantRequired()` and resolve the tenant ID in the handler:

```go
tenantID, ok := tenant.TenantIDFromContext(r.Context())
```

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
policy.RateLimitWithKeyer(limiter, "projects.list", rule, ratelimit.KeyByTenant())
```

### Scopes and keying strategies

| Scope | Constant | Key based on | When to use |
|---|---|---|---|
| Auto | `ScopeAuto` | User → Tenant → Token hash → Anonymous | Default. Tries the most specific scope available. |
| Anon | `ScopeAnon` | Static "anonymous" | Public endpoints, no identity available |
| IP | `ScopeIP` | Resolved client IP (trusted proxy headers when configured) | Public endpoints where IP is meaningful |
| User | `ScopeUser` | `AuthContext.UserID` | Authenticated endpoints, per-user limits |
| Tenant | `ScopeTenant` | `AuthContext.TenantID` | Tenant-scoped endpoints, shared limit across tenant users |
| Token | `ScopeToken` | SHA-256 hash prefix of Bearer token | When you want per-token limits (e.g., API keys) |

**Built-in keyers:**

- `ratelimit.KeyByIP()` — key by resolved client IP
- `ratelimit.KeyByUser()` — key by user ID from auth context
- `ratelimit.KeyByTenant()` — key by tenant ID from auth context
- `ratelimit.KeyByTokenHash(prefixLen)` — key by token hash prefix
- `ratelimit.KeyByUserOrTenantOrTokenHash(prefixLen)` — cascading: user → tenant → token → anon
- `ratelimit.KeyByAnonymous()` — static "anonymous" key

**Custom keyers** can be provided as `func(r *http.Request) (Scope, string)`.

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
        TenantID:    true,
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
| `TenantID` | `bool` | Include auth tenant id in tag scope |
| `UserID` | `bool` | Include auth user id in tag scope |
| `Literals` | `[]CacheTagLiteral` | Constant key/value dimensions for scope splits |

**CacheVaryBy fields:**

| Field | Type | Description |
|---|---|---|
| `Method` | `bool` | Include HTTP method in key |
| `TenantID` | `bool` | Include tenant ID from AuthContext |
| `UserID` | `bool` | Include user ID from AuthContext |
| `Role` | `bool` | Include role from AuthContext |
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
- `VaryBy.TenantID = true`

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
        {Name: "project-list", TenantID: true},
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
| Authenticated key isolation | Require `VaryBy.UserID` or `VaryBy.TenantID` | Prevent cross-user cache data leaks |
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

- Place `CacheControl(...)` after auth/tenant/rbac/rate-limit/cache policies so it applies consistently to both fresh and cached responses.
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

- Policy order: auth -> tenant -> RBAC -> rate-limit -> cache.
- Auth dependency: RBAC and tenant policies require `AuthRequired`.
- Tenant path safety: routes containing `{tenant_id}` must include `TenantRequired` and `TenantMatchFromPath("tenant_id")`.
- Cache safety: authenticated routes using `CacheRead` must vary by user or tenant.

### Static verification

```bash
go run ./cmd/superapi-verify ./...
# or
make verify
```

### Presets

Use built-in validated presets when possible:

- `policy.TenantRead(...)`
- `policy.TenantWrite(...)`
- `policy.PublicRead(...)`

Example:

```go
r.Handle(http.MethodGet, "/api/v1/projects/{id}", handler,
    policy.TenantRead(
        policy.WithAuthEngine(authEngine, auth.ModeStrict),
        policy.WithLimiter(limiter),
        policy.WithCacheManager(cacheMgr),
        policy.WithCache(30*time.Second, cache.CacheTagSpec{Name: "project", PathParams: []string{"id"}}),
        policy.WithCacheVaryBy(cache.CacheVaryBy{TenantID: true, PathParams: []string{"id"}}),
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
- No auth or tenant policies

### Authenticated route (no tenant)

Example: `GET /api/v1/system/whoami`

```go
r.Handle(http.MethodGet, "/api/v1/system/whoami", handler,
    policy.AuthRequired(authEngine, mode),
    policy.RateLimitWithKeyer(limiter, "whoami", ratelimit.Rule{
        Limit: 30, Window: time.Minute, Scope: ratelimit.ScopeUser,
    }, ratelimit.KeyByUserOrTenantOrTokenHash(16)),
)
```

Policies:
- AuthRequired (hybrid or strict)
- Rate limit by user/token (auth context available after AuthRequired)
- CacheRead only when `VaryBy.UserID` or `VaryBy.TenantID` is set

### Tenant-scoped read route

Example: `GET /api/v1/projects/{id}`

```go
r.Handle(http.MethodGet, "/api/v1/projects/{id}", handler,
    policy.AuthRequired(authEngine, auth.ModeStrict),
    policy.TenantRequired(),
    policy.RateLimitWithKeyer(limiter, "projects.get", rule, ratelimit.KeyByTenant()),
    policy.CacheRead(cacheMgr, cache.CacheReadConfig{
        TTL:                30 * time.Second,
        TagSpecs: []cache.CacheTagSpec{
            {Name: "project", PathParams: []string{"id"}},
        },
        VaryBy: cache.CacheVaryBy{
            TenantID:   true,
            PathParams: []string{"id"},
        },
    }),
)
```

Policies:
- AuthRequired strict (recommended for tenant data)
- TenantRequired
- Rate limit by tenant
- CacheRead with tenant + path param vary

### Tenant-scoped write route

Example: `POST /api/v1/projects`

```go
r.Handle(http.MethodPost, "/api/v1/projects", handler,
    policy.AuthRequired(authEngine, auth.ModeStrict),
    policy.TenantRequired(),
    policy.RequirePerm("project.write"),
    policy.RateLimitWithKeyer(limiter, "projects.create", rule, ratelimit.KeyByTenant()),
    policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
        TagSpecs: []cache.CacheTagSpec{
            {Name: "project-list", TenantID: true},
        },
    }),
)
```

Policies:
- AuthRequired strict
- TenantRequired
- RequirePerm for write permission
- Rate limit by tenant
- CacheInvalidate to bump project-list scope

### Tenant-scoped delete route

Example: `DELETE /api/v1/projects/{id}`

```go
r.Handle(http.MethodDelete, "/api/v1/projects/{id}", handler,
    policy.AuthRequired(authEngine, auth.ModeStrict),
    policy.TenantRequired(),
    policy.RequirePerm("project.delete"),
    policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
        TagSpecs: []cache.CacheTagSpec{
            {Name: "project", PathParams: []string{"id"}},
            {Name: "project-list", TenantID: true},
        },
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
    // No VaryBy.TenantID or VaryBy.UserID!
})
```

This is now a fail-fast configuration error. Authenticated routes require `VaryBy.TenantID` or `VaryBy.UserID`.

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

If you cache `GET /api/v1/projects` with `TagSpecs: [{Name:"project-list", TenantID:true}]` but forget to add matching `CacheInvalidate` tag specs on writes, list cache stays stale until TTL expires.

### Using TenantMatchFromPath with wrong param name

```go
// Route: /api/v1/tenants/{id}
policy.TenantMatchFromPath("tenant_id")  // WRONG — param is "id", not "tenant_id"
```

**Fix:** Match the chi path parameter name exactly.

### Empty permission lists

```go
policy.RequirePerm()
policy.RequireAnyPerm()
```

Both constructors require at least one non-empty permission and panic on invalid input.


## 11. Required configuration by policy

### 11.1 Auth / Tenant / RBAC

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

This keeps the template copy-paste friendly and production-safe by default.
