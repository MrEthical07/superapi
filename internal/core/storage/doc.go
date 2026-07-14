// Package storage is the relational data-access boundary for repositories.
//
// It exposes a single Postgres type that hands repositories sqlc-generated
// queries bound to either the request transaction or the pool, and owns the
// transaction lifecycle via WithTx. Services control the write boundary;
// repositories obtain *sqlcgen.Queries and never leak sqlc or pgx types into
// service or module interfaces.
package storage
