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
  (`Get` / `Insert` / `Replace` / `Delete` / `Find`), bound to the transaction
  in `ctx` when inside a transaction, or to the store otherwise.
- `document.WithTx(ctx, store, fn)` runs a write unit of work. On a backend that
  supports transactions (`document.TxStore`) it is atomic — writes inside `fn`
  commit on success and roll back on error or panic. On a backend that does not
  (e.g. standalone MongoDB), it runs `fn` directly.

Write intent is explicit so every backend behaves identically:

- `Insert` creates a document and fails with `document.ErrAlreadyExists` on a
  duplicate id.
- `Replace` upserts (create or overwrite by id).
- `Delete` returns `document.ErrNotFound` when the id is absent.

Documents are opaque (`ID` + raw `Data` payload); repositories own marshaling to
and from domain types, keeping document types out of their public contracts.

`Find` takes a `document.Query`:

- `document.ByFields(map[string]any{...})` — a portable exact-match conjunction
  over top-level payload fields that **every** backend supports.
- `document.Query{Native: ...}` — an optional backend-specific query value (e.g.
  a Mongo `bson.M`) for callers that opt into a particular backend. Backends
  ignore `Native` values they do not understand. Keep `Native` out of shared,
  backend-neutral repositories.

The bundled `document.NewInMemoryStore()` is dependency-free and suitable for
tests, examples, and local development. Swap in a real backend (e.g. Mongo) by
implementing `document.Store` (and `document.TxStore` if it supports
transactions) — no module code changes.

## Swapping in MongoDB (or any NoSQL)

The interface maps 1:1 onto a document database. Nothing in a module or
repository changes — only the store constructed in `BindDependencies`.

| document interface | MongoDB (`go.mongodb.org/mongo-driver`) |
|---|---|
| `Collection(ctx, name)` | `db.Collection(name)` (+ session from ctx) |
| `Get(ctx, id)` | `FindOne({_id: id})`; `ErrNoDocuments` → `ErrNotFound` |
| `Insert(ctx, doc)` | `InsertOne`; duplicate-key error → `ErrAlreadyExists` |
| `Replace(ctx, doc)` | `ReplaceOne({_id: id}, doc, SetUpsert(true))` |
| `Delete(ctx, id)` | `DeleteOne({_id: id})`; `DeletedCount==0` → `ErrNotFound` |
| `Find(ctx, query)` | `Find(filter)` where filter is `query.Native.(bson.M)` if set, else `bson.M` built from `query.Fields` |
| `WithTx` (TxStore) | `session.WithTransaction(...)` on a replica set; return `ErrTxUnsupported` on standalone |

A complete adapter (add `go.mongodb.org/mongo-driver` to `go.mod`, then):

```go
package mongostore

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/MrEthical07/superapi/internal/storage/document"
)

type Store struct{ db *mongo.Database }

func New(db *mongo.Database) *Store { return &Store{db: db} }

// storedDoc is how a document.Document is persisted: the id is _id and the raw
// payload is kept opaque under one field, so repositories keep owning encoding.
type storedDoc struct {
	ID   string `bson:"_id"`
	Data []byte `bson:"data"`
}

func (s *Store) Collection(ctx context.Context, name string) document.Collection {
	return &collection{c: s.db.Collection(name)}
}

// WithTx makes *Store a document.TxStore. On a replica set this is atomic; on a
// standalone deployment ReplaceOne-in-a-session errors and you can translate the
// "Transaction numbers are only allowed on a replica set" server error into
// document.ErrTxUnsupported (omitted here for brevity).
func (s *Store) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	sess, err := s.db.Client().StartSession()
	if err != nil {
		return err
	}
	defer sess.EndSession(ctx)
	_, err = sess.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		return nil, fn(sc) // sc carries the session; Collection picks it up via ctx
	})
	return err
}

type collection struct{ c *mongo.Collection }

func (col *collection) Get(ctx context.Context, id string) (document.Document, error) {
	var out storedDoc
	err := col.c.FindOne(ctx, bson.M{"_id": id}).Decode(&out)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return document.Document{}, document.ErrNotFound
	}
	if err != nil {
		return document.Document{}, err
	}
	return document.Document{ID: out.ID, Data: out.Data}, nil
}

func (col *collection) Insert(ctx context.Context, d document.Document) error {
	_, err := col.c.InsertOne(ctx, storedDoc{ID: d.ID, Data: d.Data})
	if mongo.IsDuplicateKeyError(err) {
		return document.ErrAlreadyExists
	}
	return err
}

func (col *collection) Replace(ctx context.Context, d document.Document) error {
	opts := options.Replace().SetUpsert(true)
	_, err := col.c.ReplaceOne(ctx, bson.M{"_id": d.ID}, storedDoc{ID: d.ID, Data: d.Data}, opts)
	return err
}

func (col *collection) Delete(ctx context.Context, id string) error {
	res, err := col.c.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return document.ErrNotFound
	}
	return nil
}

func (col *collection) Find(ctx context.Context, q document.Query) ([]document.Document, error) {
	var filter bson.M
	if native, ok := q.Native.(bson.M); ok {
		filter = native // caller opted into Mongo's full query power
	} else {
		filter = bson.M{}
		for k, v := range q.Fields { // portable exact-match conjunction
			filter["data."+k] = v // or index fields directly; see note below
		}
	}
	cur, err := col.c.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []document.Document
	for cur.Next(ctx) {
		var sd storedDoc
		if err := cur.Decode(&sd); err != nil {
			return nil, err
		}
		out = append(out, document.Document{ID: sd.ID, Data: sd.Data})
	}
	return out, cur.Err()
}
```

Then wire it in the module — the only change from the in-memory example:

```go
func (m *Module) BindDependencies(deps *app.Dependencies) {
	m.docs = mongostore.New(mongoDB)   // instead of document.NewInMemoryStore()
	m.svc  = newAuditService(m.docs)
}
```

Notes:

- **Portable `Fields` filtering** matches by exact equality. If you want it to
  hit `Fields` against the payload, either store the payload as a real BSON
  subdocument (decode `Data` into `bson.M` on write) so `data.<field>` filters
  work, or index those fields as top-level columns. For rich queries prefer
  `Query{Native: bson.M{...}}` in a Mongo-specific repository.
- **Standalone MongoDB has no multi-document transactions.** Return
  `document.ErrTxUnsupported` from `WithTx` (or don't implement `TxStore`); the
  free `document.WithTx` helper then runs the unit of work directly, so code
  written against it still works — it just isn't atomic on that deployment.
- This adapter costs nothing until you import it. Keep it in your own package
  (e.g. `internal/storage/document/mongostore`) so the Mongo driver is only
  pulled into `go.mod` when you actually use it.

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
