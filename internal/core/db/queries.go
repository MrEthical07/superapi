package db

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
)

var _ sqlcgen.DBTX = (*pgxpool.Pool)(nil)

func NewQueries(db sqlcgen.DBTX) *sqlcgen.Queries {
	return QueriesFrom(db)
}

func QueriesFrom(db sqlcgen.DBTX) *sqlcgen.Queries {
	return sqlcgen.New(db)
}

func QueriesFromTx(tx pgx.Tx) *sqlcgen.Queries {
	return sqlcgen.New(tx)
}
