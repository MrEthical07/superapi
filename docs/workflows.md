# Day-to-Day Workflows

This guide documents common development workflows for contributors working on SuperAPI.

Use it as a practical checklist during daily development.

## 1. Running The API

### 1.1 Minimal local run (no external dependencies)

Use this when you only need process-level behavior and basic routes.

```bash
go run ./cmd/api
```

What to expect:

- API starts on configured HTTP_ADDR (default :8080)
- health and readiness routes are available
- external dependency features are disabled unless enabled via env

### 1.2 Full local run (Postgres + Redis + auth + cache + rate-limit)

```bash
POSTGRES_ENABLED=true POSTGRES_URL="postgres://user:pass@localhost:5432/mydb?sslmode=disable" REDIS_ENABLED=true REDIS_ADDR="127.0.0.1:6379" AUTH_ENABLED=true RATELIMIT_ENABLED=true CACHE_ENABLED=true go run ./cmd/api
```

Use this mode when testing realistic route behavior.

### 1.3 Useful startup checks

After startup verify:

- GET /healthz
- GET /readyz
- GET /metrics (if metrics enabled)

## 2. Creating A New Module

### 2.1 Generate module scaffold

```bash
make module name=projects
```

This creates module files and updates module registry.

### 2.2 Immediately after scaffold

Before writing business logic, do these checks:

1. confirm route path and package name are correct
2. confirm module appears in internal/modules/modules.go
3. confirm generated files compile

### 2.3 Architecture alignment pass

Scaffold output is a starting point. Update it to follow enforced flow:

- handler -> service -> repository -> `DB().Queries(ctx)`
- service runs no queries itself; it may only call `DB().WithTx(ctx, fn)` to
  define write transaction boundaries
- repositories must not control transaction boundaries
- repository public interface is domain-focused (no sqlc/pgx types)

## 3. Implementing Data Access

For each module:

1. Choose storage type (relational via `DB()`, or the optional document store).
2. Define the repository contract in domain language.
3. Implement the repository using `DB().Queries(ctx)` and row->domain mapping.
4. Wire the repository/service in the module's dependency binding (`runtime.DB()`).
5. Keep write paths transactional (`WithTx`) and read paths direct.

## 4. Transaction Workflow

### 4.1 Write paths

Pattern:

- service calls `DB().WithTx(ctx, fn)` to define the transaction boundary
- service invokes repository write method(s) with the callback's `txCtx`
- repository runs `Queries(txCtx).<GeneratedWrite>(...)`, joining the tx
- `WithTx` commits on nil error and rolls back on error/panic
- the service runs no queries itself

### 4.2 Read paths

Pattern:

- service calls the repository read method with `ctx`
- repository runs `Queries(ctx).<GeneratedRead>(...)` on the pool
- no forced tx wrapper by default

See [docs/transactions.md](transactions.md) for the full guide and the
context-threading gotcha.

## 5. Relational Backend Workflow

### 5.1 Migration flow

Create migration:

```bash
make migrate-create NAME=add_projects_table
```

Apply migrations:

```bash
make migrate-up DB_URL="postgres://user:pass@localhost:5432/mydb?sslmode=disable"
```

Roll back as needed:

```bash
make migrate-down DB_URL="postgres://user:pass@localhost:5432/mydb?sslmode=disable"
```

### 5.2 Query source flow

If using sqlc-generated internals in repository implementation:

1. update db/schema files
2. update db/queries files
3. regenerate code

```bash
make sqlc-generate
```

Important:

- sqlc output is implementation detail
- do not expose sqlc types in service/repository public contracts

## 6. Auth Workflow

Auth persistence uses the sqlc data layer; the goAuth boundary is unchanged.

Runtime sequence:

- app wiring creates the auth repository over the `storage.Postgres` boundary
- app wiring creates the sqlc-backed `StoreUserProvider` from the repository
- the goAuth engine (v0.4.0) is built with Redis + provider + tenancy settings

When testing auth routes:

- POST /api/v1/system/auth/login
- POST /api/v1/system/auth/mfa/confirm
- POST /api/v1/system/auth/refresh
- POST /api/v1/system/auth/logout
- GET /api/v1/system/whoami (requires auth)

See [docs/auth-goauth.md](auth-goauth.md) for details.

## 7. Verification Workflow Before PR

Run standard checks:

```bash
go test ./...
go build ./...
make verify
```

Architecture review checks:

- no handler bypass to the data layer
- no service running queries directly (`Queries(ctx)`) or touching pgx
- repositories obtain queries only via `Queries(ctx)`
- no repository controlling transaction boundaries (`WithTx`)
- one storage type per module
- policy order is valid

## 8. Troubleshooting Playbook

### 8.1 Startup fails at config lint

Likely causes:

- auth enabled without redis/postgres
- invalid duration/boolean env value
- prod fail-open configuration for cache/rate-limit

Action:

- check error message
- compare env values against [docs/environment-variables.md](environment-variables.md)

### 8.2 Startup fails at dependency init

Likely causes:

- Postgres URL invalid/unreachable
- Redis addr invalid/unreachable
- tracing endpoint misconfiguration

Action:

- validate network connectivity
- validate startup ping timeout values
- test dependencies independently

### 8.3 Route behavior looks wrong

Likely causes:

- policies attached in wrong order
- missing tenant policy for tenant route
- cache vary dimensions unsafe for authenticated route

Action:

- inspect route registration in module routes.go
- run verifier and review policy errors

## 9. Suggested Daily Loop

1. sync latest changes
2. run or restart API
3. implement one small module change
4. run focused tests
5. run full test/build/verify before push
6. update docs if behavior changed

## 10. Related Docs

- [docs/overview.md](overview.md)
- [docs/architecture.md](architecture.md)
- [docs/modules.md](modules.md)
- [docs/module_guide.md](module_guide.md)
- [docs/environment-variables.md](environment-variables.md)
