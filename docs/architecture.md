# Architecture — API Template (SaaS, 10k RPS, Security-first)

This document defines the final architecture for a reusable Go API template optimized for:
- **~10,000 RPS on a single instance** for “light” endpoints (routing + auth + rate limits + cache lookups, minimal IO),
- **security-first defaults**,
- **module extensibility** with minimal boilerplate,
- **feature toggles / addons via config** (like goAuth),
- **route-level control** for auth/rate-limit/cache policies.

---

## 1) Hard Choices (selected for performance)

### HTTP stack
- **net/http + chi**
  - `chi` is minimal, fast, and plays perfectly with Go’s standard server and middleware model.
  - Keeps compatibility with observability tooling and avoids the operational edge cases of non-standard servers.
  - 10k RPS is fully achievable with net/http when the hot path is allocation-light and IO is controlled.

### JSON
- Default: **encoding/json** (simplicity + correctness)
- Architecture keeps an encoder abstraction boundary so you can swap to a faster encoder later **without changing handlers**.

### Database
- **Postgres + pgx v5 (pgxpool) + sqlc**
  - `pgxpool` is high-performance and stable.
  - `sqlc` gives type-safe queries at compile-time and avoids reflection overhead typical of ORMs.
  - This is the best “performance + correctness + maintainability” combo for SaaS templates.

### Redis
- **go-redis v9**
  - Used for: caching, rate limiting, (optionally) distributed locks, job queues, and (via goAuth) session/state.

---

## 2) Goals & Non-goals

### Goals
- **Performance:** 10k RPS single instance for light endpoints; stable tail latency via backpressure.
- **Security-first:** safe defaults, strict input validation, clean error boundaries, secure headers, least-privilege policies.
- **Extensibility:** adding a module should not require changing core server wiring.
- **Config-driven addons:** enable/disable tracing, audit, caching, strict auth mode, etc.
- **Operational hygiene:** readiness/health, metrics, structured logs, graceful shutdown.

### Non-goals (v1)
- Solving “10k RPS of heavy DB writes” purely via code (DB-heavy workloads require caching, queuing, replicas, and query/index work).
- Dynamic module loading at runtime (Go builds are static).

---

## 3) Service SLO Targets

Assuming healthy dependencies in the same region/VPC and endpoints are not doing heavy work per request:

### Light endpoints (auth/rate-limit/cache lookups)
- p50: **10–20ms**
- p95: **50–80ms**
- p99: **120–200ms**

### DB read endpoints (indexed reads, limited payload)
- p50: **15–30ms**
- p95: **70–130ms**
- p99: **150–300ms**

### DB write endpoints (transactional)
- p50: **30–60ms**
- p95: **120–200ms**
- p99: **250–400ms**

---

## 4) Repository Layout

Recommended layout:

/cmd/api/main.go

/internal/core/
  app/               # App container, lifecycle, startup/shutdown
  config/            # config structs + loader + lint
  httpx/             # router setup, middleware stack, policies
  response/          # response envelope + writers
  errors/            # typed errors + mapper to HTTP
  auth/              # adapter over goAuth + policies
  ratelimit/         # limiter + policies (redis/inmem)
  cache/             # redis cache client + policies (per-route cache)
  db/                # pgxpool wiring + migrations hook + tx helpers
  observability/     # logger, metrics, tracing hooks, audit hooks
  validate/          # decoding + validation utilities

/internal/modules/
  health/
  users/
  billing/
  ...

/docs/
  architecture.md
  module_guide.md
  runbook.md

---

## 5) Core Concepts

### 5.1 Kernel vs Modules
- **Kernel (core):** stable infrastructure and cross-cutting concerns (auth, rate limit, cache, errors, observability).
- **Modules:** business features (users, billing, projects, etc.), implemented as plug-ins via a small interface.

### 5.2 Module internal layering
Every module follows the same pattern:

**routes → handlers → service → repo**

- **routes:** endpoints + route policies
- **handlers:** decode/validate; call service
- **service:** business logic; orchestration; transactions
- **repo:** DB/Redis access only (no business rules)

This makes modules predictable and easy to scaffold.

---

## 6) Module System

### 6.1 Module Interface
A minimal stable contract:

- `Name() string`
- `Register(r Router, deps *Deps)` — define routes + attach policies
- Optional:
  - `Init(deps *Deps) error` (pre-flight checks)
  - `Start(ctx) error` / `Stop(ctx) error` (background workers, subscriptions)

### 6.2 Module registration
Baseline: explicit registry `internal/modules/registry.go`:
`var All = []core.Module{ users.New(), billing.New(), ... }`

Optional later: `go generate` that scans module folders and generates the registry.

---

## 7) Dependency Container (Deps)

Modules do not create infra clients. They receive stable dependencies:

- `Cfg *config.Config`
- `Logger`
- `Metrics`
- `DB *pgxpool.Pool` + `Tx` helper
- `Redis *redis.Client`
- `Auth *auth.Provider` (goAuth adapter)
- `Limiter *ratelimit.Limiter`
- `Cache *cache.Manager`
- `Validator`
- `Clock`, `IDGen`

Rule: **Deps must remain stable**. Add new dependencies carefully; prefer optional interfaces to avoid template churn.

---

## 8) Request Lifecycle

### 8.1 End-to-end flow
1. Accept request (net/http)
2. Attach context (deadline, request-id)
3. Global middleware (minimal hot path)
4. Route match (chi)
5. Route policies executed (auth/rate-limit/cache/validation/tenant)
6. Decode input (path/query/json)
7. Validate DTO
8. Call service
9. Map typed errors → HTTP
10. Write JSON response (consistent envelope)

### 8.2 Response envelope
Success:
```json
{ "ok": true, "data": {...}, "meta": {...optional} }
````

Error:

```json
{ "ok": false, "error": { "code": "...", "message": "...", "details": {...} }, "request_id": "..." }
```

---

## 9) Policy-First Routing (the extensibility engine)

Routes declare policies rather than hand-wiring middleware stacks everywhere.

### 9.1 Core policies (always available)

* `Public()`
* `AuthRequired(mode)` where mode ∈ { jwt-only, hybrid, strict }
* `RequirePerm(mask)` / `RequireRole(name)`
* `RateLimit(name, rule)`
* `Validate()` (auto DTO validation)
* `TenantScope()` (optional)
* `CacheRead(cfg)` / `CacheInvalidate(tags...)` (see caching section)

This keeps route addition simple:

* define DTO
* define handler
* register route with policies

---

## 10) Security Architecture (highest priority)

### 10.1 Secure-by-default configuration

Startup runs config lint:

* missing secrets, weak cookie settings, unsafe CORS, disabled throttles, etc. are warned/blocked.
* Production can be configured to **fail-fast** on high severity lint.

### 10.2 Authentication

Auth is provided via **goAuth** behind a core adapter:

* route-level selection of auth mode (jwt-only/hybrid/strict)
* strict routes can fail closed when Redis is unavailable (configurable status behavior)

### 10.3 Authorization

* Permission masks (O(1) checks)
* Optional tenant policy to prevent cross-tenant reads/writes
* Explicit “admin” scopes if needed (never implied)

### 10.4 Input safety

* request body size limit
* strict JSON parsing option (disallow unknown fields for public APIs)
* validation required for every DTO
* consistent error mapping (no stack traces to clients)

### 10.5 Abuse resistance

* global limiter defaults (IP + token/user)
* dedicated buckets for auth flows (login/refresh/reset)
* incremental lockouts / penalties handled by auth module + policies

### 10.6 Audit hooks (optional addon)

* async bounded queue, non-blocking
* critical actions emit audit events (auth changes, role changes, billing actions)

---

## 11) Redis Integration (Policies for per-route caching & control)

Redis is integrated as an **infra dependency**, but caching behavior is controlled via **route policies** so you can configure:

* which routes are cached,
* TTLs and stale strategy,
* cache keys (per tenant/user/query),
* invalidation tags.

### 11.1 Cache policy: `CacheRead`

A route can declare a cache policy:

CacheRead config includes:

* `Enabled` (config toggle + per-route override)
* `TTL` (hard TTL)
* `StaleTTL` (optional: serve stale while revalidating)
* `Key` builder: deterministic, composable
* `VaryBy`: tenant id, user id, roles, query params, headers
* `NegativeCaching`: cache “not found” for short TTL (optional)
* `MaxObjectBytes`: prevent caching huge payloads
* `OnBypass`: debug or emergency bypass toggle

### 11.2 Cache key strategy

Keys are versioned and namespaced:

`cache:{env}:{service}:{route}:{version}:{tenant}:{user}:{hash(params)}`

Rules:

* include tenant/user only when needed (avoid exploding keyspace)
* ensure stable ordering of query params
* never include secrets/raw tokens in keys
* version keys to bust on schema changes

### 11.3 Stampede protection

Cache layer supports (configurable):

* **singleflight** (in-process) for high QPS routes
* optional Redis lock for cross-instance stampede control (only if required)

### 11.4 Invalidation: `CacheInvalidate`

Write routes can declare invalidation tags:

* `CacheInvalidate("users:*", "projects:tenant:{tid}")`

Implementation options (configurable):

* tag → key-set tracking (more storage)
* tag → version bump (cheap, recommended for high performance)
* explicit key deletion (only for narrow sets)

**Recommended default:** tag-based **version bump** to avoid large delete storms.

---

## 12) Rate Limiting (Policies)

Rate limiting is part of the kernel and selectable per route.

### 12.1 Global default limiting

* per-IP baseline
* per-user/token when authenticated
* protects DB/Redis from overload

### 12.2 Route overrides

Routes can declare:

* different rates
* different keys (IP vs user vs tenant)
* stricter limits for sensitive endpoints (login, billing)

Implementation:

* Redis-backed token bucket / leaky bucket (fast Lua or atomic ops)
* optional in-memory limiter for dev and for ultra-low latency routes (not distributed)

---

## 13) Performance Design for 10k RPS

### 13.1 Hot path constraints

To hit 10k RPS reliably:

* keep global middleware short and allocation-light
* avoid fmt-heavy logs in hot path (structured logs, sampled)
* avoid maps in response creation when possible
* avoid reflection-heavy decoding loops
* keep policy checks O(1)

### 13.2 Backpressure and overload control

* global max in-flight requests gate (semaphore)
* fast-fail under overload:

  * return 429 when limiter triggers
  * return 503 when concurrency gate triggers
* strict timeouts on downstream calls prevent request pileups

### 13.3 Timeouts (defaults; configurable)

* server read header timeout (e.g., 5s)
* request timeout (e.g., 10–15s)
* DB query timeout (e.g., 2–5s typical)
* Redis op timeout (e.g., 50–150ms)

### 13.4 DB reality check

10k RPS with DB writes per request is not a template problem; it’s a system design problem.
The template supports:

* caching
* queue-based async writes (optional addon)
* read replicas patterns
* query instrumentation
  …but DB capacity must be planned per product.

---

## 14) Reliability & Resilience

### 14.1 Dependency failure behavior

* Redis down:

  * jwt-only endpoints still work
  * strict auth endpoints fail closed (behavior configurable: 401 vs 503)
  * cache policies fail open (bypass cache) by default unless configured otherwise
* DB down:

  * readiness fails
  * endpoints return typed dependency-unavailable errors

### 14.2 Graceful shutdown

* stop accepting new requests
* drain in-flight with deadline
* stop module workers
* flush audit/metrics best-effort

### 14.3 Health endpoints

* `/healthz`: process alive
* `/readyz`: dependency checks + module readiness

---

## 15) Observability

### 15.1 Logging

* JSON structured logs
* request-id on every request
* configurable sampling to keep overhead low at 10k RPS

### 15.2 Metrics

Prometheus metrics (default):

* request count by route/status
* latency histograms
* in-flight
* limiter rejections
* cache hit/miss/stale/evictions
* DB pool stats
* Redis latency/errors

### 15.3 Tracing (optional addon)

* enabled via config
* OTel propagation hooks and exporter config
* keep tracing overhead off by default in very high-RPS services unless needed

### 15.4 Audit (optional addon)

* async, bounded queue
* sinks: stdout/json/file; expandable later

---

## 16) Configuration System (Addons & Safe Defaults)

Config is env-driven (12-factor) and supports:

* environment modes (dev/staging/prod)
* feature toggles (audit, tracing, cache, strict auth default)
* policy defaults (rate limits, cache TTLs)

Startup config lint:

* warns/errors on insecure or risky production settings.

---

## 17) Testing & Benchmarking

### 17.1 Testing layers

* unit: services, key validation, policy logic
* integration: DB/Redis + routes
* contract: error mapping and response envelope stability

### 17.2 Performance benchmarks included

* baseline load tests (k6/vegeta)
* scenarios:

  * jwt-only 10k RPS
  * strict auth + redis 10k RPS
  * cache-heavy route 10k RPS
  * mixed load

---

## 18) Implementation Notes (What this template enables)

With this architecture, adding a module is:

1. create `internal/modules/<name>/`
2. implement `module.go` + `routes.go` + DTOs + service + repo
3. register module (or use generator later)

You do not reconfigure:

* auth
* rate limiting
* caching
* error model
* observability
* DB/Redis wiring

All are reused and controlled per route via policies.

---

