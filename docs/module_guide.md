# Module Data Layer Guide

This guide focuses on one thing: how to implement module data access correctly
under the enforced sqlc data layer.

If you only need practical rules for service/repository code, this is the right
document.

## 1. Non-Negotiable Architecture Rules

Required flow:

Service -> Repository -> sqlc queries -> pgx (pool or transaction)

Hard constraints:

- service calls repository only
- service may call `DB().WithTx(...)` to define a write boundary, but runs no
  queries itself
- repository obtains generated queries via `DB().Queries(ctx)` and owns all
  query + mapping logic
- repository does not control transaction boundaries
- handler does not bypass service/repository
- one storage type per module

Why this exists:

- prevents architecture drift over time
- keeps business code backend-agnostic (no sqlc/pgx types on public interfaces)
- lets storage internals change with a small blast radius

The `superapi-verify` static checker enforces these rules and fails the build on
violations.

## 2. The Data-Access Boundary You Should Know

The relational data layer is a single thin type, `storage.Postgres`, reached
from a module via `m.runtime.DB()`. It has exactly two methods you use:

- `DB().Queries(ctx) *sqlcgen.Queries`
	- generated sqlc queries, bound to the active transaction in `ctx` or to the
	  pool — repositories call this per operation
- `DB().WithTx(ctx, func(txCtx) error { ... }) error`
	- opens a transaction, threads it through `txCtx`, commits/rolls back —
	  services call this to define write boundaries

Key idea: repositories run queries; services own transaction boundaries. There
are no store contracts or operation wrappers to learn — just these two methods.
See [docs/transactions.md](transactions.md) for the full transaction guide.

## 3. Choosing A Storage Type Per Module

Pick one backend family per module:

- relational module -> the `storage.Postgres` boundary (`DB()`), the default
- document module -> the optional `internal/storage/document` store, constructed
  in the module's own binding (see [docs/document-store.md](document-store.md))

Do not branch inside business flow with "if sql else document".

If you need both for a feature, split into separate modules with explicit
boundaries.

## 4. Service Layer Pattern

Service responsibilities:

- validate business-level input and rules
- orchestrate sequence of repository calls
- control write transaction boundaries

Service should not:

- construct queries
- call store execution methods directly
- depend on driver/query-object types

### 4.1 Write path service skeleton

Pattern:

1. validate request
2. call `s.repo.pg.WithTx(ctx, func(txCtx) error { ... })` (or a `DB()` handle
   the service holds)
3. call repository write methods with `txCtx` inside the callback
4. return domain output

### 4.2 Read path service skeleton

Pattern:

1. validate request
2. call the repository read method directly (passing `ctx`)
3. return domain output

No transaction wrapper unless you have a specific reason.

## 5. Repository Layer Pattern

Repository responsibilities:

- own query/filter/projection logic
- translate domain inputs into generated query params
- map generated rows to domain models
- map storage errors (e.g. `pgx.ErrNoRows`) to domain/app errors

Repository should not:

- orchestrate high-level business workflows
- open/commit/roll back transactions
- expose sqlc/pgx types in public interfaces

### 5.1 Relational repository pattern

A repository holds `*storage.Postgres` and, per method, calls
`r.pg.Queries(ctx).<GeneratedMethod>(ctx, params)`, then maps the generated row
to a domain model:

```go
func (r *Repo) GetByID(ctx context.Context, id string) (Item, error) {
    row, err := r.pg.Queries(ctx).GetItem(ctx, id)
    if err != nil {
        if errors.Is(err, pgx.ErrNoRows) {
            return Item{}, ErrItemNotFound
        }
        return Item{}, err
    }
    return mapItemRow(row), nil
}
```

`internal/core/auth/user_repository.go` is a complete real-world example.

### 5.2 Document repository pattern

For a document-backed module, the repository holds a `document.Store` (or
`document.TxStore`) instead and calls `Collection(...).Get/Insert/Replace/...`.
See the worked example in [docs/document-store.md](document-store.md).

Keep domain mapping in the repository either way.

## 6. Transaction Rules (Detailed)

Transactions run through `storage.Postgres.WithTx`; there is no per-backend store
transaction API to learn.

Rules:

- write paths use `WithTx`, called from the service
- read paths are direct repository calls by default
- repository methods must thread the incoming `ctx` into `Queries(ctx)` so they
  join the service's transaction when one is active
- if a repository drops the transaction context and uses a fresh one, its query
  silently runs on the pool outside the transaction — always thread `ctx`

Full detail and examples: [docs/transactions.md](transactions.md).

## 7. Interface Design Rules

Good repository interface:

- domain nouns and verbs
- clear use-case semantics
- no backend leakage

Examples:

- CreateOrder(ctx, input) (Order, error)
- GetOrderByID(ctx, tenantID, orderID) (Order, error)
- ListOrders(ctx, tenantID, filter) ([]Order, error)

Bad examples:

- ExecSQL(ctx, query, args...)
- QueryRows(ctx, stmt) (...)
- Find(ctx, bson.M)

## 8. Mapping Rules

Repository mapping direction:

- domain input -> generated query params
- generated row -> domain output

The `storage.Postgres` boundary stays domain-agnostic — it hands out queries and
owns transactions, and never sees domain types.

This rule keeps storage changes local to the repository implementation.

## 9. Using The Module Scaffold Safely

The generator gives a fast starter layout.

Before shipping:

- adjust generated service/repo contracts to domain-focused signatures
- ensure service does not drift into store/driver calls
- implement real repository operations and mappings
- add tests for read and write behaviors

## 10. Validation Checklist Before PR

1. Service file runs no queries directly; it only calls the repository and, for
   writes, `WithTx`.
2. Repository interface uses domain methods only (no sqlc/pgx types).
3. Repository implementation owns query/filter logic via `Queries(ctx)`.
4. Repository does not open/commit/roll back transactions.
5. Write paths are wrapped in `WithTx`.
6. Read paths avoid unnecessary transaction wrappers.
7. Repository methods thread `ctx` into `Queries(ctx)`.
8. Policy stacks are valid for protected routes.
9. go test ./..., go build ./..., and make verify pass.

## 11. Quick Anti-Pattern Table

| Anti-pattern | Why it is bad | Correct replacement |
|---|---|---|
| Service calls `Queries(ctx)` directly | Breaks architecture boundary | Move query code to the repository |
| Repository returns sqlc/pgx rows | Leaks backend details upward | Return domain models |
| Repository calls `WithTx` | Repository owns a transaction boundary | Move `WithTx` to the service |
| Repository uses a fresh context in a tx | Query runs outside the transaction | Thread the incoming `ctx` into `Queries(ctx)` |
| Handler performs business decisions | Hard to test and reuse | Move to service |
| One module switches between SQL/doc backends | Increases branch complexity and risk | Separate module or explicit backend choice |

## 12. Related References

- [docs/modules.md](modules.md)
- [docs/crud-examples.md](crud-examples.md)
- [docs/transactions.md](transactions.md)
- [docs/architecture.md](architecture.md)
- [docs/policies.md](policies.md)
