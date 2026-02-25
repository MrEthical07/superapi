# api-template

A security-first, high-performance Go API template for SaaS projects.

## Requirements
- Go 1.26+

## Quick start
```bash
go test ./...
go run .
```

## HTTP middleware config

Global (server-level) middleware is configured via environment variables:

- `HTTP_MIDDLEWARE_REQUEST_ID_ENABLED` (default: `true`)
- `HTTP_MIDDLEWARE_RECOVERER_ENABLED` (default: `true`)
- `HTTP_MIDDLEWARE_MAX_BODY_BYTES` (default: `0`, disabled)
- `HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED` (default: `false`)
- `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` (default: `0`, disabled)

Notes:
- `HTTP_MIDDLEWARE_MAX_BODY_BYTES` must be `>= 0`.
- `HTTP_MIDDLEWARE_REQUEST_TIMEOUT` must be a valid duration and `>= 0`.

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