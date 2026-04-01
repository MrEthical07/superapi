# Module Data Layer Guide

This project uses a global baseline for schema + typed SQL generation:

- Migrations: `db/migrations/`
- sqlc schema source: `db/schema/`
- sqlc query files: `db/queries/`
- Generated code: `internal/core/db/sqlcgen/`

## Rules

- Do not edit generated files in `internal/core/db/sqlcgen/` manually.
- Add schema changes through versioned migrations first.
- Keep SQL in `.sql` files; avoid runtime string-built SQL in module code.
- Run `sqlc generate` after changing schema/query files and commit generated output.

## Typical workflow

1. Create migration:

   - `migrate create -ext sql -dir db/migrations -seq add_my_table`

2. Update schema mirror in `db/schema/` to reflect final table/queryable shape.

3. Add/update SQL files in `db/queries/`.

4. Regenerate typed queries:

   - `sqlc generate`

5. Run checks:

   - `go fmt ./...`
   - `go test ./...`
   - `go build ./...`

## Integration

- Construct query set from pool/transaction-compatible DBTX via `internal/core/db.NewQueries(...)`.
- Keep module service/repository layers thin and typed around generated query methods.

## Module data access conventions

## Route policies (core engine)

Routes can attach per-route policies during registration through `httpx.Router`:

```go
r.Handle(http.MethodPost, "/api/v1/example", handler,
   policy.RequireJSON(),
   policy.WithHeader("X-Example", "1"),
)
```

### Scaffold quick start

```bash
make module name=projects
```

Expected output:

```text
generated module "projects" (package="projects" route=/api/v1/projects)
```

Quick checks after generation:

```bash
go test ./internal/devx/modulegen ./internal/modules/projects
go run ./cmd/superapi-verify ./internal/modules/projects
```

Or run `make module` with no `name` to use the interactive wizard. Create files manually only if you want a custom layout.

Policy type:

- `type Policy func(http.Handler) http.Handler`

Ordering rule (deterministic):

- For route policies `[P1, P2, P3]`, execution is:
   - request: `P1 -> P2 -> P3 -> handler`
   - response unwind: `handler -> P3 -> P2 -> P1`

This means the first listed policy is outermost. Use this convention consistently:

- Place authentication/authorization policies first (outermost) once introduced.
- Place mutation/caching policies carefully after auth checks.
- Policies may short-circuit by writing a response and not calling `next`.

Built-in utility policies currently available:

- `policy.Noop()`
- `policy.RequireJSON()`
- `policy.WithHeader(key, value)`

Service layer owns transaction boundaries. Repositories should never begin/commit transactions.

Recommended shape:

- `internal/modules/<module>/repo.go`
   - accepts `*sqlcgen.Queries` (or narrow interface over generated methods)
   - performs DB operations only
- `internal/modules/<module>/service.go`
   - orchestrates business logic
   - for read-only paths, use queries bound to pool: `db.NewQueries(pool)`
   - for transactional paths, use `db.WithTx(...)` / `db.WithTxResult(...)`

### Read-only flow

1. Service creates query handle bound to pool.
2. Service/repo executes typed sqlc methods.
3. Errors propagate to handler mapping layer.

### Transactional flow

1. Service calls `db.WithTx(ctx, pool, fn)` (or result variant).
2. Helper begins transaction with context.
3. Helper binds `sqlc` queries to tx and passes them to callback.
4. On callback success: commit.
5. On callback error: rollback and return original callback error.
6. On panic: rollback (best effort) and re-panic.

### Error semantics

- Rollback failures are intentionally best effort and do not mask callback errors.
- Commit failure is returned when callback succeeds but commit fails.
