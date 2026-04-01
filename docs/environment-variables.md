# Environment Variables

This is the runtime env reference for SuperAPI.

Source of truth: `internal/core/config/config.go` (`Load()` defaults + `Lint()` constraints).

---

## Core app

| Env var | Default | Notes |
|---|---|---|
| `APP_PROFILE` | empty | Optional profile defaults. Allowed: `minimal`, `dev`, `prod`. Invalid values fail startup lint. |
| `APP_ENV` | `dev` | `prod`/`production` changes defaults for some settings (security headers, tracing insecure transport, metrics auth requirement). |
| `APP_SERVICE_NAME` | `api-template` | Used as service identity; tracing service name falls back to this value when not explicitly set. |

### APP_PROFILE defaults and precedence

`APP_PROFILE` injects default values before config parsing:

- `minimal`: disables auth/cache/ratelimit/postgres/redis wiring.
- `dev`: enables auth/cache/ratelimit/postgres/redis with dev-friendly defaults.
- `prod`: enables strict auth and fail-closed defaults for cache/rate-limit.

Precedence order for every key is:

1. Explicit environment variable.
2. Active profile default.
3. Built-in fallback in `config.Load()`.

---

## HTTP server transport

| Env var | Default | Notes |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Listen address. |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` | Slowloris protection. Must be `> 0`. |
| `HTTP_READ_TIMEOUT` | `15s` | Full request read timeout. Must be `> 0`. |
| `HTTP_WRITE_TIMEOUT` | `15s` | Response write timeout. Must be `> 0`. |
| `HTTP_IDLE_TIMEOUT` | `60s` | Keep-alive idle timeout. Must be `> 0`. |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | Graceful shutdown deadline. Must be `> 0`. |
| `HTTP_MAX_HEADER_BYTES` | `1048576` (1 MiB) | Maximum request header size. Must be `>= 4096`. |

---

## HTTP middleware: core toggles

| Env var | Default | Notes |
|---|---|---|
| `HTTP_MIDDLEWARE_REQUEST_ID_ENABLED` | `true` | Enables request-id middleware. |
| `HTTP_MIDDLEWARE_RECOVERER_ENABLED` | `true` | Enables panic recovery middleware. |
| `HTTP_MIDDLEWARE_MAX_BODY_BYTES` | `1048576` (1 MiB) | Global body cap. Must be `>= 0`. |
| `HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED` | `true` in `prod`, otherwise `false` | Security headers middleware toggle. |
| `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` | `0` (disabled) | Request context timeout. Must be `>= 0`; if enabled, must be `<= HTTP_WRITE_TIMEOUT`. |

### Access log middleware

| Env var | Default | Notes |
|---|---|---|
| `HTTP_MIDDLEWARE_ACCESS_LOG_ENABLED` | `true` | Access logging toggle. |
| `HTTP_MIDDLEWARE_ACCESS_LOG_SAMPLE_RATE` | `0.05` | Must be in `[0,1]`. |
| `HTTP_MIDDLEWARE_ACCESS_LOG_EXCLUDE_PATHS` | `/healthz,/readyz,/metrics` | Each path must be non-empty and start with `/`. |
| `HTTP_MIDDLEWARE_ACCESS_LOG_SLOW_THRESHOLD` | `2s` | Must be `>= 0`. |
| `HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_USER_AGENT` | `false` | Include User-Agent field in access logs. |
| `HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_REMOTE_IP` | `false` | Include resolved client IP in access logs. |

### Client IP middleware

| Env var | Default | Notes |
|---|---|---|
| `HTTP_TRUSTED_PROXIES` | empty | Comma-separated CIDRs/IPs. Enables trusted proxy forwarding behavior. |

### CORS middleware

| Env var | Default | Notes |
|---|---|---|
| `HTTP_MIDDLEWARE_CORS_ENABLED` | `false` | CORS middleware toggle. |
| `HTTP_MIDDLEWARE_CORS_ALLOW_ORIGINS` | empty | Comma-separated origin list. |
| `HTTP_MIDDLEWARE_CORS_DENY_ORIGINS` | empty | Comma-separated origin deny list. |
| `HTTP_MIDDLEWARE_CORS_ALLOW_METHODS` | empty | If empty, middleware uses common HTTP methods fallback. |
| `HTTP_MIDDLEWARE_CORS_ALLOW_HEADERS` | empty | If empty, preflight echoes request headers. |
| `HTTP_MIDDLEWARE_CORS_EXPOSE_HEADERS` | empty | Comma-separated response headers to expose. |
| `HTTP_MIDDLEWARE_CORS_ALLOW_CREDENTIALS` | `false` | Cannot be combined with wildcard allow origins (`*`). |
| `HTTP_MIDDLEWARE_CORS_MAX_AGE` | `0` | Must be `>= 0`. |
| `HTTP_MIDDLEWARE_CORS_ALLOW_PRIVATE_NETWORK` | `false` | Enables private network preflight response header. |

---

## Logging

| Env var | Default | Notes |
|---|---|---|
| `LOG_LEVEL` | `info` | Minimum level for zerolog output (`debug`, `info`, `warn`, `error`, `fatal`). |
| `LOG_FORMAT` | `json` | `json` or `text`. |

---

## Authentication

| Env var | Default | Notes |
|---|---|---|
| `AUTH_ENABLED` | `false` | Enables auth subsystem. |
| `AUTH_MODE` | `hybrid` | Allowed: `jwt_only`, `hybrid`, `strict` (also accepts `jwt-only` / `jwtonly`). |

Notes:
- When `AUTH_ENABLED=true`, startup lint requires both `REDIS_ENABLED=true` and `POSTGRES_ENABLED=true`.

---

## Rate limiting

| Env var | Default | Notes |
|---|---|---|
| `RATELIMIT_ENABLED` | `false` | Enables Redis-backed route rate limiting. |
| `RATELIMIT_FAIL_OPEN` | `true` | Allows requests when Redis is unavailable. |
| `RATELIMIT_DEFAULT_LIMIT` | `10` | Must be `> 0`. |
| `RATELIMIT_DEFAULT_WINDOW` | `1m` | Must be `> 0`. |

Notes:
- When `RATELIMIT_ENABLED=true`, startup lint requires `REDIS_ENABLED=true`.

---

## Response cache

| Env var | Default | Notes |
|---|---|---|
| `CACHE_ENABLED` | `false` | Enables Redis-backed route response cache. |
| `CACHE_FAIL_OPEN` | `true` | Bypasses cache when Redis is unavailable. |
| `CACHE_DEFAULT_MAX_BYTES` | `262144` (256 KiB) | Must be `> 0`. |

Notes:
- When `CACHE_ENABLED=true`, startup lint requires `REDIS_ENABLED=true`.

---

## PostgreSQL

| Env var | Default | Notes |
|---|---|---|
| `POSTGRES_ENABLED` | `false` | Enables Postgres dependency wiring. |
| `POSTGRES_URL` | empty | Required when Postgres is enabled. |
| `POSTGRES_MAX_CONNS` | `10` | Must be `> 0`. |
| `POSTGRES_MIN_CONNS` | `0` | Must be `>= 0` and `<= POSTGRES_MAX_CONNS`. |
| `POSTGRES_CONN_MAX_LIFETIME` | `30m` | Must be `>= 0`. |
| `POSTGRES_CONN_MAX_IDLE_TIME` | `5m` | Must be `>= 0`. |
| `POSTGRES_STARTUP_PING_TIMEOUT` | `3s` | Must be `> 0`. |
| `POSTGRES_HEALTH_CHECK_TIMEOUT` | `1s` | Must be `> 0`. |

---

## Redis

| Env var | Default | Notes |
|---|---|---|
| `REDIS_ENABLED` | `false` | Enables Redis dependency wiring. |
| `REDIS_ADDR` | `127.0.0.1:6379` | Required when Redis is enabled (must be non-empty). |
| `REDIS_PASSWORD` | empty | Optional password. |
| `REDIS_DB` | `0` | Must be `>= 0`. |
| `REDIS_DIAL_TIMEOUT` | `2s` | Must be `> 0`. |
| `REDIS_READ_TIMEOUT` | `2s` | Must be `> 0`. |
| `REDIS_WRITE_TIMEOUT` | `2s` | Must be `> 0`. |
| `REDIS_POOL_SIZE` | `10` | Must be `> 0`. |
| `REDIS_MIN_IDLE_CONNS` | `0` | Must be `>= 0` and `<= REDIS_POOL_SIZE`. |
| `REDIS_STARTUP_PING_TIMEOUT` | `3s` | Must be `> 0`. |
| `REDIS_HEALTH_CHECK_TIMEOUT` | `1s` | Must be `> 0`. |

---

## Metrics

| Env var | Default | Notes |
|---|---|---|
| `METRICS_ENABLED` | `true` | Enables `/metrics` endpoint registration. |
| `METRICS_PATH` | `/metrics` | Must be non-empty and start with `/`. |
| `METRICS_AUTH_TOKEN` | empty | In `prod`/`production`, required when metrics are enabled. |

---

## Tracing

| Env var | Default | Notes |
|---|---|---|
| `TRACING_ENABLED` | `false` | Enables tracing subsystem. |
| `TRACING_SERVICE_NAME` | empty | Falls back to `APP_SERVICE_NAME` when empty. |
| `TRACING_EXPORTER` | `otlpgrpc` | Allowed value: `otlpgrpc`. |
| `TRACING_OTLP_ENDPOINT` | `localhost:4317` | Required (non-empty) when tracing is enabled. |
| `TRACING_SAMPLER` | `traceidratio` | Allowed: `always_on`, `always_off`, `traceidratio`. |
| `TRACING_SAMPLE_RATIO` | `0.05` | Must be in `[0,1]` when tracing is enabled. |
| `TRACING_INSECURE` | `true` in non-prod, `false` in prod | Transport security toggle for OTLP exporter. |

---

## Dependency rules summary

- `AUTH_ENABLED=true` requires Redis and Postgres.
- `RATELIMIT_ENABLED=true` requires Redis.
- `CACHE_ENABLED=true` requires Redis.
- `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` must not exceed `HTTP_WRITE_TIMEOUT` when enabled.
- In `prod` / `production`, metrics require `METRICS_AUTH_TOKEN` if metrics are enabled.
