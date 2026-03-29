# Policy Reference

Policies are per-route middleware functions applied during route registration. They execute in declaration order (first listed = outermost) and can short-circuit the request by writing a response without calling `next`.

**Type:** `type Policy func(http.Handler) http.Handler`

**File:** `internal/core/policy/policy.go`

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
    policy.AuthRequired(provider, mode),

    // 2. Tenant scope (after auth — needs AuthContext)
    policy.TenantRequired(),

    // 3. Tenant path match (optional — for routes with tenant_id in URL)
    policy.TenantMatchFromPath("tenant_id"),

    // 4. RBAC (after tenant — needs AuthContext)
    policy.RequireRole("admin"),
    // or: policy.RequirePerm("project.write"),
    // or: policy.RequireAnyPerm("project.write", "project.admin"),

    // 5. Rate limit (after auth — so user/tenant scope is available for keying)
    policy.RateLimit(limiter, rule),

    // 6. Cache (innermost — closest to handler)
    policy.CacheRead(cacheMgr, cacheConfig),
    // or for writes:
    policy.CacheInvalidate(cacheMgr, invalidateConfig),
)
```

---

## 2. Auth policies

File: `internal/core/policy/auth.go`

### AuthRequired(provider, mode)

Extracts Bearer token from `Authorization` header, validates it using the auth provider, and injects `AuthContext` into the request context.

```go
policy.AuthRequired(m.auth, m.mode)
```

**Behavior:**
- Missing/empty `Authorization` header → 401 `unauthorized`
- Invalid or non-`Bearer` format → 401 `unauthorized`
- Provider validation failure → 401 `unauthorized`
- Success: `auth.AuthContext` injected into context via `auth.WithContext()`

**Auth modes** (passed to provider):

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

### RequireRole(roles...)

Checks that the authenticated user has one of the specified roles.

```go
policy.RequireRole("admin", "super_admin")
```

**Behavior:**
- No AuthContext in context → 401 `unauthorized`
- Role not in allowed set → 403 `forbidden`
- Role matches any → passes through

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
- Empty perms list → Noop (passes through)

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

Controlled by `RATELIMIT_FAIL_OPEN` (default: `true`).

- **Fail-open (default):** When Redis is unavailable, requests are allowed through. The decision outcome is recorded as `fail_open`.
- **Fail-closed:** When Redis is unavailable, the rate limiter returns an error and the policy responds with 500.

### Retry-After header

When a request is rate-limited (429), the `Retry-After` header is set with the number of seconds until the window resets.

### Production notes

- Rate limit keys use low-cardinality values. Route patterns (not raw URLs), scopes, and sanitized identifiers.
- Bearer tokens are never stored in keys — only a SHA-256 hash prefix (16 hex chars by default).
- If the limiter is nil (rate limiting disabled), the policy becomes a noop.
- Invalid rules (limit <= 0 or window <= 0) result in a noop policy.

---

## 5. Cache policies

File: `internal/core/policy/cache.go`

### CacheRead(manager, config)

Serves cached responses for matching requests and stores responses on cache miss.

```go
policy.CacheRead(cacheMgr, cache.CacheReadConfig{
    TTL:  30 * time.Second,
    Tags: []string{"project"},
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
| `Tags` | `[]string` | — | Invalidation tags included in key (version-bumped on write) |
| `Methods` | `[]string` | `["GET", "HEAD"]` | HTTP methods eligible for caching |
| `CacheStatuses` | `[]int` | `[200]` | HTTP status codes to cache |
| `VaryBy` | `CacheVaryBy` | — | Dimensions that differentiate cache entries |
| `FailOpen` | `*bool` | Global `CACHE_FAIL_OPEN` | Per-route fail-open override |
| `AllowAuthenticated` | `bool` | `false` | Allow caching responses for authenticated requests |

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
2. Check if authenticated caching should be bypassed (see below)
3. Build cache key from route pattern + vary-by dimensions + tag versions
4. Attempt cache GET
   - **Hit:** Serve cached response directly, return
   - **Miss:** Continue to handler
5. Capture handler response
6. If response is cacheable, store in Redis with TTL

**Authenticated caching bypass:**

Responses for authenticated users are **not cached** unless:
- `AllowAuthenticated: true` is set, OR
- `VaryBy.UserID` or `VaryBy.TenantID` is set

This prevents accidentally serving one user's data to another.

**Not cached:**
- Streaming responses (flushed or hijacked)
- Responses larger than MaxBytes
- Responses with `Set-Cookie` header
- Non-matching status codes

### CacheInvalidate(manager, config)

Bumps tag versions after a successful write operation, causing all cached entries using those tags to miss on next read.

```go
policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
    Tags: []string{"project"},
})
```

**Behavior:**
1. Passes request to handler
2. If handler returns a 2xx status code, bumps all configured tags
3. If handler returns non-2xx, no invalidation occurs

**Production notes:**
- Invalidation uses `INCR` on tag version keys (`cver:{env}:{tag}`), which is O(1)
- This is NOT mass key deletion — it's cheap and fast
- Multiple tags can be invalidated in a single Redis pipeline
- If the manager is nil or tags are empty, the policy becomes a noop

### Safe defaults summary

| Default | Value | Why |
|---|---|---|
| Only cache GET/HEAD | Prevent caching side effects | |
| Only cache 200 | Prevent caching error responses | |
| Skip Set-Cookie responses | Prevent session leaks | |
| Skip oversized responses | Prevent Redis memory abuse | |
| Skip authenticated (unless opted in) | Prevent cross-user data leaks | |
| Fail-open on Redis error | Availability over cache | |

---

## 6. Utility policies

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

## 7. Recommended policy stacks by endpoint type

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
    policy.AuthRequired(provider, mode),
    policy.RateLimitWithKeyer(limiter, "whoami", ratelimit.Rule{
        Limit: 30, Window: time.Minute, Scope: ratelimit.ScopeUser,
    }, ratelimit.KeyByUserOrTenantOrTokenHash(16)),
)
```

Policies:
- AuthRequired (hybrid or strict)
- Rate limit by user/token (auth context available after AuthRequired)
- CacheRead only if response is truly user-specific and you set `VaryBy.UserID` or `AllowAuthenticated`

### Tenant-scoped read route

Example: `GET /api/v1/projects/{id}`

```go
r.Handle(http.MethodGet, "/api/v1/projects/{id}", handler,
    policy.AuthRequired(provider, auth.ModeStrict),
    policy.TenantRequired(),
    policy.RateLimitWithKeyer(limiter, "projects.get", rule, ratelimit.KeyByTenant()),
    policy.CacheRead(cacheMgr, cache.CacheReadConfig{
        TTL:                30 * time.Second,
        Tags:               []string{"project"},
        AllowAuthenticated: true,
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
    policy.AuthRequired(provider, auth.ModeStrict),
    policy.TenantRequired(),
    policy.RequirePerm("project.write"),
    policy.RateLimitWithKeyer(limiter, "projects.create", rule, ratelimit.KeyByTenant()),
    policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
        Tags: []string{"project"},
    }),
)
```

Policies:
- AuthRequired strict
- TenantRequired
- RequirePerm for write permission
- Rate limit by tenant
- CacheInvalidate to bump project tag

### Tenant-scoped delete route

Example: `DELETE /api/v1/projects/{id}`

```go
r.Handle(http.MethodDelete, "/api/v1/projects/{id}", handler,
    policy.AuthRequired(provider, auth.ModeStrict),
    policy.TenantRequired(),
    policy.RequirePerm("project.delete"),
    policy.CacheInvalidate(cacheMgr, cache.CacheInvalidateConfig{
        Tags: []string{"project"},
    }),
)
```

---

## 8. Common mistakes and how to avoid them

### Putting CacheRead before AuthRequired

```go
// BAD — cache is checked before auth, could serve cached data to unauthenticated users
r.Handle(method, pattern, handler,
    policy.CacheRead(cacheMgr, cfg),
    policy.AuthRequired(provider, mode),
)
```

**Fix:** Always put AuthRequired before CacheRead.

### Caching authenticated responses without vary-by

```go
// BAD — all authenticated users share the same cache entry
policy.CacheRead(cacheMgr, cache.CacheReadConfig{
    TTL: 30 * time.Second,
    // No VaryBy.TenantID or VaryBy.UserID!
    // No AllowAuthenticated!
})
```

The cache policy will **bypass** (not cache) authenticated responses in this case, which is safe but wasteful. If you want caching for authenticated endpoints, either set `AllowAuthenticated: true` or set `VaryBy.TenantID`/`VaryBy.UserID`.

### Rate limiting before auth on user-scoped routes

```go
// BAD — rate limit uses anon scope because auth hasn't run yet
r.Handle(method, pattern, handler,
    policy.RateLimit(limiter, ratelimit.Rule{Scope: ratelimit.ScopeUser}),
    policy.AuthRequired(provider, mode),
)
```

**Fix:** Auth must come first so the rate limiter can key by user.

### Forgetting CacheInvalidate on write routes

If you cache `GET /api/v1/projects` with tag `"project"` but forget to add `CacheInvalidate` with tag `"project"` on `POST /api/v1/projects`, writes will not invalidate the cached list until TTL expires.

### Using TenantMatchFromPath with wrong param name

```go
// Route: /api/v1/tenants/{id}
policy.TenantMatchFromPath("tenant_id")  // WRONG — param is "id", not "tenant_id"
```

**Fix:** Match the chi path parameter name exactly.

### Empty permission lists

```go
policy.RequirePerm()  // No permissions — still enforces auth (returns 401 if not authenticated)
policy.RequireAnyPerm()  // Empty list — becomes Noop (passes through!)
```

`RequirePerm()` with no args: checks auth context exists but doesn't check any specific permission.
`RequireAnyPerm()` with no args: returns `Noop()` — no check at all.


## 9. Required configuration by policy

### 9.1 Auth / Tenant / RBAC

Required environment:

- `AUTH_ENABLED=true`
- `AUTH_MODE=jwt_only|hybrid|strict`
- `REDIS_ENABLED=true`
- `POSTGRES_ENABLED=true`

If auth is disabled, routes with `AuthRequired` will always return `401`.

### 9.2 Rate limit

Required environment:

- `RATELIMIT_ENABLED=true`
- `REDIS_ENABLED=true`

Optional tuning:

- `RATELIMIT_FAIL_OPEN` (default `true`)
- `RATELIMIT_DEFAULT_LIMIT`
- `RATELIMIT_DEFAULT_WINDOW`

### 9.3 Cache

Required environment:

- `CACHE_ENABLED=true`
- `REDIS_ENABLED=true`

Optional tuning:

- `CACHE_FAIL_OPEN` (default `true`)
- `CACHE_DEFAULT_MAX_BYTES`

## 10. Extensibility guidelines

When adding a new policy:

1. Keep it stateless and constructor-injected.
2. Use centralized envelope responses via `response.Error`.
3. Use typed app error codes from `internal/core/errors/errors.go`.
4. Add focused tests under `internal/core/policy/*_test.go`.
5. Document required env/config and exact failure behavior in this file.

This keeps the template copy-paste friendly and production-safe by default.
