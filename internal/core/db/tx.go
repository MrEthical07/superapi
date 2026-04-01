package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
)

type txHandle interface {
	sqlcgen.DBTX
	Commit(context.Context) error
	Rollback(context.Context) error
}

// WithTx runs a callback in a transaction and discards callback return values.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(*sqlcgen.Queries) error) error {
	if pool == nil {
		return fmt.Errorf("nil pool")
	}
	if fn == nil {
		return fmt.Errorf("nil tx callback")
	}

	_, err := WithTxResult(ctx, pool, func(q *sqlcgen.Queries) (struct{}, error) {
		return struct{}{}, fn(q)
	})
	return err
}

// WithTxResult runs a callback in a transaction and returns a typed result.
func WithTxResult[T any](ctx context.Context, pool *pgxpool.Pool, fn func(*sqlcgen.Queries) (T, error)) (T, error) {
	var zero T
	if pool == nil {
		return zero, fmt.Errorf("nil pool")
	}
	if fn == nil {
		return zero, fmt.Errorf("nil tx callback")
	}

	return withTxResult(ctx, func(beginCtx context.Context) (txHandle, error) {
		return pool.BeginTx(beginCtx, pgx.TxOptions{})
	}, fn)
}

func withTxResult[T any](ctx context.Context, begin func(context.Context) (txHandle, error), fn func(*sqlcgen.Queries) (T, error)) (result T, err error) {
	tx, err := begin(ctx)
	if err != nil {
		return result, fmt.Errorf("begin tx: %w", err)
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

	result, err = fn(QueriesFrom(tx))
	if err != nil {
		return result, err
	}

	shouldRollback = false
	if err := tx.Commit(ctx); err != nil {
		return result, fmt.Errorf("commit tx: %w", err)
	}

	return result, nil
}
