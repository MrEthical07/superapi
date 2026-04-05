package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresRunner interface {
	Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, query string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) pgx.Row
}

// txBeginner is an internal interface for beginning transactions, enabling testing.
type txBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type txRunnerKey struct{}

// PostgresRelationalStore executes relational repository operations over pgx.
type PostgresRelationalStore struct {
	pool  *pgxpool.Pool
	begin txBeginner
}

// NewPostgresRelationalStore creates a relational store backed by a pgx pool.
func NewPostgresRelationalStore(pool *pgxpool.Pool) (*PostgresRelationalStore, error) {
	if pool == nil {
		return nil, errors.New("nil postgres pool")
	}
	return &PostgresRelationalStore{pool: pool, begin: pool}, nil
}

// Kind identifies this store as relational.
func (s *PostgresRelationalStore) Kind() Kind {
	return KindRelational
}

// Execute runs one repository-defined relational operation.
func (s *PostgresRelationalStore) Execute(ctx context.Context, op RelationalOperation) error {
	if s == nil || s.pool == nil {
		return errors.New("relational store is not configured")
	}
	if op == nil {
		return errors.New("nil relational operation")
	}

	runner := s.runnerFromContext(ctx)
	exec := &postgresRelationalExecutor{runner: runner}
	return op.ExecuteRelational(ctx, exec)
}

// WithTx enforces a transaction scope for write-path orchestration.
func (s *PostgresRelationalStore) WithTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if s == nil || s.pool == nil {
		return errors.New("relational store is not configured")
	}
	if fn == nil {
		return errors.New("nil transaction callback")
	}

	tx, err := s.begin.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	shouldRollback := true
	defer func() {
		if rec := recover(); rec != nil {
			if shouldRollback {
				_ = tx.Rollback(ctx)
			}
			panic(rec)
		}
		if shouldRollback {
			_ = tx.Rollback(ctx)
		}
	}()

	txCtx := context.WithValue(ctx, txRunnerKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}

	shouldRollback = false
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *PostgresRelationalStore) runnerFromContext(ctx context.Context) postgresRunner {
	if ctx != nil {
		if tx, ok := ctx.Value(txRunnerKey{}).(pgx.Tx); ok {
			return tx
		}
	}
	return s.pool
}

type postgresRelationalExecutor struct {
	runner postgresRunner
}

func (e *postgresRelationalExecutor) Exec(ctx context.Context, query string, args ...any) error {
	_, err := e.runner.Exec(ctx, query, args...)
	return err
}

func (e *postgresRelationalExecutor) QueryRow(ctx context.Context, query string, scan func(RowScanner) error, args ...any) error {
	if scan == nil {
		return errors.New("nil scan callback")
	}
	row := e.runner.QueryRow(ctx, query, args...)
	return scan(row)
}

func (e *postgresRelationalExecutor) Query(ctx context.Context, query string, scan func(RowScanner) error, args ...any) error {
	if scan == nil {
		return errors.New("nil scan callback")
	}

	rows, err := e.runner.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
