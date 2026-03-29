# Cache Guide

Deep guide to the cache subsystem — key construction, tag-based invalidation, policy integration, and production tuning.

---

## Architecture overview

The cache layer has three components:

| Component | File | Role |
|---|---|---|
| **Manager** | `internal/core/cache/manager.go` | Key building, Redis get/set, tag version management |
| **CacheRead policy** | `internal/core/policy/cache.go` | Per-route middleware that serves/stores cached responses |
| **CacheInvalidate policy** | `internal/core/policy/cache.go` | Per-route middleware that bumps tags after writes |

Caching is per-route and opt-in. You attach cache policies to individual routes in your module's `routes.go`. There is no automatic caching.

---

## Enabling the cache

**Required env vars:**

```
CACHE_ENABLED=true
REDIS_ENABLED=true    # Cache requires Redis
```

**Optional tuning:**

| Env var | Default | Description |
|---|---|---|
| `CACHE_DEFAULT_MAX_BYTES` | `262144` (256 KiB) | Max response body size to store |
| `CACHE_FAIL_OPEN` | `true` | Pass through on Redis errors |

Notes:
- Route TTL is configured per-route via `CacheReadConfig.TTL` (there is no global `CACHE_DEFAULT_TTL` env var).
- Key prefixes are currently fixed in code as `cache` (read keys) and `cver` (tag version keys).

---

## Key construction

Every cached response has a unique Redis key built from the route pattern and vary-by dimensions.

### Key format

```
cache:{env}:{route_pattern}:{content_hash}
```

Example:

```
cache:prod:/api/v1/projects/{id}:a3f8b2c1e9d045...
```

### Content hash (vary-by fingerprint)

The content hash is a SHA-256 of a canonical string built from the vary-by config. The canonical string is constructed by `BuildReadKey()` in [internal/core/cache/manager.go](internal/core/cache/manager.go).

**Canonical string components** (in order, separated by `|`):

1. `method={METHOD}` — if `VaryBy.Method` is true
2. `tenant={TENANT_ID}` — if `VaryBy.TenantID` is true
3. `user={USER_ID}` — if `VaryBy.UserID` is true
4. `role={ROLE}` — if `VaryBy.Role` is true
5. `path:{name}={value}` — for each `VaryBy.PathParams` entry, sorted by name
6. `query:{name}={value}` — for each `VaryBy.QueryParams` entry, sorted by name
7. `header:{name}={value}` — for each `VaryBy.Headers` entry, sorted by name (lowercase)
8. `tags:{tag1}={ver1},{tag2}={ver2}` — tag version tokens for all `Tags`

Tag versions are fetched from Redis (`MGET cver:{env}:{tag1} cver:{env}:{tag2} ...`). Missing tags get version `"0"`.

### Why tag versions are part of the key

When a tag version changes (via `CacheInvalidate`), the canonical string changes, which produces a different SHA-256 hash, which maps to a different Redis key. The old cached entry simply expires via TTL — no explicit deletion needed.

This is the core invalidation mechanism: **bump the tag version → cache key changes → automatic miss**.

---

## Tag-based invalidation

### How it works

1. `CacheRead` includes tag versions in the key hash.
2. `CacheInvalidate` runs `INCR` on tag version keys after successful writes.
3. Next `CacheRead` request sees a new tag version → different hash → cache miss → fresh response.

### Tag naming conventions

Use a simple noun for the entity:

```go
Tags: []string{"project"}           // Single entity
Tags: []string{"project", "team"}   // Cross-entity invalidation
```

Do NOT use route-specific tags like `"project-list"` or `"project-detail"`. The version bump should affect all cached entries for that entity.

### Tag version key format

```
cver:{env}:{tag_name}
```

Example: `cver:prod:project`

### Tag lifecycle

| Event | What happens |
|---|---|
| First CacheRead with tag `X` | MGET `cver:{env}:X` → miss, version defaults to `"0"` |
| First CacheInvalidate with tag `X` | INCR `cver:{env}:X` → key created with value `1` |
| Subsequent invalidation | INCR → value `2`, `3`, etc. |
| Tag version overflow | Version continues incrementing. Redis INCR returns up to 2^63-1. |
| Old cached entries | Expire via TTL. No explicit cleanup needed. |

---

## VaryBy strategies

### Choosing the right dimensions

| Endpoint pattern | VaryBy config | Why |
|---|---|---|
| `GET /api/v1/settings` (global) | `{}` (empty) | Same response for everyone |
| `GET /api/v1/projects` (tenant list) | `TenantID: true, QueryParams: ["limit","cursor"]` | Different data per tenant, paginated |
| `GET /api/v1/projects/{id}` (detail) | `TenantID: true, PathParams: ["id"]` | Specific item per tenant |
| `GET /api/v1/projects/self` (self) | `TenantID: true` | Tenant's own scoped data |
| `GET /api/v1/users/me` (self) | `UserID: true` | User's own data |
| `GET /api/v1/projects?status=active` | `TenantID: true, QueryParams: ["status","limit","cursor"]` | Filtered + paginated |

### Common mistakes

**Missing TenantID vary** — Tenant A's data served to Tenant B:
```go
// BAD
VaryBy: cache.CacheVaryBy{PathParams: []string{"id"}}
// GOOD
VaryBy: cache.CacheVaryBy{TenantID: true, PathParams: []string{"id"}}
```

**Too many query params** — Low cache hit rate:
```go
// BAD — every unique combination of all params creates a new cache entry
VaryBy: cache.CacheVaryBy{QueryParams: []string{"limit","cursor","sort","order","filter","q"}}
// BETTER — only include params that meaningfully change the result
VaryBy: cache.CacheVaryBy{QueryParams: []string{"limit","cursor"}}
```

**VaryBy headers with high cardinality** — `Accept-Language` or `User-Agent` will create a cache entry per unique header value. Use sparingly.

---

## Practical examples

### Caching a tenant-scoped list

```go
// routes.go
func (m *Module) registerRoutes(r httpx.Router) {
    r.Handle(http.MethodGet, "/api/v1/projects", m.handler.List,
        policy.AuthRequired(m.auth, m.mode),
        policy.TenantRequired(),
        policy.CacheRead(m.cacheMgr, cache.CacheReadConfig{
            TTL:                30 * time.Second,
            Tags:               []string{"project"},
            AllowAuthenticated: true,
            VaryBy: cache.CacheVaryBy{
                TenantID:    true,
                QueryParams: []string{"limit", "cursor"},
            },
        }),
    )
}
```

### Caching a detail endpoint

```go
r.Handle(http.MethodGet, "/api/v1/projects/{id}", m.handler.GetByID,
    policy.AuthRequired(m.auth, m.mode),
    policy.TenantRequired(),
    policy.CacheRead(m.cacheMgr, cache.CacheReadConfig{
        TTL:                60 * time.Second,
        Tags:               []string{"project"},
        AllowAuthenticated: true,
        VaryBy: cache.CacheVaryBy{
            TenantID:   true,
            PathParams: []string{"id"},
        },
    }),
)
```

### Invalidating on create

```go
r.Handle(http.MethodPost, "/api/v1/projects", m.handler.Create,
    policy.AuthRequired(m.auth, m.mode),
    policy.TenantRequired(),
    policy.RequirePerm("project.write"),
    policy.CacheInvalidate(m.cacheMgr, cache.CacheInvalidateConfig{
        Tags: []string{"project"},
    }),
)
```

### Invalidating on update or delete

```go
r.Handle(http.MethodPut, "/api/v1/projects/{id}", m.handler.Update,
    policy.AuthRequired(m.auth, m.mode),
    policy.TenantRequired(),
    policy.RequirePerm("project.write"),
    policy.CacheInvalidate(m.cacheMgr, cache.CacheInvalidateConfig{
        Tags: []string{"project"},
    }),
)

r.Handle(http.MethodDelete, "/api/v1/projects/{id}", m.handler.Delete,
    policy.AuthRequired(m.auth, m.mode),
    policy.TenantRequired(),
    policy.RequirePerm("project.delete"),
    policy.CacheInvalidate(m.cacheMgr, cache.CacheInvalidateConfig{
        Tags: []string{"project"},
    }),
)
```

### Cross-entity invalidation

If updating a team also affects project listings:

```go
// On team update
policy.CacheInvalidate(m.cacheMgr, cache.CacheInvalidateConfig{
    Tags: []string{"team", "project"},  // Bumps both tags
})
```

---

## TTL guidance

| Endpoint type | Suggested TTL | Rationale |
|---|---|---|
| Tenant config / settings | 60–120s | Low write frequency, high read frequency |
| Entity list (paginated) | 15–30s | Moderate churn, invalidation handles most staleness |
| Entity detail | 30–60s | Stable between writes, invalidation handles updates |
| Self/me endpoints | 10–30s | Per-user, changes on profile update |
| Public/static data | 120–300s | Rarely changes |

These are starting points. Monitor cache hit rates and adjust.

---

## Fail-open behavior

When Redis is unavailable:

- **Fail-open (default):** Request bypasses cache entirely — runs handler directly. No error returned.
- **Fail-closed:** Returns 500 to the client.

Per-route override:

```go
failClosed := false
policy.CacheRead(cacheMgr, cache.CacheReadConfig{
    TTL:      30 * time.Second,
    FailOpen: &failClosed,  // Override global setting for this route
})
```

---

## What is NOT cached

The cache policy will skip storing a response if:

1. HTTP method is not in `Methods` (default: GET, HEAD)
2. Response status is not in `CacheStatuses` (default: 200)
3. Response body exceeds `MaxBytes` (default: 256 KiB)
4. Response includes `Set-Cookie` header
5. Request is authenticated and neither `AllowAuthenticated` nor `VaryBy.UserID`/`VaryBy.TenantID` is set
6. The response writer was hijacked (WebSocket upgrade) or flushed (streaming)

---

## Monitoring and debugging

### Cache hit/miss

The cache policy sets an `X-Cache` response header:

- `HIT` — served from cache
- `MISS` — fetched from handler, stored in cache
- `BYPASS` — caching skipped (method, auth, error, etc.)

### Redis key inspection

To see what's cached:

```bash
redis-cli KEYS "cache:dev:*"
redis-cli KEYS "cver:dev:*"
```

To check a tag version:

```bash
redis-cli GET "cver:dev:project"
```

To force-invalidate a tag manually:

```bash
redis-cli INCR "cver:dev:project"
```

### Key space cleanup

Old cache entries expire via TTL. There is no garbage collection. If you need to flush all cache entries:

```bash
redis-cli KEYS "cache:dev:*" | xargs redis-cli DEL
```

Or use `FLUSHDB` in development.
