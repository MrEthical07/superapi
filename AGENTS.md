# Project Guidelines

Use these for every task in this workspace.

## Engineering Rules

- Production-ready from day 1 (no demo shortcuts).
- No over-engineering.
- Keep hot path lean.
- No hidden magic/global state where avoidable.
- Backward-compatible refactors where possible.
- Prefer explicit interfaces over premature abstractions.

## Code Style

- Language: Go 1.26+ (`go.mod`).
- Keep handlers/services explicit and small; follow constructor + method patterns in `internal/core/app/app.go` and `internal/core/db/tx.go`.
- Reuse typed app errors from `internal/core/errors/errors.go`; return API errors through `internal/core/response/response.go`.
- For JSON endpoints, use `httpx.JSON` (`internal/core/httpx/typedjson.go`) for strict decoding and validation.
- Do not edit generated sqlc files in `internal/core/db/sqlcgen/`.

## Architecture

- Real server bootstrap is `cmd/api/main.go` (config load/lint, logger init, app run). Root `main.go` is a placeholder.
- Modules implement `app.Module` and register routes via `httpx.Router` (`internal/core/app/app.go`, `internal/core/httpx/router.go`).
- Module registry is centralized in `internal/modules/modules.go`.
- Dependencies (Postgres, Redis, readiness) are wired in `internal/core/app/deps.go`; modules may bind via `DependencyBinder`.

## Build and Test

- Primary checks: `go test ./...` and `go build ./...`.
- Make targets: `make fmt`, `make vet`, `make test`, `make sqlc-generate`, `make migrate-*`.
- For running the API, prefer `go run ./cmd/api`.
- sqlc generation: `sqlc generate` (or `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate`).

## Project Conventions

- Readiness model: `/healthz` is liveness; `/readyz` reports dependency readiness and may return 503 (`internal/modules/health/routes.go`, `internal/core/readiness/service.go`).
- Middleware behavior is env-driven (`internal/core/config/config.go` and `internal/core/httpx/globalmiddleware.go`).
- Keep config validation through `config.Lint` during startup (`cmd/api/main.go`).
- DB workflow baseline:
	1. Add migration in `db/migrations/`
	2. Mirror schema in `db/schema/`
	3. Add queries in `db/queries/`
	4. Regenerate sqlc
- Keep transaction boundaries in service layer (see `docs/module_guide.md` and `internal/core/db/tx.go`).

## Integration Points

- Postgres: `internal/core/db/postgres.go`, enabled by `POSTGRES_ENABLED` and validated at startup.
- Redis: `internal/core/cache/redis.go`, enabled by `REDIS_ENABLED` and validated at startup.
- Query access should go through `internal/core/db/queries.go` wrappers.

## Security

- Keep recoverer/request-id middleware enabled by default (`internal/core/httpx/recover.go`, `internal/core/httpx/requestid.go`).
- Use `MaxBodyBytes` and strict typed JSON decode to reduce input abuse (`internal/core/httpx/maxbody.go`, `internal/core/httpx/typedjson.go`).
- Do not leak internal errors to clients; preserve centralized sanitization in `internal/core/response/response.go`.
- Security headers are optional but supported via middleware toggles (`internal/core/httpx/securityheaders.go`).
