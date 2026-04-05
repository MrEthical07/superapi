package storage

import (
	"context"
	"errors"
)

// NoopDocumentStore is a transaction-capable document store placeholder.
// It executes document operations through a no-op executor contract.
type NoopDocumentStore struct{}

// Kind identifies this store as document-oriented.
func (NoopDocumentStore) Kind() Kind {
	return KindDocument
}

// WithTx provides semantic transaction scopes for document backends.
func (NoopDocumentStore) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if fn == nil {
		return errors.New("nil transaction callback")
	}
	return fn(ctx)
}

// Execute runs one document operation with a no-op executor.
func (NoopDocumentStore) Execute(ctx context.Context, op DocumentOperation) error {
	if op == nil {
		return errors.New("nil document operation")
	}
	return op.ExecuteDocument(ctx, noopDocumentExecutor{})
}

type noopDocumentExecutor struct{}

func (noopDocumentExecutor) Run(context.Context, string, any, any) error {
	return nil
}
