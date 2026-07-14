// Package document is an optional, self-contained document (NoSQL) storage
// boundary for modules that need document-oriented persistence alongside — but
// separate from — the relational data layer.
//
// It is deliberately located outside internal/core: core does not import it,
// and it does not import core wiring. Because Go only compiles packages that are
// imported into the build, a project that never wires a document store pays
// nothing for this package, and a project that wants it gone deletes this
// directory. This is package-level dead-code elimination by construction.
//
// # Shape
//
// The package mirrors the relational boundary's ergonomics: a module obtains a
// per-operation collection handle via Store.Collection and runs a write unit of
// work via the free WithTx helper. It defines its own small interface so
// backends (the bundled in-memory store, or a Mongo/other implementation you
// add) can be swapped without touching module code.
//
// The interface is designed to map cleanly onto a real document database:
//
//   - Write intent is explicit — Insert (create, fail on duplicate) vs Replace
//     (upsert) — matching Mongo's InsertOne vs ReplaceOne(upsert). Native
//     errors map onto ErrNotFound / ErrAlreadyExists.
//   - Find takes a Query with a portable Fields conjunction plus an optional
//     backend-specific Native value (e.g. a Mongo bson.M) so a backend can
//     expose its full query power without this interface leaking driver types.
//   - Transactions are an optional capability (TxStore). The free WithTx helper
//     runs a unit of work transactionally when the backend supports it and
//     directly otherwise, so a standalone MongoDB (no transactions) still works.
//
// See docs/document-store.md for a complete MongoDB adapter you can drop in.
//
// # Wiring (per module, no shared branching)
//
// A module that needs documents constructs a Store in its own BindDependencies
// and hands it to that module's repository — there is no "if sql else mongo"
// branching in shared code (see AGENTS.md). Example:
//
//	type Module struct {
//	    docs   document.Store
//	    repo   *auditRepo
//	}
//
//	func (m *Module) BindDependencies(deps *app.Dependencies) {
//	    // Wire whichever document backend this module needs. The in-memory
//	    // store is dependency-free and suitable for tests/examples; swap in a
//	    // Mongo-backed document.Store in production.
//	    m.docs = document.NewInMemoryStore()
//	    m.repo = newAuditRepo(m.docs)
//	}
//
// The repository depends on document.Store (not on a concrete backend), keeps
// document driver types out of its public contract, and owns storage-model to
// domain-model mapping — the same rules the relational layer follows.
package document
