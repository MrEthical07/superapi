package document

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a document does not exist for the given id.
var ErrNotFound = errors.New("document not found")

// Document is an opaque, backend-agnostic record: a string id and a raw payload
// (typically JSON). Repositories own marshaling to and from domain types, so the
// boundary never encodes domain structure.
type Document struct {
	ID   string
	Data []byte
}

// Filter selects documents by exact field-equality on decoded top-level JSON
// fields. An empty filter matches all documents. Backends may implement richer
// query support; this minimal shape keeps the interface portable.
type Filter map[string]any

// Collection is the per-collection operation surface handed to repositories.
// A nil-safe zero value is never valid; obtain one from Store.Collection.
type Collection interface {
	// Get returns the document with the given id, or ErrNotFound.
	Get(ctx context.Context, id string) (Document, error)
	// Put inserts or replaces a document by its id.
	Put(ctx context.Context, doc Document) error
	// Delete removes a document by id, returning ErrNotFound if absent.
	Delete(ctx context.Context, id string) error
	// Find returns all documents matching the filter (all when empty).
	Find(ctx context.Context, filter Filter) ([]Document, error)
}

// Store is the document storage boundary. It yields per-collection handles and
// owns the write transaction lifecycle, mirroring the relational boundary so the
// two read consistently. Implementations keep document driver types out of this
// contract.
type Store interface {
	// Collection returns a handle bound to the transaction carried in ctx (when
	// inside WithTx) or to the store otherwise.
	Collection(ctx context.Context, name string) Collection
	// WithTx runs fn inside a document transaction. Repositories that use
	// Collection(ctx) within fn participate in the same transaction. Semantics
	// follow the relational WithTx: rollback on error or panic, commit
	// otherwise. Backends without native transactions provide a semantic scope.
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}
