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
