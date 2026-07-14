package document

import (
	"context"
	"errors"
)

// Sentinel errors. Backends map their native errors onto these so repositories
// depend only on this package, never on a driver.
var (
	// ErrNotFound is returned when a document does not exist for the given id.
	ErrNotFound = errors.New("document not found")
	// ErrAlreadyExists is returned by Insert when a document with the id exists.
	ErrAlreadyExists = errors.New("document already exists")
	// ErrTxUnsupported is returned by WithTx on a backend/deployment that cannot
	// provide multi-document transactions (for example a standalone MongoDB
	// server). Callers that require atomicity should surface this rather than
	// assume a silent non-atomic fallback.
	ErrTxUnsupported = errors.New("document transactions are not supported by this backend")
)

// Document is an opaque, backend-agnostic record: a string id and a raw payload
// (typically JSON). Repositories own marshaling to and from domain types, so the
// boundary never encodes domain structure. Backends decode the payload into
// their own representation (BSON for Mongo, JSONB for a document-in-SQL backend,
// etc.) and index the id.
type Document struct {
	ID   string
	Data []byte
}

// Query selects documents. It is intentionally backend-defined so each backend
// can express its native query power without this interface leaking driver
// types or capping expressiveness.
//
//   - Fields is a portable conjunction of exact field-equality conditions over
//     top-level fields of the (JSON) payload. An empty/omitted Fields matches
//     all documents. Every backend supports this.
//   - Native carries a backend-specific query value (for example a Mongo bson.M
//     or bson.D) for callers that opt into a particular backend. Backends ignore
//     Native values they do not understand; the in-memory store ignores it and
//     uses Fields only. Repositories that set Native accept a backend coupling
//     by doing so — keep it out of shared, backend-neutral repositories.
type Query struct {
	Fields map[string]any
	Native any
}

// AllDocuments is the zero query: it matches every document in a collection.
var AllDocuments = Query{}

// ByFields builds a portable exact-match query.
func ByFields(fields map[string]any) Query {
	return Query{Fields: fields}
}

// Collection is the per-collection operation surface handed to repositories.
// Obtain one from Store.Collection; the zero value is not valid.
//
// Write intent is explicit so every backend behaves identically:
//   - Insert creates a new document and fails with ErrAlreadyExists if the id
//     is taken (Mongo InsertOne).
//   - Replace upserts: it creates or overwrites by id (Mongo ReplaceOne with
//     upsert). Use it when you do not care whether the id already existed.
//   - Delete removes by id, returning ErrNotFound if absent.
type Collection interface {
	// Get returns the document with the given id, or ErrNotFound.
	Get(ctx context.Context, id string) (Document, error)
	// Insert creates a new document, failing with ErrAlreadyExists on a
	// duplicate id.
	Insert(ctx context.Context, doc Document) error
	// Replace inserts or overwrites a document by its id (upsert).
	Replace(ctx context.Context, doc Document) error
	// Delete removes a document by id, returning ErrNotFound if absent.
	Delete(ctx context.Context, id string) error
	// Find returns all documents matching the query (all for AllDocuments).
	Find(ctx context.Context, query Query) ([]Document, error)
}

// Store is the document storage boundary. It yields per-collection handles and,
// when the backend supports it, a write transaction scope. Implementations keep
// driver types out of this contract.
//
// Transactions are an optional capability: a Store need not support them. Use
// the free function WithTx to run a unit of work that participates in a
// transaction when the backend provides one and runs directly otherwise —
// callers that strictly require atomicity should type-assert TxStore or check
// for ErrTxUnsupported.
type Store interface {
	// Collection returns a handle bound to the transaction carried in ctx (when
	// inside a TxStore's WithTx) or to the store otherwise.
	Collection(ctx context.Context, name string) Collection
}

// TxStore is implemented by backends that support multi-document transactions.
// A Mongo-backed store implements it only against a replica set / mongos;
// against a standalone server its WithTx returns ErrTxUnsupported.
type TxStore interface {
	Store
	// WithTx runs fn inside a transaction. Repositories that use Collection(ctx)
	// within fn participate in the same transaction. Semantics follow the
	// relational WithTx: rollback on error or panic, commit otherwise. Returns
	// ErrTxUnsupported when the deployment cannot provide a transaction.
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// WithTx runs fn transactionally when the store supports transactions, and
// directly (fn against the plain store) when it does not. This lets a service
// own a write boundary uniformly: on a transactional backend the writes are
// atomic; on a non-transactional one they run without a transaction rather than
// failing. A service that must have atomicity should instead type-assert
// TxStore and handle ErrTxUnsupported explicitly.
func WithTx(ctx context.Context, store Store, fn func(ctx context.Context) error) error {
	if fn == nil {
		return errors.New("nil transaction callback")
	}
	if txStore, ok := store.(TxStore); ok {
		return txStore.WithTx(ctx, fn)
	}
	return fn(ctx)
}
