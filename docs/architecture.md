# Architecture — Under the Hood

This document explains how SuperAPI works internally: request lifecycle, middleware pipeline, policy chain, dependency wiring, error model, and observability flow.

---

## 1. Repository layout

```
cmd/
  api/main.go              # Real server entry point
  migrate/main.go          # Migration CLI
  modulegen/main.go        # Module scaffolder CLI
internal/
  core/
    app/                   # App container, lifecycle, dependency init
      app.go               # App struct, New(), Run(), module registration
      deps.go              # Dependencies struct, initDependencies(), closeDependencies()
    config/config.go       # Env-based config + Lint() validation
    httpx/                 # Router, global middleware, typed JSON, tracing middleware
    response/response.go   # Response envelope + error mapping
    errors/errors.go       # Typed AppError model
    auth/                  # goAuth adapter, AuthContext, Provider interface
    policy/                # Route-level policies (auth, tenant, rate limit, cache, JSON, headers)
    ratelimit/             # Redis-backed limiter + keying strategies
    cache/                 # Redis-backed cache manager + key builder
    db/                    # pgxpool wiring, tx helpers, sqlc query wrappers, migrations
    tenant/tenant.go       # Tenant scope helpers
    logx/logx.go           # zerolog wrapper
    metrics/metrics.go     # Prometheus metrics service
    tracing/service.go     # OpenTelemetry tracing service
    readiness/service.go   # Dependency health check aggregator
    params/params.go       # URL param extraction (chi wrapper)
  modules/
    modules.go             # Centralized module registry
    health/                # Liveness + readiness module
    system/                # System utilities (whoami, parse-duration)
    tenants/               # Tenant CRUD reference module
  devx/modulegen/          # Module generator logic
db/
  migrations/              # Versioned SQL migration files
  schema/                  # Canonical schema for sqlc
  queries/                 # SQL query definitions for sqlc
docs/                      # This documentation
```

---

## 2. Startup sequence

Entry point: `cmd/api/main.go`

```
1. config.Load()          — Read all env vars into Config struct
2. config.Lint()          — Validate config (fail-fast on bad values)
3. logx.New()             — Initialize zerolog logger
4. app.New(cfg, logger, modules.All())
   a. httpx.NewMux()      — Create chi-backed router
   b. initDependencies()  — Wire Postgres, Redis, Metrics, Auth, RateLimit, Cache, Tracing
   c. router.Use(CaptureRoutePattern)  — Register route pattern capture middleware
   d. Register metrics endpoint (/metrics) if enabled
   e. AssembleGlobalMiddleware() — Wrap router with global middleware stack
   f. Wrap with metrics instrumentation (InstrumentHTTP)
   g. Create http.Server with configured timeouts
   h. For each module:
      - If DependencyBinder: call BindDependencies(deps)
      - Call module.Register(router)
5. signal.NotifyContext()  — Listen for SIGINT/SIGTERM
6. app.Run(ctx)           — Start server, wait for shutdown signal
```

### Dependency initialization order (in `initDependencies`)

1. **Readiness service** — always created
2. **Postgres pool** — if `POSTGRES_ENABLED=true`; startup ping with timeout; registered in readiness
3. **Redis client** — if `REDIS_ENABLED=true`; startup ping with timeout; registered in readiness
4. **Metrics service** — if `METRICS_ENABLED=true`; registers Prometheus collectors (including pgxpool if Postgres enabled)
5. **Auth provider** — parse auth mode; if `AUTH_ENABLED=true`, build goAuth engine provider (requires Redis)
6. **Rate limiter** — if `RATELIMIT_ENABLED=true`, create Redis-backed limiter (requires Redis)
7. **Cache manager** — if `CACHE_ENABLED=true`, create Redis-backed cache manager (requires Redis)
8. **Tracing service** — if `TRACING_ENABLED=true`, init OTLP gRPC exporter + tracer provider

If any enabled dependency fails to initialize, all previously created resources are cleaned up and startup fails.

### Shutdown sequence

1. Context cancelled (SIGINT/SIGTERM)
2. `http.Server.Shutdown()` with configured timeout — drains in-flight requests
3. `closeDependencies()`: Redis close → Postgres close → Tracing shutdown → Auth close

---

## 3. Request lifecycle

### 3.1 Global middleware pipeline

Middleware is applied in `AssembleGlobalMiddleware()` (file: `internal/core/httpx/globalmiddleware.go`).

**Execution order (outermost → innermost):**

1. **RequestID** — Reads `X-Request-Id` header or generates a 32-hex-char random ID; injects into context and response header
2. **Recoverer** — Catches panics, logs with structured context (request_id, method, path, panic value), returns sanitized 500
3. **SecurityHeaders** — Sets `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` (if enabled)
4. **MaxBodyBytes** — Limits request body size for POST/PUT/PATCH requests (if configured > 0)
5. **RequestTimeout** — Sets `context.WithTimeout` on the request context (if configured > 0); returns 504 if deadline exceeded before response
6. **Tracing** — Extracts W3C traceparent, creates server span, records attributes and status on defer (if enabled)
7. **AccessLog** — Logs request method, route pattern, status, duration, bytes; sampled by deterministic request-id hash

Then: **Metrics instrumentation** wraps the entire stack (measures in-flight, total requests, duration by method/route/status).

Then: **Router** (chi) matches the route pattern and invokes the handler.

### 3.2 Route-level policy chain

When a route is registered with policies:

```go
r.Handle(http.MethodGet, "/api/v1/resource/{id}", handler,
    policy.AuthRequired(provider, mode),      // P1 (outermost)
    policy.TenantRequired(),                   // P2
    policy.RateLimit(limiter, rule),           // P3
    policy.CacheRead(cacheMgr, cfg),           // P4 (innermost before handler)
)
```

Execution for `[P1, P2, P3, P4]`:

- **Request flow:** P1 → P2 → P3 → P4 → handler
- **Response unwind:** handler → P4 → P3 → P2 → P1

The first policy listed is outermost. Any policy can short-circuit by writing a response and NOT calling `next.ServeHTTP()`.

Implementation: `policy.Chain()` in `internal/core/policy/policy.go` iterates policies in reverse to build the handler chain, so the first policy wraps everything else.

### 3.3 Complete request flow diagram

```
Client request
  │
  ├─ RequestID middleware (assign/propagate X-Request-Id)
  ├─ Recoverer (catch panics → 500)
  ├─ SecurityHeaders (optional response headers)
  ├─ MaxBodyBytes (limit body size for write methods)
  ├─ RequestTimeout (context deadline → 504 on timeout)
  ├─ Tracing (create span, extract traceparent)
  ├─ AccessLog (log on defer: method, route, status, duration)
  ├─ Metrics instrumentation (counters, histograms, in-flight gauge)
  │
  ├─ Chi router (pattern match)
  │   │
  │   ├─ Route policies (auth → tenant → rate limit → cache → ...)
  │   │   │
  │   │   └─ Handler (decode input, call service, write response)
  │   │
  │   ├─ 404 Not Found (no route matched)
  │   └─ 405 Method Not Allowed (route exists, wrong method)
  │
  └─ Response flows back through middleware stack
```

---

## 4. Error model

### 4.1 Typed errors

All application errors use `AppError` (file: `internal/core/errors/errors.go`):

```go
type AppError struct {
    Code       Code    // e.g., "bad_request", "not_found"
    Message    string  // Safe message for clients
    StatusCode int     // HTTP status code
    Details    any     // Optional structured details
    Cause      error   // Internal cause (never exposed to clients)
}
```

**Error codes** (all defined in `internal/core/errors/errors.go`):

| Code | Typical HTTP Status | Usage |
|---|---|---|
| `internal_error` | 500 | Unexpected server errors |
| `bad_request` | 400 | Validation failures, malformed input |
| `not_found` | 404 | Resource not found |
| `method_not_allowed` | 405 | Wrong HTTP method |
| `unauthorized` | 401 | Missing or invalid authentication |
| `forbidden` | 403 | Authenticated but insufficient permissions |
| `too_many_requests` | 429 | Rate limit exceeded |
| `conflict` | 409 | Unique constraint violation |
| `timeout` | 504 | Request deadline exceeded |
| `dependency_unavailable` | 503 | Database/Redis unavailable |

### 4.2 Error mapping (centralized)

File: `internal/core/response/response.go`

The `response.Error()` function handles all error-to-HTTP mapping:

1. **`context.DeadlineExceeded`** → 504 with code `timeout`
2. **`*AppError`** → Uses the error's own `StatusCode`, `Code`, `Message`, and `Details`
3. **Any other error** → 500 with code `internal_error` and generic message `"internal server error"` (internal details never leaked)

### 4.3 Response envelope

All responses use a consistent JSON envelope:

```json
{
  "ok": true,
  "data": { ... },
  "request_id": "abc123..."
}
```

```json
{
  "ok": false,
  "error": {
    "code": "bad_request",
    "message": "name is required",
    "details": null
  },
  "request_id": "abc123..."
}
```

Helper functions: `response.OK()`, `response.Created()`, `response.Error()`, `response.JSON()`.

---

## 5. Dependency wiring

### 5.1 Postgres

File: `internal/core/db/postgres.go`

- Uses `pgxpool` for connection pooling
- Configuration parsed from `POSTGRES_URL`; pool parameters from env vars
- **Startup behavior:** Pool is created, then a startup ping is sent with `POSTGRES_STARTUP_PING_TIMEOUT` (default 3s). If ping fails, startup aborts.
- **Health check:** Registered in readiness service with `POSTGRES_HEALTH_CHECK_TIMEOUT` (default 1s). Called on `/readyz`.
- **When disabled:** `deps.Postgres` is nil; modules that need it check for nil and return `dependency_unavailable` (503).

Pool tuning env vars:

| Env | Default | Description |
|---|---|---|
| `POSTGRES_MAX_CONNS` | 10 | Maximum pool connections |
| `POSTGRES_MIN_CONNS` | 0 | Minimum idle connections |
| `POSTGRES_CONN_MAX_LIFETIME` | 30m | Max connection age |
| `POSTGRES_CONN_MAX_IDLE_TIME` | 5m | Max idle time before close |

### 5.2 Redis

File: `internal/core/cache/redis.go`

- Uses `redis.NewClient` with configured timeouts and pool settings
- **Startup behavior:** Client is created, then startup ping with `REDIS_STARTUP_PING_TIMEOUT` (default 3s). If ping fails, startup aborts.
- **Health check:** Registered in readiness with `REDIS_HEALTH_CHECK_TIMEOUT` (default 1s).
- **Fail-open behaviors:**
  - Rate limiting: If `RATELIMIT_FAIL_OPEN=true` (default), Redis errors allow requests through
  - Caching: If `CACHE_FAIL_OPEN=true` (default), Redis errors bypass cache (request goes to handler)
  - Auth (strict mode): If Redis is unreachable, authentication fails (no fail-open for security)

### 5.3 Readiness signals

File: `internal/core/readiness/service.go`

The readiness service aggregates dependency health checks:

- Each dependency is registered with `Add(name, enabled, timeout, checkFn)`
- `Check(ctx)` returns a `Report` with per-dependency status
- If any enabled dependency check fails → overall status is `not_ready`
- Disabled dependencies report `disabled` (not counted as failures)

**`/readyz` response** (HTTP 200 when ready, 503 when not):

```json
{
  "ok": true,
  "data": {
    "status": "ready",
    "dependencies": {
      "postgres": { "status": "ok" },
      "redis": { "status": "ok" }
    }
  }
}
```

Error messages are sanitized: `context.DeadlineExceeded` becomes `"timeout"`, all others become `"unavailable"`.

### 5.4 Metrics

File: `internal/core/metrics/metrics.go`

Prometheus metrics registered (namespace: `superapi`):

| Metric | Type | Labels | Description |
|---|---|---|---|
| `http_requests_total` | Counter | method, route, status | Total HTTP requests |
| `http_request_duration_seconds` | Histogram | method, route, status | Request latency |
| `http_in_flight_requests` | Gauge | — | Current in-flight requests |
| `rate_limit_requests_total` | Counter | route, outcome | Rate limit decisions |
| `cache_operations_total` | Counter | route, outcome | Cache operations |
| `ready` | Gauge | — | Service readiness (1=ready, 0=not_ready) |
| `dependency_ready` | Gauge | dependency, status | Per-dependency readiness |
| `db_pool_*` | Gauge | — | Postgres pool stats (if enabled) |

Route labels use **low-cardinality route patterns** (e.g., `/api/v1/tenants/{id}`), not raw paths. This prevents metric cardinality explosion.

### 5.5 Tracing

File: `internal/core/tracing/service.go`

- Uses OpenTelemetry SDK with OTLP/gRPC exporter
- Propagation: W3C `traceparent` + `baggage`
- One server span per request, named `METHOD /route/pattern`
- Span attributes: `http.method`, `http.route`, `http.status_code`, `request.id`, `server.address`, `server.port`
- 5xx responses are recorded as span errors
- Sampler options: `always_on`, `always_off`, `traceidratio` (default: `traceidratio` at 5%)
- If OTLP endpoint is unreachable, API still starts; spans are best-effort

### 5.6 Access logging

File: `internal/core/httpx/accesslog.go`

- Structured JSON logs via zerolog
- **Sampling:** Deterministic hash of request_id against configured sample rate (default 5%). Same request_id always produces the same sample decision.
- **Always logged regardless of sampling:** 5xx responses, requests exceeding slow threshold
- **Excluded paths:** `/healthz`, `/readyz`, `/metrics` by default (configurable)
- **Route labels:** Uses captured route patterns, not raw URL paths
- **Log level:** Info for normal, Warn for 4xx or slow, Error for 5xx
- **Security:** Request bodies, Authorization headers, Cookie headers, and query strings are NOT logged

---

## 6. Module system

### 6.1 Module interface

File: `internal/core/app/app.go`

```go
type Module interface {
    Name() string
    Register(r httpx.Router) error
}
```

Optional interface for receiving dependencies:

```go
type DependencyBinder interface {
    BindDependencies(*Dependencies)
}
```

### 6.2 Module lifecycle

1. Module is listed in `internal/modules/modules.go` (`All()` function)
2. During `app.New()`:
   a. If the module implements `DependencyBinder`, `BindDependencies(deps)` is called
   b. `Register(router)` is called — module registers routes via `router.Handle()`
3. Module is active for the lifetime of the server

### 6.3 Dependency injection

Modules receive dependencies through `BindDependencies(*app.Dependencies)`. The `Dependencies` struct provides:

| Field | Type | Description |
|---|---|---|
| `Postgres` | `*pgxpool.Pool` | Nil if Postgres disabled |
| `Redis` | `*redis.Client` | Nil if Redis disabled |
| `Readiness` | `*readiness.Service` | Always available |
| `Metrics` | `*metrics.Service` | Always available (may be disabled internally) |
| `Tracing` | `*tracing.Service` | Always available (may be disabled internally) |
| `Auth` | `auth.Provider` | DisabledProvider if auth disabled |
| `AuthMode` | `auth.Mode` | Configured auth mode |
| `RateLimit` | `config.RateLimitConfig` | Rate limit configuration values |
| `Cache` | `config.CacheConfig` | Cache configuration values |
| `Limiter` | `ratelimit.Limiter` | Nil if rate limiting disabled |
| `CacheMgr` | `*cache.Manager` | Nil if caching disabled |

---

## 7. sqlc and transaction helpers

### 7.1 Query access

File: `internal/core/db/queries.go`

```go
// From pool (non-transactional reads)
q := db.NewQueries(pool)

// From transaction
q := db.QueriesFrom(tx)
q := db.QueriesFromTx(pgxTx)
```

The key insight: `sqlcgen.Queries` accepts `DBTX` interface, which both `*pgxpool.Pool` and `pgx.Tx` satisfy. Repositories always receive `*sqlcgen.Queries` and never care whether they're inside a transaction or not.

### 7.2 Transaction helpers

File: `internal/core/db/tx.go`

**`WithTx`** — for write operations that don't return a value:

```go
err := db.WithTx(ctx, pool, func(q *sqlcgen.Queries) error {
    _, err := q.CreateTenant(ctx, params)
    return err
})
```

**`WithTxResult`** — for write operations that return a value:

```go
tenant, err := db.WithTxResult(ctx, pool, func(q *sqlcgen.Queries) (Tenant, error) {
    row, err := q.CreateTenant(ctx, params)
    if err != nil {
        return Tenant{}, err
    }
    return fromRow(row), nil
})
```

**Transaction semantics:**
- Begin → run callback → commit on success / rollback on error
- On panic: rollback (best effort), then re-panic
- Rollback failures are intentionally swallowed (do not mask callback error)
- Commit failure is returned when callback succeeds but commit fails

### 7.3 Repository pattern

Repositories accept `*sqlcgen.Queries` and perform data access only. They never start transactions.

```go
type repository struct {
    q *sqlcgen.Queries
}

func NewRepository(q *sqlcgen.Queries) Repository {
    return &repository{q: q}
}
```

Services own transaction boundaries:

```go
type service struct {
    pool *pgxpool.Pool
    repo Repository
}

// Read path: repo uses queries bound to pool directly
func (s *service) GetByID(ctx context.Context, id string) (Tenant, error) {
    return s.repo.GetByID(ctx, id)
}

// Write path: service wraps in transaction
func (s *service) Create(ctx context.Context, req Request) (Result, error) {
    return db.WithTxResult(ctx, s.pool, func(q *sqlcgen.Queries) (Result, error) {
        return NewRepository(q).Create(ctx, input)
    })
}
```

---

## 8. Typed JSON handler adapter

File: `internal/core/httpx/typedjson.go`

The `httpx.JSON[Req, Resp]()` adapter converts a typed function into an `http.Handler`:

```go
func JSON[Req any, Resp any](fn JSONHandlerFunc[Req, Resp]) http.Handler
```

Where `JSONHandlerFunc` is:

```go
type JSONHandlerFunc[Req any, Resp any] func(ctx context.Context, req Req) (Resp, error)
```

**What it does automatically:**

1. Limits body to 1 MiB
2. Decodes JSON with `DisallowUnknownFields()` (strict)
3. Rejects multiple JSON values in body
4. Calls `Validate()` if request implements `Validatable` interface
5. On success: writes `response.OK(w, resp, requestID)`
6. On error: maps through `response.Error()` (AppError passthrough, internal sanitized)

**Decode error mapping:**

| Condition | AppError |
|---|---|
| Empty body | `bad_request`: "request body is required" |
| Body too large | `bad_request`: "request body too large" |
| Syntax error | `bad_request`: "malformed JSON body" |
| Type mismatch | `bad_request`: "invalid JSON field type" |
| Unknown field | `bad_request`: "unknown field in request body" |
| Multiple objects | `bad_request`: "request body must contain a single JSON object" |

---

## 9. Production notes

### Failure modes

| Dependency down | Behavior |
|---|---|
| Postgres unreachable at startup | **Startup fails** (fail-fast) |
| Postgres unreachable at runtime | `/readyz` returns 503; DB-dependent endpoints return 503 (`dependency_unavailable`) |
| Redis unreachable at startup | **Startup fails** (fail-fast) if Redis is enabled |
| Redis unreachable at runtime | Rate limiting: fail-open by default (requests pass through). Caching: fail-open by default (bypass cache). Auth (strict mode): fails closed (401). `/readyz` returns 503. |
| Tracing endpoint unreachable | Startup succeeds; spans are lost silently |

### Security considerations

- Internal errors are never leaked to clients (centralized sanitization in `response.Error()`)
- Bearer tokens are never stored in rate limit keys (SHA-256 hash prefix only)
- Request bodies and auth headers are never logged
- Cache keys never contain raw tokens, IPs, or full query strings
- `Set-Cookie` responses are never cached
- Authenticated responses are bypassed from cache unless explicitly configured with vary-by dimensions
- Tenant mismatch returns 404 (not 403) to prevent tenant enumeration

### Cardinality safety

- Route labels in metrics and logs use chi route patterns (e.g., `/api/v1/tenants/{id}`), not raw paths
- Rate limit keys use low-cardinality scopes (`ip`, `user`, `tenant`, `token`, `anon`)
- Cache keys use route pattern + selected vary dimensions + query param hash — not raw URLs
- Access logs are sampled by default (5%) with always-log exceptions for errors and slow requests
