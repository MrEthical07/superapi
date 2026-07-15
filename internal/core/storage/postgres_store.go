package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
)

// txKey is the context key under which an in-flight pgx transaction is stashed
// by WithTx so that Queries can bind subsequent repository calls to that tx.
type txKey struct{}

// txBeginner begins transactions. The concrete pool satisfies it; tests supply
// a fake so transaction orchestration can be exercised without a live database.
type txBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// Postgres is the relational data-access boundary. It hands repositories sqlc
// queries bound to either the request transaction (when one is active) or the
// pool, and owns the transaction lifecycle via WithTx. It deliberately exposes
// no query surface of its own: repositories obtain *sqlcgen.Queries and call
// generated methods, keeping sqlc/pgx types out of service and module APIs.
type Postgres struct {
	pool  *pgxpool.Pool
	begin txBeginner
}

// NewPostgres creates a relational boundary backed by a pgx pool.
func NewPostgres(pool *pgxpool.Pool) (*Postgres, error) {
	if pool == nil {
		return nil, errors.New("nil postgres pool")
	}
	return &Postgres{pool: pool, begin: pool}, nil
}

// Queries returns sqlc queries bound to the transaction carried in ctx, or to
// the pool when no transaction is active. Repositories call this per operation
// so reads run on the pool and writes inside WithTx run on the same tx.
func (p *Postgres) Queries(ctx context.Context) *sqlcgen.Queries {
	if ctx != nil {
		if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
			return sqlcgen.New(tx)
		}
	}
	return sqlcgen.New(p.pool)
}

// WithTx runs fn inside a pgx transaction. The transaction is stashed in the
// context passed to fn, so any repository call using Queries(ctx) within fn
// participates in the same transaction. Services own this write boundary.
//
// On a returned error the transaction is rolled back; on a panic it is rolled
// back and the panic is re-propagated; otherwise it is committed.
func (p *Postgres) WithTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if p == nil || p.pool == nil {
		return errors.New("postgres boundary is not configured")
	}
	if fn == nil {
		return errors.New("nil transaction callback")
	}

	tx, err := p.begin.BeginTx(ctx, pgx.TxOptions{})
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

	txCtx := context.WithValue(ctx, txKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}

	shouldRollback = false
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
