# Security Environment Variable Recommendations (SuperAPI Template)

This document gives production-focused recommendations for all runtime environment variables defined by this template.

Evidence anchors:
- Env inventory and defaults: `docs/environment-variables.md:9-186`
- Runtime loading and lint constraints: `internal/core/config/config.go:142-615`
- Metrics endpoint auth behavior: `internal/core/app/app.go:45-49,92-112`
- Redis/Postgres client wiring: `internal/core/cache/redis.go:14-22`, `internal/core/db/postgres.go:14-22`

## 1) Security baseline profile (recommended)

Use this as a starting profile for internet-facing production:

```bash
APP_ENV=production
APP_SERVICE_NAME=superapi-prod

HTTP_ADDR=:8080
HTTP_READ_HEADER_TIMEOUT=5s
HTTP_READ_TIMEOUT=15s
HTTP_WRITE_TIMEOUT=20s
HTTP_IDLE_TIMEOUT=60s
HTTP_SHUTDOWN_TIMEOUT=15s
HTTP_MAX_HEADER_BYTES=32768

HTTP_MIDDLEWARE_REQUEST_ID_ENABLED=true
HTTP_MIDDLEWARE_RECOVERER_ENABLED=true
HTTP_MIDDLEWARE_MAX_BODY_BYTES=1048576
HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED=true
HTTP_MIDDLEWARE_REQUEST_TIMEOUT=10s
HTTP_MIDDLEWARE_TRACING_EXCLUDE_PATHS=/healthz,/readyz,/metrics
HTTP_TRUSTED_PROXIES=10.0.0.0/8,192.168.0.0/16

HTTP_MIDDLEWARE_CORS_ENABLED=false
HTTP_MIDDLEWARE_CORS_ALLOW_CREDENTIALS=false
HTTP_MIDDLEWARE_CORS_ALLOW_PRIVATE_NETWORK=false

LOG_LEVEL=info
LOG_FORMAT=json

AUTH_ENABLED=true
AUTH_MODE=strict

RATELIMIT_ENABLED=true
RATELIMIT_FAIL_OPEN=false
RATELIMIT_DEFAULT_LIMIT=100
RATELIMIT_DEFAULT_WINDOW=1m

CACHE_ENABLED=false
CACHE_FAIL_OPEN=false
CACHE_DEFAULT_MAX_BYTES=262144
CACHE_TAG_VERSION_CACHE_TTL=250ms

POSTGRES_ENABLED=true
POSTGRES_URL=postgres://<user>:<pass>@<host>:5432/<db>?sslmode=verify-full
POSTGRES_MAX_CONNS=30
POSTGRES_MIN_CONNS=5
POSTGRES_CONN_MAX_LIFETIME=30m
POSTGRES_CONN_MAX_IDLE_TIME=5m
POSTGRES_STARTUP_PING_TIMEOUT=3s
POSTGRES_HEALTH_CHECK_TIMEOUT=1s

REDIS_ENABLED=true
REDIS_ADDR=<private-host>:6379
REDIS_PASSWORD=<strong-secret>
REDIS_DB=0
REDIS_DIAL_TIMEOUT=2s
REDIS_READ_TIMEOUT=2s
REDIS_WRITE_TIMEOUT=2s
REDIS_POOL_SIZE=30
REDIS_MIN_IDLE_CONNS=5
REDIS_STARTUP_PING_TIMEOUT=3s
REDIS_HEALTH_CHECK_TIMEOUT=1s

METRICS_ENABLED=true
METRICS_PATH=/metrics
METRICS_AUTH_TOKEN=<strong-random-token>
METRICS_EXCLUDE_PATHS=/healthz,/readyz

TRACING_ENABLED=true
TRACING_SERVICE_NAME=superapi-prod
TRACING_EXPORTER=otlpgrpc
TRACING_OTLP_ENDPOINT=<otel-collector>:4317
TRACING_SAMPLER=traceidratio
TRACING_SAMPLE_RATIO=0.05
TRACING_INSECURE=false
```

## 2) Recommended values for all current env vars

### Core app

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `APP_ENV` | `production` | Prevents accidental dev posture (`dev` defaults weaken controls). |
| `APP_SERVICE_NAME` | Stable, explicit non-empty value | Avoids ambiguous telemetry identity and incident confusion. |

### HTTP server transport

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `HTTP_ADDR` | Explicit bind (usually `:8080` behind gateway) | Keeps listener behavior deterministic. |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` to `10s` | Slowloris resistance. |
| `HTTP_READ_TIMEOUT` | `10s` to `30s` | Limits socket/resource hold times. |
| `HTTP_WRITE_TIMEOUT` | Slightly above request timeout (for example `20s`) | Prevents hanging response writes. |
| `HTTP_IDLE_TIMEOUT` | `30s` to `120s` | Limits idle keepalive abuse. |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` to `30s` | Predictable shutdown, avoids abrupt cutoffs. |
| `HTTP_MAX_HEADER_BYTES` | `16KiB` to `64KiB` (`32768` suggested) | Reduces oversized-header DoS window vs 1 MiB defaults. |

### HTTP middleware: core toggles

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `HTTP_MIDDLEWARE_REQUEST_ID_ENABLED` | `true` | Correlation and forensic traceability. |
| `HTTP_MIDDLEWARE_RECOVERER_ENABLED` | `true` | Prevents process crash on panic paths. |
| `HTTP_MIDDLEWARE_MAX_BODY_BYTES` | Explicit non-zero (`1048576` baseline) | Keeps request body abuse bounded. |
| `HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED` | `true` | Adds baseline browser hardening headers. |
| `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` | Enabled (`5s` to `30s`) and `<= HTTP_WRITE_TIMEOUT` | Limits long-running app work and stuck handlers. |
| `HTTP_MIDDLEWARE_TRACING_EXCLUDE_PATHS` | Keep `/healthz,/readyz,/metrics` unless tracing these paths is required | Reduces span noise and low-value telemetry overhead. |

### Access log middleware

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `HTTP_MIDDLEWARE_ACCESS_LOG_ENABLED` | `true` | Required for abuse detection and investigations. |
| `HTTP_MIDDLEWARE_ACCESS_LOG_SAMPLE_RATE` | `0.05` to `0.20` | Balance signal and storage cost. |
| `HTTP_MIDDLEWARE_ACCESS_LOG_EXCLUDE_PATHS` | Keep defaults; review custom additions carefully | Avoid accidental blind spots. |
| `HTTP_MIDDLEWARE_ACCESS_LOG_SLOW_THRESHOLD` | `1s` to `3s` | Flags slow-path abuse and dependency issues. |
| `HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_USER_AGENT` | `true` for internet APIs | Better bot/fraud triage. |
| `HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_REMOTE_IP` | `true` with correct trusted proxies | Supports detection/rate-limit investigations. |

### Client IP middleware

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `HTTP_TRUSTED_PROXIES` | Exact gateway CIDRs/IPs only | Prevents spoofed `Forwarded` / `X-Forwarded-For` trust. |

### CORS middleware

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `HTTP_MIDDLEWARE_CORS_ENABLED` | `false` unless browser cross-origin access is required | Smaller cross-origin attack surface. |
| `HTTP_MIDDLEWARE_CORS_ALLOW_ORIGINS` | Explicit allowlist only | Prevents arbitrary web origin access. |
| `HTTP_MIDDLEWARE_CORS_DENY_ORIGINS` | Optional targeted blocklist | Extra safeguard for known bad origins. |
| `HTTP_MIDDLEWARE_CORS_ALLOW_METHODS` | Minimal needed set | Least-privilege request surface. |
| `HTTP_MIDDLEWARE_CORS_ALLOW_HEADERS` | Explicit minimal set | Avoid broad request header acceptance. |
| `HTTP_MIDDLEWARE_CORS_EXPOSE_HEADERS` | Explicit minimal set | Avoid overexposing response metadata. |
| `HTTP_MIDDLEWARE_CORS_ALLOW_CREDENTIALS` | `false` unless mandatory | Credentialed CORS greatly increases risk. |
| `HTTP_MIDDLEWARE_CORS_MAX_AGE` | Small positive value (for example `300s`) | Reduces stale policy windows. |
| `HTTP_MIDDLEWARE_CORS_ALLOW_PRIVATE_NETWORK` | `false` | Avoids enabling private-network browser preflight unless needed. |

### Logging

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `LOG_LEVEL` | `info` (or `warn` for noisy systems) | Avoid excessive sensitive operational detail. |
| `LOG_FORMAT` | `json` | Structured detection pipelines and alerting. |

### Authentication

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `AUTH_ENABLED` | `true` for non-public APIs | Avoids accidental anonymous data access. |
| `AUTH_MODE` | `strict` for sensitive APIs; `hybrid` only with accepted revocation gap | `jwt_only`/`hybrid` can permit revoked token usage under certain conditions. |

### Rate limiting

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `RATELIMIT_ENABLED` | `true` | Baseline abuse and brute-force control. |
| `RATELIMIT_FAIL_OPEN` | `false` for security-sensitive routes | Prevents bypass when Redis is degraded. |
| `RATELIMIT_DEFAULT_LIMIT` | Route-dependent, conservative | Prevents accidental unlimited pressure. |
| `RATELIMIT_DEFAULT_WINDOW` | `1m` (or route-specific) | Predictable enforcement behavior. |

### Response cache

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `CACHE_ENABLED` | `false` unless explicitly needed | Avoids accidental sensitive-response caching. |
| `CACHE_FAIL_OPEN` | `false` for strict consistency/security routes | Prevents silent behavior changes on Redis failure. |
| `CACHE_DEFAULT_MAX_BYTES` | Explicit bounded value (`256KiB` baseline) | Reduces cache memory abuse/oversized objects. |
| `CACHE_TAG_VERSION_CACHE_TTL` | Low positive duration (`100ms` to `500ms`) | Reduces repeated Redis `MGET` load while keeping invalidation freshness tight. |

### PostgreSQL

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `POSTGRES_ENABLED` | `true` when required | Explicit dependency posture. |
| `POSTGRES_URL` | Enforce TLS (`sslmode=verify-full` preferred) | Protects DB credentials and data in transit. |
| `POSTGRES_MAX_CONNS` | Sized to workload and DB limits | Prevents self-induced DB exhaustion. |
| `POSTGRES_MIN_CONNS` | Non-zero warm pool (for example 2-5) | Improves stability under bursts. |
| `POSTGRES_CONN_MAX_LIFETIME` | `15m` to `60m` | Limits long-lived stale connections. |
| `POSTGRES_CONN_MAX_IDLE_TIME` | `1m` to `10m` | Reclaims idle resources. |
| `POSTGRES_STARTUP_PING_TIMEOUT` | `2s` to `5s` | Fast fail on bad dependency wiring. |
| `POSTGRES_HEALTH_CHECK_TIMEOUT` | `1s` to `3s` | Keeps readiness checks bounded. |

### Redis

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `REDIS_ENABLED` | `true` if auth/rate-limit/cache depend on it | Explicit dependency posture. |
| `REDIS_ADDR` | Private network endpoint only | Avoids direct internet exposure. |
| `REDIS_PASSWORD` | Strong secret, never empty in prod | Prevents unauthenticated Redis access. |
| `REDIS_DB` | Explicit value per environment | Reduces accidental keyspace overlap. |
| `REDIS_DIAL_TIMEOUT` | `1s` to `3s` | Fast failure for degraded dependencies. |
| `REDIS_READ_TIMEOUT` | `1s` to `3s` | Avoids long blocked reads. |
| `REDIS_WRITE_TIMEOUT` | `1s` to `3s` | Avoids long blocked writes. |
| `REDIS_POOL_SIZE` | Sized to workload and Redis capacity | Prevents connection churn/exhaustion. |
| `REDIS_MIN_IDLE_CONNS` | Non-zero for busy services | Better latency under bursts. |
| `REDIS_STARTUP_PING_TIMEOUT` | `2s` to `5s` | Fail-fast dependency checks. |
| `REDIS_HEALTH_CHECK_TIMEOUT` | `1s` to `3s` | Bounded readiness checks. |

### Metrics

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `METRICS_ENABLED` | `true` only if scraped by trusted system; else `false` | Reduces reconnaissance surface when unused. |
| `METRICS_PATH` | Keep explicit and stable (`/metrics`) | Operational predictability; secure with auth/network controls. |
| `METRICS_AUTH_TOKEN` | Always set in production and staging | Prevents unauthenticated telemetry scraping. |
| `METRICS_EXCLUDE_PATHS` | Keep `/healthz,/readyz` excluded unless needed | Avoids metric noise from high-frequency health checks. |

### Tracing

| Variable | Recommended prod value | Security rationale |
|---|---|---|
| `TRACING_ENABLED` | `true` if observability required; else `false` | Reduces data egress if unused. |
| `TRACING_SERVICE_NAME` | Explicit stable name | Clean incident triage and attribution. |
| `TRACING_EXPORTER` | `otlpgrpc` | Current supported value. |
| `TRACING_OTLP_ENDPOINT` | Private collector endpoint | Avoids uncontrolled trace egress. |
| `TRACING_SAMPLER` | `traceidratio` (or risk-based policy) | Balanced cost and visibility. |
| `TRACING_SAMPLE_RATIO` | `0.01` to `0.10` baseline | Reduces high-volume data leakage/cost. |
| `TRACING_INSECURE` | `false` | Enforces transport security for telemetry. |

## 3) Recommended new env vars to add (security gaps)

These are not currently present but are strongly recommended for this template:

| Proposed variable | Why add it | Suggested wiring point |
|---|---|---|
| `AUTH_JWT_ISSUER` | Explicit issuer pinning for token validation | `internal/core/auth/goauth_provider.go` before `Build()` |
| `AUTH_JWT_AUDIENCE` | Prevent token replay across services | `internal/core/auth/goauth_provider.go` |
| `AUTH_JWT_JWKS_URL` or `AUTH_JWT_PUBLIC_KEY` | Explicit trust source for signatures | `internal/core/auth/goauth_provider.go` |
| `AUTH_JWT_CLOCK_SKEW` | Controlled token validation tolerance | `internal/core/auth/goauth_provider.go` |
| `REDIS_TLS_ENABLED` / `REDIS_TLS_MIN_VERSION` | In-transit security for Redis | `internal/core/cache/redis.go` (`redis.Options.TLSConfig`) |
| `POSTGRES_SSLMODE_REQUIRED` (or lint policy) | Enforce `sslmode=require/verify-*` in prod | `internal/core/config/config.go` lint rules |
| `METRICS_BIND_ADDR` | Isolate metrics listener from public API listener | `internal/core/app/app.go` |

## 4) Fast verification checklist

- Confirm `APP_ENV=production` is set in all production workloads.
- Confirm `METRICS_AUTH_TOKEN` is non-empty everywhere metrics are enabled.
- Confirm `AUTH_ENABLED=true` on all non-public APIs and review route policies for every write/read endpoint.
- Confirm `RATELIMIT_ENABLED=true` and `RATELIMIT_FAIL_OPEN=false` for abuse-sensitive routes.
- Confirm Postgres and Redis traffic is encrypted in transit (and remove `sslmode=disable` from operational examples).
- Confirm `HTTP_TRUSTED_PROXIES` is set only to real reverse-proxy CIDRs.
