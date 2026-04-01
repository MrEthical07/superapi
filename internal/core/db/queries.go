package db

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
)

var _ sqlcgen.DBTX = (*pgxpool.Pool)(nil)

// NewQueries returns typed sqlc queries for the provided DB handle.
func NewQueries(db sqlcgen.DBTX) *sqlcgen.Queries {
	return QueriesFrom(db)
}

// QueriesFrom constructs sqlc queries from any sqlc-compatible DBTX.
func QueriesFrom(db sqlcgen.DBTX) *sqlcgen.Queries {
	return sqlcgen.New(db)
}

// QueriesFromTx constructs sqlc queries bound to a pgx transaction.
func QueriesFromTx(tx pgx.Tx) *sqlcgen.Queries {
	return sqlcgen.New(tx)
}
