# Overview

## What this template is

SuperAPI is a production-grade Go API template for SaaS projects. It provides a fully wired HTTP server with:

- Module-based architecture (add features without touching core wiring)
- Route-level policy engine (auth, rate limiting, caching, tenant isolation, RBAC)
- Postgres (pgx v5 + sqlc) and Redis (go-redis v9) integration
- Prometheus metrics, OpenTelemetry tracing, structured logging (zerolog)
- goAuth-backed authentication (JWT validation, session checks, MFA-aware)
- Feature toggles via environment variables (everything disabled by default, opt-in)
- Module scaffolder (`make module name=projects`) for zero-boilerplate module creation

## What problems it solves

| Problem | How the template addresses it |
|---|---|
| Repetitive API boilerplate | Modules implement a 2-method interface; routes, middleware, deps are wired automatically |
| Auth/RBAC scattered in handlers | Declarative per-route policies (`AuthRequired`, `RequireRole`, `TenantRequired`, etc.) |
| Cache stampedes and invalidation | Tag-version-bump invalidation, vary-by dimensions, safe defaults for authenticated caching |
| Rate limit concerns mixed with business logic | Redis-backed per-route rate limiting as a policy; configurable scopes (IP, user, tenant, token) |
| Inconsistent error responses | Typed `AppError` model with centralized HTTP mapping; no internal error leaks |
| Observability afterthoughts | Metrics, tracing, structured access logs built in from day 1 |
| DB migration chaos | `golang-migrate` CLI + `cmd/migrate` wrapper; sqlc for type-safe queries |

## Technology stack

| Component | Library | Why |
|---|---|---|
| HTTP router | `net/http` + `chi` v5 | Standard, fast, middleware-compatible |
| JSON | `encoding/json` | Correctness first; strict decode, unknown field rejection |
| Database | PostgreSQL via `pgx/v5` pool + `sqlc` | Type-safe queries, no ORM reflection overhead |
| Cache / Rate limit / Sessions | Redis via `go-redis/v9` | Atomic Lua scripts for rate limiting, JSON cache storage |
| Auth | goAuth (`github.com/MrEthical07/goAuth`) | JWT + session validation, MFA, role/permission extraction |
| Logging | zerolog | Zero-allocation structured JSON logs |
| Metrics | Prometheus client | Counter/histogram/gauge for HTTP, cache, rate limit, DB pool, readiness |
| Tracing | OpenTelemetry (OTLP/gRPC) | W3C trace context propagation, per-request spans |
| Migrations | golang-migrate | Versioned up/down SQL migrations |

## How to run

### Minimal (no dependencies)

```bash
go run ./cmd/api
```

Server starts on `:8080` with health/system endpoints. Postgres and Redis features are disabled.

### With Postgres + Redis

```bash
export POSTGRES_ENABLED=true
export POSTGRES_URL="postgres://user:pass@localhost:5432/mydb?sslmode=disable"
export REDIS_ENABLED=true
export REDIS_ADDR="127.0.0.1:6379"

# Apply migrations
go run ./cmd/migrate up

# Start server
go run ./cmd/api
```

### With auth enabled

```bash
export AUTH_ENABLED=true
export AUTH_MODE=hybrid          # jwt_only | hybrid | strict
export REDIS_ENABLED=true
export REDIS_ADDR="127.0.0.1:6379"
export POSTGRES_ENABLED=true
export POSTGRES_URL="postgres://user:pass@localhost:5432/mydb?sslmode=disable"
go run ./cmd/api
```

### Feature toggles summary

| Feature | Env var | Default | Requires |
|---|---|---|---|
| Postgres | `POSTGRES_ENABLED` | `false` | `POSTGRES_URL` |
| Redis | `REDIS_ENABLED` | `false` | `REDIS_ADDR` |
| Auth (goAuth) | `AUTH_ENABLED` | `false` | Redis + Postgres |
| Rate limiting | `RATELIMIT_ENABLED` | `false` | Redis |
| Response caching | `CACHE_ENABLED` | `false` | Redis |
| Prometheus metrics | `METRICS_ENABLED` | `true` | — |
| OpenTelemetry tracing | `TRACING_ENABLED` | `false` | OTLP endpoint |
| Security headers | `HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED` | `false` | — |
| Request timeout | `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` | `0` (disabled) | — |
| Max body bytes | `HTTP_MIDDLEWARE_MAX_BODY_BYTES` | `0` (disabled) | — |
| CORS middleware | `HTTP_MIDDLEWARE_CORS_ENABLED` | `false` | — |
| Trusted proxies | `HTTP_TRUSTED_PROXIES` | empty | — |

Notes:
- Client IP resolution trusts `Forwarded` / `X-Forwarded-For` only when `HTTP_TRUSTED_PROXIES` is configured.
- `HTTP_MIDDLEWARE_CORS_ALLOW_CREDENTIALS=true` cannot be used with `HTTP_MIDDLEWARE_CORS_ALLOW_ORIGINS=*`.

## Key endpoints (built-in)

| Method | Path | Description |
|---|---|---|
| GET | `/healthz` | Process liveness (always 200) |
| GET | `/readyz` | Dependency readiness (200 or 503) |
| GET | `/metrics` | Prometheus metrics |
| POST | `/system/parse-duration` | Typed JSON handler example |
| GET | `/api/v1/system/whoami` | Auth-protected principal info |
| POST | `/api/v1/tenants` | Create tenant |
| GET | `/api/v1/tenants` | List tenants |
| GET | `/api/v1/tenants/{id}` | Get tenant by ID (cached) |
| GET | `/api/v1/tenants/self` | Get own tenant (auth + tenant required) |

## Where to start

1. Read [docs/architecture.md](architecture.md) for under-the-hood understanding
2. Read [docs/modules.md](modules.md) to learn how to add a module
3. Read [docs/policies.md](policies.md) for auth/rate-limit/cache/tenant policy reference
4. Read [docs/crud-examples.md](crud-examples.md) for full CRUD with recommended policy stacks
5. Use `make module` for the interactive wizard, or `make module name=<your_module>` for flags-only scaffolding
