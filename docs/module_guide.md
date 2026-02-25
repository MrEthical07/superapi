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
