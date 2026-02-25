package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeDBTX struct{}

func (f *fakeDBTX) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeDBTX) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, nil
}

func (f *fakeDBTX) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return nil
}

func TestNewQueries(t *testing.T) {
	q := NewQueries(&fakeDBTX{})
	if q == nil {
		t.Fatalf("NewQueries() = nil")
	}
}
