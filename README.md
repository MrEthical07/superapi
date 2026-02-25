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

## Database migrations and sqlc baseline

The template uses:

- Migrations: `golang-migrate`
- Query generation: `sqlc` targeting `pgx/v5`

Folder convention (global baseline):

- `db/migrations/` versioned migration files
- `db/schema/` canonical schema for sqlc
- `db/queries/` SQL query definitions
- `internal/core/db/sqlcgen/` generated sqlc package (DO NOT EDIT MANUALLY)

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

- `make sqlc-generate`
- `make migrate-create NAME=add_feature_table`
- `make migrate-up DB_URL=postgres://...`
- `make migrate-down DB_URL=postgres://...`
- `make migrate-version DB_URL=postgres://...`

Integration point:

- Use `internal/core/db.NewQueries(pool)` with `*pgxpool.Pool` (or tx-compatible DBTX).

Operational note:

- Migrations are intentionally not auto-run during API startup; apply them explicitly in deploy workflows.

## Tenants reference module

The first DB-backed reference module is available at:

- `POST /api/v1/tenants`
- `GET /api/v1/tenants/{id}`
- `GET /api/v1/tenants?limit=50`

It demonstrates the module pattern with typed handlers, service/repo separation, sqlc queries, and transactional create flow.