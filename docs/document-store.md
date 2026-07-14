# Optional Document Store

SuperAPI's data layer is relational (sqlc over pgx). Some modules also need
document-oriented (NoSQL) persistence. That is provided by an **optional,
self-contained** package: `internal/storage/document`.

## Optional by construction

The package lives outside `internal/core`. Core does not import it, and it does
not import core wiring. Because Go only compiles packages that are imported into
the build, a project that never wires a document store excludes it from the
binary automatically (package-level dead-code elimination), and a project that
wants it gone deletes the directory. Nothing in core references it.

You can confirm the exclusion:

```
go list -deps ./cmd/api | grep storage/document   # prints nothing by default
```

## Shape

The document boundary mirrors the relational one so the two read consistently:

- `document.Store.Collection(ctx, name)` returns a per-collection handle
  (`Get` / `Put` / `Delete` / `Find`), bound to the transaction in `ctx` when
  inside `WithTx`, or to the store otherwise.
- `document.Store.WithTx(ctx, fn)` owns the write transaction boundary. Writes
  inside `fn` commit atomically on success and roll back on error or panic.

Documents are opaque (`ID` + raw `Data` payload); repositories own marshaling to
and from domain types, keeping document types out of their public contracts.

The bundled `document.NewInMemoryStore()` is dependency-free and suitable for
tests, examples, and local development. Swap in a real backend (e.g. Mongo) by
implementing `document.Store` — no module code changes.

## Wiring in a module (no shared branching)

A module that needs documents constructs a `document.Store` in its **own**
`BindDependencies` and hands it to that module's repository. There is no
`if sql else mongo` branching in shared code (see AGENTS.md §3/§8):

```go
type Module struct {
    docs document.Store
    svc  *auditService
}

func (m *Module) BindDependencies(deps *app.Dependencies) {
    // Wire whichever document backend this module needs.
    m.docs = document.NewInMemoryStore()  // or a Mongo-backed document.Store
    m.svc  = newAuditService(m.docs)
}
```

A complete, compiling example — repository + service + transactional batch
write — lives in `internal/storage/document/example`. It is illustrative and is
**not** registered in `internal/modules/modules.go`; copy it into a real module
when you need it, or delete it (and the `document` package) if you do not.

## Removing it

Delete `internal/storage/document/` (and any module wiring you added). Nothing
in core or the runtime registry references it, so removal is a clean, bounded
deletion.
