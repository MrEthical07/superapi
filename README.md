# SuperAPI

A security-first, high-performance Go API template for multi-tenant SaaS projects.

## Requirements

- Go 1.26+
- PostgreSQL 15+ (optional, enable via `POSTGRES_ENABLED=true`)
- Redis 7+ (optional, enable via `REDIS_ENABLED=true`)

## Quick start

```bash
go test ./...
go run ./cmd/api
```

## Where to start

| Goal | Read |
|---|---|
| Understand the project | [docs/overview.md](docs/overview.md) |
| Learn the architecture | [docs/architecture.md](docs/architecture.md) |
| Full environment reference | [docs/environment-variables.md](docs/environment-variables.md) |
| Day-to-day dev workflows | [docs/workflows.md](docs/workflows.md) |
| Build a new module | [docs/modules.md](docs/modules.md) |
| Full CRUD walkthrough | [docs/crud-examples.md](docs/crud-examples.md) |
| Route policies (auth, rate limit, cache) | [docs/policies.md](docs/policies.md) |
| Cache deep dive | [docs/cache-guide.md](docs/cache-guide.md) |
| Performance testing runbook | [docs/performance-testing.md](docs/performance-testing.md) |
| Auth & goAuth integration | [docs/auth-goauth.md](docs/auth-goauth.md) |
| Auth bootstrap generation | [docs/auth-bootstrap.md](docs/auth-bootstrap.md) |

## Feature toggles

All major subsystems are opt-in via environment variables:

| Feature | Env var | Default |
|---|---|---|
| PostgreSQL | `POSTGRES_ENABLED` | `false` |
| Redis | `REDIS_ENABLED` | `false` |
| Authentication | `AUTH_ENABLED` | `false` |
| Rate limiting | `RATELIMIT_ENABLED` | `false` |
| Response caching | `CACHE_ENABLED` | `false` |
| Prometheus metrics | `METRICS_ENABLED` | `true` |
| OpenTelemetry tracing | `TRACING_ENABLED` | `false` |

Dependencies: auth requires both Redis and Postgres. Rate limiting and caching require Redis.

For the complete list of runtime env vars and defaults, see `docs/environment-variables.md`.

Runtime identity and environment:

- `APP_ENV` (default: `dev`) influences prod-safe defaults (for example, security headers and tracing transport).
- `APP_SERVICE_NAME` (default: `api-template`) is used for service identity and tracing fallback naming.

## HTTP middleware config

Global (server-level) middleware is configured via environment variables:

- `HTTP_MIDDLEWARE_REQUEST_ID_ENABLED` (default: `true`)
- `HTTP_MIDDLEWARE_RECOVERER_ENABLED` (default: `true`)
- `HTTP_MIDDLEWARE_MAX_BODY_BYTES` (default: `1048576` / 1 MiB)
- `HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED` (default: `true` in `prod`, otherwise `false`)
- `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` (default: `0`, disabled)
- `HTTP_MIDDLEWARE_ACCESS_LOG_ENABLED` (default: `true`)
- `HTTP_MIDDLEWARE_ACCESS_LOG_SAMPLE_RATE` (default: `0.05`)
- `HTTP_MIDDLEWARE_ACCESS_LOG_EXCLUDE_PATHS` (default: `/healthz,/readyz,/metrics`)
- `HTTP_MIDDLEWARE_ACCESS_LOG_SLOW_THRESHOLD` (default: `2s`)
- `HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_USER_AGENT` (default: `false`)
- `HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_REMOTE_IP` (default: `false`)
- `HTTP_TRUSTED_PROXIES` (default: empty; comma-separated CIDRs/IPs)
- `HTTP_MIDDLEWARE_CORS_ENABLED` (default: `false`)
- `HTTP_MIDDLEWARE_CORS_ALLOW_ORIGINS` (default: empty)
- `HTTP_MIDDLEWARE_CORS_DENY_ORIGINS` (default: empty)
- `HTTP_MIDDLEWARE_CORS_ALLOW_METHODS` (default: empty; defaults to common methods)
- `HTTP_MIDDLEWARE_CORS_ALLOW_HEADERS` (default: empty; echoes request headers)
- `HTTP_MIDDLEWARE_CORS_EXPOSE_HEADERS` (default: empty)
- `HTTP_MIDDLEWARE_CORS_ALLOW_CREDENTIALS` (default: `false`)
- `HTTP_MIDDLEWARE_CORS_MAX_AGE` (default: `0`)
- `HTTP_MIDDLEWARE_CORS_ALLOW_PRIVATE_NETWORK` (default: `false`)

All `HTTP_*` env values are linted at startup; invalid values fail `config.Lint()`.

## Auth adapter (goAuth-backed)

Route-level authentication is provided through core policies backed by a thin adapter over goAuth primitives.

Configuration:

- `AUTH_ENABLED` (default: `false`)
- `AUTH_MODE` (default: `hybrid`; valid: `jwt_only`, `hybrid`, `strict`)

Notes:

- `AUTH_ENABLED=true` requires both `REDIS_ENABLED=true` and `POSTGRES_ENABLED=true`.
- Existing public endpoints remain public unless a route declares auth policies.
- Demo protected route: `GET /api/v1/system/whoami`.
	- Without bearer token: returns `401`.
	- With valid token and provider validation: returns safe principal fields (`user_id`, `tenant_id`, `role`, `permissions`).

## Rate limit policy (Redis-backed)

Route-level rate limiting is available through policy wrappers and is Redis-backed using an atomic Lua script.

Configuration:

- `RATELIMIT_ENABLED` (default: `false`)
- `RATELIMIT_FAIL_OPEN` (default: `true`)
- `RATELIMIT_DEFAULT_LIMIT` (default: `10`)
- `RATELIMIT_DEFAULT_WINDOW` (default: `1m`)

Notes:

- `RATELIMIT_ENABLED=true` requires `REDIS_ENABLED=true` (startup lint).
- Fail-open behavior (`RATELIMIT_FAIL_OPEN=true`) allows requests when Redis is unavailable to avoid full API outage.
- Key format: `rl:{env}:{route}:{scope}:{id}` where `route` is route pattern and `scope` is low-cardinality (`ip`, `user`, `tenant`, `token`, `anon`).
- Token-based scope stores only token fingerprint hash prefix (never raw bearer tokens).

Policy metrics:

- `superapi_rate_limit_requests_total{route,outcome}` where outcome is one of `allowed`, `blocked`, `fail_open`, `error`.

## Cache policy (Redis-backed)

Route-level response caching is available through policy wrappers and backed by Redis.

Configuration:

- `CACHE_ENABLED` (default: `false`)
- `CACHE_FAIL_OPEN` (default: `true`)
- `CACHE_DEFAULT_MAX_BYTES` (default: `262144` / 256 KiB)

Notes:

- `CACHE_ENABLED=true` requires `REDIS_ENABLED=true` (startup lint).
- Invalidation strategy uses tag version bump (O(1) invalidation), not mass key deletion.
- Version keys are stored as `cver:{env}:{tag}` and incremented on write-route invalidation.
- Read keys are deterministic and low-cardinality: route pattern + selected vary dimensions + selected query hash + tag-version token.
- Raw query strings, request IDs, and IPs are not used in cache keys by default.

Safety defaults:

- Cache read policy only applies to `GET`/`HEAD` by default.
- Only `200` responses are cached by default.
- Responses with `Set-Cookie` are bypassed (not cached).
- Responses larger than configured max bytes are bypassed.
- Authenticated responses are bypassed unless cache key explicitly varies by user and/or tenant (or auth caching is explicitly allowed).
- Redis failures are fail-open by default (`CACHE_FAIL_OPEN=true`) so requests continue to origin handlers.

Policy metrics:

- `superapi_cache_operations_total{route,outcome}` where outcome is one of `hit`, `miss`, `set`, `bypass`, `error`.

Example cache policy wiring:

- Read route example: use `CacheRead` with route-level `TTL`, tags, and `VaryBy` dimensions.
- Write route example: use `CacheInvalidate` to bump related tags.

## Tenant scope + RBAC policy helpers

Tenant isolation helpers are provided as route policies so checks stay centralized and hard to forget.

Core helper package:

- `internal/core/tenant`
	- `TenantIDFromContext(ctx)`
	- `RequireTenant(ctx)`
	- `IsSameTenant(principalTenant, resourceTenant)`

Policy helpers:

- `policy.TenantRequired()`
	- Requires auth context and non-empty `tenant_id`.
	- Missing auth context -> `401 unauthorized`.
	- Auth present but missing tenant scope -> `403 forbidden`.
- `policy.TenantMatchFromPath(paramName)`
	- Compares path tenant id (via `httpx.URLParam`) with principal `tenant_id`.
	- Missing path param -> `400 bad_request`.
	- Mismatch -> `404 not_found` (chosen to reduce tenant enumeration risk).

RBAC helpers:

- `policy.RequirePerm(...)`: all-of permissions.
- `policy.RequireAnyPerm(...)`: any-of permissions.

RBAC status rules:

- Missing auth context -> `401 unauthorized`.
- Authenticated but missing required permission -> `403 forbidden`.

Recommended attachment order for tenant-scoped routes:

1. `AuthRequired(...)`
2. `TenantRequired()`
3. `TenantMatchFromPath("tenant_id")` (for routes containing tenant id in path)
4. RBAC checks (`RequirePerm` / `RequireAnyPerm`) as needed.

Demonstration pattern:

- Apply `AuthRequired` + `TenantRequired` to tenant-scoped routes in your modules.
- For path-based tenant isolation, add `TenantMatchFromPath("tenant_id")`.

Notes:
- `HTTP_MIDDLEWARE_MAX_BODY_BYTES` must be `>= 0`.
- `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` must be a valid duration and `>= 0`.
- `HTTP_MIDDLEWARE_ACCESS_LOG_SAMPLE_RATE` must be in `[0,1]`.
- `HTTP_MIDDLEWARE_ACCESS_LOG_SLOW_THRESHOLD` must be `>= 0`.
- `HTTP_MIDDLEWARE_ACCESS_LOG_EXCLUDE_PATHS` entries must start with `/`.

Access logging behavior:

- Uses structured logs with route patterns (for example, `/api/v1/projects/{id}`) instead of raw URL paths.
- Logs are sampled using a deterministic request-id hash.
- 5xx responses are always logged, even when sampling would skip.
- Requests above slow threshold are always logged, even when sampling would skip.
- Request bodies, Authorization/Cookie headers, and query strings are not logged by default.
- Remote IP logging uses the resolved client IP (trusted proxy headers only when `HTTP_TRUSTED_PROXIES` is set).

Request ID behavior:

- Incoming `X-Request-Id` is accepted only if it is <= 64 chars and matches `[A-Za-z0-9._-]`. Otherwise a new ID is generated.

## Timeout semantics

Server-level transport timeouts:

- `HTTP_READ_HEADER_TIMEOUT`: bounds header read time (slowloris protection).
- `HTTP_READ_TIMEOUT`: bounds full request read time (headers + body).
- `HTTP_WRITE_TIMEOUT`: hard cap for writing the response.
- `HTTP_IDLE_TIMEOUT`: keep-alive idle connection timeout.
- `HTTP_SHUTDOWN_TIMEOUT`: graceful shutdown deadline for draining in-flight requests.
- `HTTP_MAX_HEADER_BYTES`: maximum allowed request header size.

Application-level request timeout:

- `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` (default: `0`, disabled)
- When enabled (`> 0`), middleware sets `context.WithTimeout` for downstream handler/service logic.
- If the request deadline is exceeded before any response is written, API returns:
	- HTTP `504 Gateway Timeout`
	- envelope error code: `timeout`
	- message: `request timed out`

Validation and tuning guidance:

- `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` must be `>= 0`.
- If enabled, it must be `<= HTTP_WRITE_TIMEOUT` (lint enforced).
- `HTTP_SHUTDOWN_TIMEOUT` must be `> 0`.
- `HTTP_MAX_HEADER_BYTES` must be `>= 4096`.
- Recommended production tuning: set middleware timeout slightly below write timeout so application logic cancels early and returns a controlled JSON timeout response.

Notes:

- Timeout middleware is cooperative (context-based), so downstream code must honor request context cancellation.
- Streaming endpoints are not explicitly supported by this timeout response path; for streaming workloads, evaluate dedicated timeout/heartbeat semantics per endpoint.

## Typed endpoint example

The API includes `POST /system/parse-duration` using the typed JSON adapter.

Example:

```bash
curl -i -X POST http://localhost:8080/system/parse-duration \
	-H "Content-Type: application/json" \
	-d '{"duration":"1500ms"}'
```

## Infrastructure wiring

PostgreSQL and Redis wiring is available and disabled by default.

PostgreSQL:

- `POSTGRES_ENABLED` (default: `false`)
- `POSTGRES_URL` (required when enabled)
- `POSTGRES_MAX_CONNS` (default: `10`)
- `POSTGRES_MIN_CONNS` (default: `0`)
- `POSTGRES_CONN_MAX_LIFETIME` (default: `30m`)
- `POSTGRES_CONN_MAX_IDLE_TIME` (default: `5m`)
- `POSTGRES_STARTUP_PING_TIMEOUT` (default: `3s`)
- `POSTGRES_HEALTH_CHECK_TIMEOUT` (default: `1s`)

Redis:

- `REDIS_ENABLED` (default: `false`)
- `REDIS_ADDR` (required when enabled; default: `127.0.0.1:6379`)
- `REDIS_PASSWORD` (optional)
- `REDIS_DB` (default: `0`)
- `REDIS_DIAL_TIMEOUT` (default: `2s`)
- `REDIS_READ_TIMEOUT` (default: `2s`)
- `REDIS_WRITE_TIMEOUT` (default: `2s`)
- `REDIS_POOL_SIZE` (default: `10`)
- `REDIS_MIN_IDLE_CONNS` (default: `0`)
- `REDIS_STARTUP_PING_TIMEOUT` (default: `3s`)
- `REDIS_HEALTH_CHECK_TIMEOUT` (default: `1s`)

Readiness behavior:

- Startup strategy is fail-fast for enabled dependencies.
- `/healthz` is process aliveness only.
- `/readyz` returns dependency statuses (`ok`, `disabled`, `error`) and `status` (`ready` or `not_ready`).

## Metrics

Prometheus metrics are enabled by default and exposed from the API process.

Configuration:

- `METRICS_ENABLED` (default: `true`)
- `METRICS_AUTH_TOKEN` (default: empty)

Production hardening:

- In `prod`/`production`, metrics require `METRICS_AUTH_TOKEN` when metrics are enabled.
- Send `Authorization: Bearer <METRICS_AUTH_TOKEN>` to access metrics.
- `METRICS_PATH` (default: `/metrics`)

Notes:

- `METRICS_PATH` must be non-empty and start with `/`.
- When `METRICS_ENABLED=false`, the metrics endpoint is not registered.

Baseline metrics:

- `superapi_http_requests_total{method,route,status}`
- `superapi_http_request_duration_seconds{method,route,status}`
- `superapi_http_in_flight_requests`
- `superapi_cache_operations_total{route,outcome}`
- `superapi_ready`
- `superapi_dependency_ready{dependency,status}` (`status` in `ok|disabled|error`)

When Postgres is enabled, these pool gauges are also exported:

- `superapi_db_pool_acquired_connections`
- `superapi_db_pool_idle_connections`
- `superapi_db_pool_total_connections`
- `superapi_db_pool_max_connections`

## Tracing

OpenTelemetry tracing hooks are available and disabled by default.

Configuration:

- `TRACING_ENABLED` (default: `false`)
- `TRACING_INSECURE` (default: `false` in `prod`, otherwise `true`)
- `TRACING_SERVICE_NAME` (default: `APP_SERVICE_NAME`)
- `TRACING_EXPORTER` (default: `otlpgrpc`)
- `TRACING_OTLP_ENDPOINT` (default: `localhost:4317`)
- `TRACING_SAMPLER` (default: `traceidratio`; options: `always_on`, `always_off`, `traceidratio`)
- `TRACING_SAMPLE_RATIO` (default: `0.05`, used by `traceidratio`)

Behavior:

- Uses W3C `traceparent` + baggage propagation.
- Creates one server span per request.
- Span name uses low-cardinality route patterns (for example, `GET /api/v1/projects/{id}`), not raw paths.
- Adds attributes: `http.method`, `http.route`, `http.status_code`, optional `server.address`/`server.port`, and `request.id`.
- Does not capture request/response bodies, query strings, or sensitive headers.

Middleware order (outermost → innermost):

- RequestID → ClientIP → Recoverer → CORS → SecurityHeaders → MaxBodyBytes → RequestTimeout → Tracing → AccessLog → Router

Example enablement:

```bash
TRACING_ENABLED=true \
TRACING_EXPORTER=otlpgrpc \
TRACING_OTLP_ENDPOINT=localhost:4317 \
TRACING_SAMPLER=traceidratio \
TRACING_SAMPLE_RATIO=0.05 \
go run ./cmd/api
```

Operational note:

- If exporter endpoint is unreachable, API startup still succeeds; spans are best-effort and tracer provider shutdown is attempted during app shutdown.

## Database migrations and sqlc baseline

The template uses:

- Migrations: `golang-migrate`
- Query generation: `sqlc` targeting `pgx/v5`

Folder convention:

- `db/migrations/` versioned migration files
- `internal/modules/<module>/db/schema.sql` module-local sqlc schema source
- `internal/modules/<module>/db/queries.sql` module-local sqlc query source
- `db/schema/` generated sync output consumed by sqlc
- `db/queries/` generated sync output consumed by sqlc
- `internal/core/db/sqlcgen/` generated sqlc package (DO NOT EDIT MANUALLY)

Sync module-local SQL into the global sqlc input folders:

```bash
make db-sync
```

sqlc workflow:

```bash
sqlc generate
```

Equivalent without preinstalled sqlc:

```bash
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate
```

Migration workflow (`golang-migrate` CLI):

```bash
migrate create -ext sql -dir db/migrations -seq add_feature_table
POSTGRES_ENABLED=true POSTGRES_URL="$DB_URL" go run ./cmd/migrate up
POSTGRES_ENABLED=true POSTGRES_URL="$DB_URL" go run ./cmd/migrate down --steps=1
POSTGRES_ENABLED=true POSTGRES_URL="$DB_URL" go run ./cmd/migrate version
```

`cmd/migrate` commands:

```bash
go run ./cmd/migrate --help
go run ./cmd/migrate up
go run ./cmd/migrate down --steps=1
go run ./cmd/migrate version
go run ./cmd/migrate force --version=1
```

Notes:
- `POSTGRES_ENABLED=true` and `POSTGRES_URL` are required for `cmd/migrate`.
- `down` defaults to one step and supports `--steps=N`.
- "no change" outcomes are treated as successful operations.

Make targets are also available:

- `make db-sync`
- `make sqlc-generate`
- `make migrate-create NAME=add_feature_table`
- `make migrate-up DB_URL=postgres://...`
- `make migrate-down DB_URL=postgres://...`
- `make migrate-version DB_URL=postgres://...`

Integration point:

- Use `internal/core/db.NewQueries(pool)` with `*pgxpool.Pool` (or tx-compatible DBTX).

Operational note:

- Migrations are intentionally not auto-run during API startup; apply them explicitly in deploy workflows.

## Reference module pattern

Use the module scaffolder to generate a DB-backed reference module skeleton and implement your own routes.

## Module scaffolder (DX)

Generate a fully wired module skeleton with one command:

```bash
make module name=projects
```

If you omit `name`, the generator starts an interactive wizard and asks which options to scaffold.

Optional overwrite of existing module folder:

```bash
make module name=projects force=1
```

Advanced scaffolding flags:

```bash
# Include module-local db/schema.sql and db/queries.sql
make module name=projects db=1

# Attach generated auth/tenant/rate-limit/cache policy examples to routes
make module name=projects auth=1 tenant=1 ratelimit=1 cache=1

# Also scaffold a global migration (requires db=1)
make module name=projects db=1 migration=1
```

Flag constraints enforced by generator:

- `tenant=1` requires `auth=1`
- `migration=1` requires `db=1`

Behavior:

- Creates `internal/modules/<package>/` with:
	- `module.go`, `routes.go`, `dto.go`, `handler.go`, `service.go`, `repo.go`
	- `handler_test.go`, `service_test.go`
- Optional DB scaffolding creates `internal/modules/<package>/db/schema.sql` and `internal/modules/<package>/db/queries.sql`
- Optional migration scaffolding creates global `db/migrations/NNNNNN_<route>.up.sql` and `.down.sql`
- Adds package import + module `New()` entry to module registry (`internal/modules/modules.go`).
- Registry updates are idempotent (no duplicate imports/entries).
- Without `force`, generation fails when target module directory already exists.

Name normalization rules:

- Input must be lowercase and may contain: `a-z`, `0-9`, `-`, `_`.
- Must start with a letter.
- Package/folder normalization: snake_case.
- Route normalization: kebab-case under `/api/v1/<route>`.

Examples:

- `projects` -> package `projects`, route `/api/v1/projects`
- `project_tasks` -> package `project_tasks`, route `/api/v1/project-tasks`
- `project-tasks` -> package `project_tasks`, route `/api/v1/project-tasks`

Generated default route:

- `GET /api/v1/<module>/ping` returns envelope data:
	- `{ "status": "ok", "module": "<module>" }`

Policy guidance in generated routes:

- Can attach generated `AuthRequired`, `TenantRequired`, `RateLimit`, and `CacheRead` hooks based on wizard/flag options.
- Routes call `policy.*` directly, while the module runtime only supplies injected dependencies like auth provider, limiter, and cache manager.

## Auth bootstrap generator (DX)

Create auth bootstrap schema/queries/provider wiring:

```bash
make auth
```

Run authgen with a config file:

```bash
make auth-config file=authgen.example.yaml
```

Auth bootstrap docs are generated/updated at `docs/auth-bootstrap.md`.
