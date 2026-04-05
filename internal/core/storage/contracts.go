package storage

import "context"

// Kind identifies the storage backend family.
type Kind string

const (
	KindRelational Kind = "relational"
	KindDocument   Kind = "document"
)

// Store is the root store contract for backend identification.
type Store interface {
	Kind() Kind
}

// TransactionalStore is mandatory for all stores.
// Implementations can provide backend-native or semantic no-op transactions.
type TransactionalStore interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// RelationalStore executes repository-defined relational operations.
type RelationalStore interface {
	Store
	TransactionalStore
	Execute(ctx context.Context, op RelationalOperation) error
}

// DocumentStore executes repository-defined document operations.
type DocumentStore interface {
	Store
	TransactionalStore
	Execute(ctx context.Context, op DocumentOperation) error
}

// RowScanner is the minimal row scanning contract used by repositories.
type RowScanner interface {
	Scan(dest ...any) error
}

// RelationalExecutor is the execution surface used by relational operations.
type RelationalExecutor interface {
	Exec(ctx context.Context, query string, args ...any) error
	QueryRow(ctx context.Context, query string, scan func(RowScanner) error, args ...any) error
	Query(ctx context.Context, query string, scan func(RowScanner) error, args ...any) error
}

// DocumentExecutor is the execution surface used by document operations.
type DocumentExecutor interface {
	Run(ctx context.Context, command string, payload any, out any) error
}

// RelationalOperation encapsulates repository-owned relational query logic.
type RelationalOperation interface {
	ExecuteRelational(ctx context.Context, exec RelationalExecutor) error
}

// DocumentOperation encapsulates repository-owned document query logic.
type DocumentOperation interface {
	ExecuteDocument(ctx context.Context, exec DocumentExecutor) error
}
