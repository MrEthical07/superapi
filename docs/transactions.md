# Transactions

This guide explains how to use database transactions in module repository and
service code. It is short by design: transactions in SuperAPI need **no extra
imports and no new package** — they are two methods on the data-access boundary
you already have.

## 1. The whole API

The relational data layer is a single thin type, `storage.Postgres`, reached
from a module via `m.runtime.DB()`. It exposes exactly two methods:

```go
// Get generated sqlc queries, bound to the transaction in ctx (if any) or the pool.
func (p *Postgres) Queries(ctx context.Context) *sqlcgen.Queries

// Run fn inside a transaction; commit on nil error, roll back on error or panic.
func (p *Postgres) WithTx(ctx context.Context, fn func(ctx context.Context) error) error
```

That is the entire surface. There is no `Begin`/`Commit`/`Rollback` to call, no
transaction object to pass around, and no store/operation abstraction to learn.

## 2. The pattern

The rule is simple and enforced by `superapi-verify`:

- **Services** open transactions with `WithTx`. They run no queries themselves.
- **Repositories** run queries with `Queries(ctx)`. They never open, commit, or
  roll back a transaction.

```go
// SERVICE — owns the transaction boundary.
func (s *service) Transfer(ctx context.Context, in TransferInput) error {
    return s.repo.pg.WithTx(ctx, func(txCtx context.Context) error {
        if err := s.repo.Debit(txCtx, in.From, in.Amount); err != nil {
            return err // fn returns non-nil -> WithTx rolls back
        }
        return s.repo.Credit(txCtx, in.To, in.Amount)
    }) // fn returns nil -> WithTx commits
}

// REPOSITORY — just uses Queries(ctx). The same code works in or out of a tx.
func (r *Repo) Debit(ctx context.Context, id string, amt int64) error {
    return r.pg.Queries(ctx).DebitAccount(ctx, sqlcgen.DebitAccountParams{ID: id, Amount: amt})
}

func (r *Repo) Credit(ctx context.Context, id string, amt int64) error {
    return r.pg.Queries(ctx).CreditAccount(ctx, sqlcgen.CreditAccountParams{ID: id, Amount: amt})
}
```

## 3. How it works (why the repo doesn't need to know)

`WithTx` begins a `pgx` transaction and stashes it in the context it passes to
`fn` (under a private key). `Queries(ctx)` looks for that transaction:

- if present, it binds the generated queries to the transaction
- if absent, it binds them to the pool

So a repository method written **once** participates in a transaction when called
with the transaction context, and runs as a standalone statement on the pool
otherwise. The repository is transaction-agnostic; the service decides.

## 4. Reads don't need a transaction

Plain reads call the repository directly — no `WithTx`:

```go
func (s *service) Get(ctx context.Context, id string) (Item, error) {
    return s.repo.GetByID(ctx, id) // runs on the pool
}
```

Only wrap a read in `WithTx` if you specifically need a consistent snapshot
across multiple statements.

## 5. Commit, rollback, and panics

`WithTx` handles the lifecycle for you:

- `fn` returns `nil` -> the transaction commits.
- `fn` returns an error -> the transaction rolls back and that error is returned.
- `fn` panics -> the transaction rolls back and the panic is re-propagated.

You never call commit or rollback yourself.

## 6. The one gotcha: thread the context

The transaction lives entirely in the context. If a repository (or service) drops
the `txCtx` and uses a fresh context, the query silently runs on the pool
**outside** the transaction — no error is raised, but atomicity is lost.

```go
// WRONG — ignores the transaction, runs on the pool.
err := s.repo.pg.WithTx(ctx, func(txCtx context.Context) error {
    return s.repo.Debit(context.Background(), id, amt) // BUG: not in the tx
})

// RIGHT — thread txCtx all the way down.
err := s.repo.pg.WithTx(ctx, func(txCtx context.Context) error {
    return s.repo.Debit(txCtx, id, amt)
})
```

Rule of thumb: pass the `ctx` you were given straight into `Queries(ctx)` and
into every call you make inside `WithTx`. Never substitute
`context.Background()` or `context.TODO()` in a data-access path.

## 7. Nesting

`WithTx` does not open a nested (savepoint) transaction — if you call it again
with a context that already carries a transaction, a fresh top-level transaction
is begun on the pool for the inner call. Keep a single `WithTx` at the service
boundary of a write use-case and do all repository writes inside that one
callback, rather than nesting `WithTx` calls.

## 8. Testing

Because the boundary is a small type, services and repositories are easy to test.
`storage.Postgres` accepts a transaction beginner, so orchestration (commit on
success, rollback on error) can be exercised with a fake without a live database;
see `internal/core/storage/postgres_store_test.go`. For query behavior, test the
repository against a real Postgres (or a container) since sqlc runs real SQL.

## 9. Related docs

- [docs/module_guide.md](module_guide.md) — data-layer rules for modules
- [docs/crud-examples.md](crud-examples.md) — a full CRUD module using this pattern
- [docs/architecture.md](architecture.md) — the data-access boundary in context
