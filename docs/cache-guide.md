# Cache Guide

Deep guide to Redis-backed route cache behavior: key building, dynamic tag specs, scoped invalidation, and performance tuning.

---

## Architecture overview

The cache subsystem has three main pieces:

| Component | File | Role |
|---|---|---|
| **Manager** | `internal/core/cache/manager.go` | Key build, Redis get/set, tag version token fetch, tag version bumps |
| **CacheRead policy** | `internal/core/policy/cache.go` | Route middleware for read-through cache |
| **CacheInvalidate policy** | `internal/core/policy/cache.go` | Route middleware for scoped version bumps after successful writes |

Caching is route-level and opt-in.

---

## Enabling cache

Required:

```bash
CACHE_ENABLED=true
REDIS_ENABLED=true
```

Optional tuning:

| Env var | Default | Description |
|---|---|---|
| `CACHE_DEFAULT_MAX_BYTES` | `262144` | Max response body bytes stored when route `MaxBytes` is not set |
| `CACHE_FAIL_OPEN` | `true` in non-prod, `false` in prod | Bypass cache on Redis failure (or fail request when false) |
| `CACHE_TAG_VERSION_CACHE_TTL` | `250ms` | Process-local TTL for cached tag version tokens |

Notes:
- TTL is route-level (`CacheReadConfig.TTL`).
- Read key prefix is `cache:{env}:...`.
- Tag version key prefix is `cver:{env}:...`.

---

## Key construction

### Final Redis key format

```text
cache:{env}:{route_part}:{short_hash}
```

Example:

```text
cache:prod:/api/v1/projects/{id}:2df708dc47c207792eaf2cf732445d75
```

### Canonical string

The hash part is computed from a canonical string assembled in manager code. The canonical string includes selected VaryBy dimensions plus tag version token.

Canonical parts are appended in deterministic order:

1. `route=...`
2. `method=...` when `VaryBy.Method`
3. `tenant=...` when `VaryBy.TenantID`
4. `user=...` when `VaryBy.UserID`
5. `role=...` when `VaryBy.Role`
6. `path.{name}=...` for configured path params
7. `header.{name}=...` for configured headers
8. `query_hash=...` for configured query params
9. `auth=allowed` when principal exists and `AllowAuthenticated` is true
10. `tags=...` token from resolved tag specs and Redis versions

Then:
- `SHA-256(canonical)` is computed
- first 16 bytes are hex-encoded as `short_hash`

---

## Dynamic tag specs

Static tag arrays were replaced with structured `TagSpecs`.

### CacheRead config

```go
TagSpecs []cache.CacheTagSpec
```

### CacheInvalidate config

```go
TagSpecs []cache.CacheTagSpec
```

### Tag spec shape

```go
type CacheTagSpec struct {
    Name       string
    PathParams []string
    TenantID   bool
    UserID     bool
    Literals   []cache.CacheTagLiteral
}
```

V1 supports only:
- path params
- auth tenant id
- auth user id
- literal key/value dimensions

Query/header-derived tag params are intentionally blocked in v1 to avoid cardinality explosion.

---

## Why tags exist when VaryBy already exists

- `VaryBy` defines **who gets separate cache entries**.
- `TagSpecs` define **which entries get invalidated together after writes**.

No data bleed is handled by `VaryBy`.
Freshness on writes is handled by tag version bumps.

---

## Invalidation lifecycle (bump-miss)

1. Read route computes effective tag names from `TagSpecs` and request context.
2. Manager fetches current versions from Redis (`MGET cver:{env}:{tag}`) and includes token in key hash input.
3. Write route succeeds (2xx), `CacheInvalidate` resolves effective tag names and calls `INCR` per tag version key.
4. Next read sees changed tag version token, canonical string changes, key hash changes, cache miss occurs, fresh value is stored.

This is called **bump-miss invalidation**.

---

## Tag naming and scope strategy

Use precise scopes to avoid over-invalidation.

### Recommended patterns

| Route type | Recommended tag spec |
|---|---|
| Detail endpoint | `Name: "project", PathParams: ["id"]` |
| Tenant list endpoint | `Name: "project-list", TenantID: true` |
| User self endpoint | `Name: "user-profile", UserID: true` |
| Cross-entity list | `Name: "dashboard-list", TenantID: true, Literals: [{Key:"view",Value:"summary"}]` |

### Write route best practice

When write can affect both detail and list responses, bump both scopes.

Example for project update:

```go
policy.CacheInvalidate(m.cacheMgr, cache.CacheInvalidateConfig{
    TagSpecs: []cache.CacheTagSpec{
        {Name: "project", PathParams: []string{"id"}},
        {Name: "project-list", TenantID: true},
    },
})
```

This invalidates the updated project detail and tenant list keys without evicting unrelated projects from other ids.

---

## Practical examples

### Tenant project list read cache

```go
policy.CacheRead(m.cacheMgr, cache.CacheReadConfig{
    TTL: 30 * time.Second,
    TagSpecs: []cache.CacheTagSpec{
        {Name: "project-list", TenantID: true},
    },
    VaryBy: cache.CacheVaryBy{
        TenantID:    true,
        QueryParams: []string{"limit", "cursor"},
    },
})
```

### Project detail read cache

```go
policy.CacheRead(m.cacheMgr, cache.CacheReadConfig{
    TTL: 60 * time.Second,
    TagSpecs: []cache.CacheTagSpec{
        {Name: "project", PathParams: []string{"id"}},
    },
    VaryBy: cache.CacheVaryBy{
        TenantID:   true,
        PathParams: []string{"id"},
    },
})
```

### Project update invalidation (detail + list)

```go
policy.CacheInvalidate(m.cacheMgr, cache.CacheInvalidateConfig{
    TagSpecs: []cache.CacheTagSpec{
        {Name: "project", PathParams: []string{"id"}},
        {Name: "project-list", TenantID: true},
    },
})
```

### User profile cache with user-scoped tag

```go
policy.CacheRead(m.cacheMgr, cache.CacheReadConfig{
    TTL: 30 * time.Second,
    TagSpecs: []cache.CacheTagSpec{
        {Name: "user-profile", UserID: true},
    },
    VaryBy: cache.CacheVaryBy{UserID: true},
    AllowAuthenticated: true,
})
```

---

## What is not cached

Cache write is bypassed when:

1. Method is not allowed (`Methods`, default GET/HEAD)
2. Status is not cacheable (`CacheStatuses`, default 200)
3. Body exceeds `MaxBytes`
4. Response has `Set-Cookie`
5. Response is streaming/hijacked

Auth safety rule still applies:
- authenticated route cache requires `VaryBy.UserID` or `VaryBy.TenantID`.

---

## Fail-open behavior

On Redis failures:

- fail-open: bypass cache and continue handler
- fail-closed: return dependency-unavailable response

In prod/prodution-like environments, startup lint rejects `CACHE_FAIL_OPEN=true` when cache is enabled.

Per-route override:

```go
failOpen := false
policy.CacheRead(m.cacheMgr, cache.CacheReadConfig{
    TTL: 30 * time.Second,
    FailOpen: &failOpen,
})
```

---

## Monitoring and debugging

Cache outcomes emitted as metrics labels:
- hit
- miss
- set
- bypass
- error

Inspect keys:

```bash
redis-cli KEYS "cache:dev:*"
redis-cli KEYS "cver:dev:*"
```

Check one version key:

```bash
redis-cli GET "cver:dev:project|path.id=proj_123"
```

Force a manual bump:

```bash
redis-cli INCR "cver:dev:project|path.id=proj_123"
```

---

## Performance notes

- static key parts and normalized tag specs are prepared at route registration
- route label is memoized per resolved route pattern in policy runtime
- tag version tokens are cached in-process for `CACHE_TAG_VERSION_CACHE_TTL`
- successful bump clears process-local token cache immediately

Tune `CACHE_TAG_VERSION_CACHE_TTL` low for very high-cardinality dynamic tags.
