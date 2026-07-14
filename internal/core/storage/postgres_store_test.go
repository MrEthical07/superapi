package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// stubTx is a minimal pgx.Tx mock that records commit/rollback calls.
type stubTx struct {
	commitErr      error
	rollbackErr    error
	commitCalled   bool
	rollbackCalled bool
}

func (s *stubTx) Commit(ctx context.Context) error {
	s.commitCalled = true
	return s.commitErr
}

func (s *stubTx) Rollback(ctx context.Context) error {
	s.rollbackCalled = true
	return s.rollbackErr
}

func (s *stubTx) Begin(ctx context.Context) (pgx.Tx, error) { return nil, nil }
func (s *stubTx) Conn() *pgx.Conn                           { return nil }
func (s *stubTx) LargeObjects() pgx.LargeObjects            { return pgx.LargeObjects{} }
func (s *stubTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (s *stubTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (s *stubTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) { return nil, nil }
func (s *stubTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row        { return nil }
func (s *stubTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (s *stubTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }

// fakeBeginner implements txBeginner using a pre-built stubTx.
type fakeBeginner struct {
	tx  pgx.Tx
	err error
}

func (f *fakeBeginner) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	return f.tx, f.err
}

// newTestPostgres creates a Postgres boundary with a fake beginner and a
// sentinel non-nil pool (so nil-pool guards pass). The fake pool must never
// have any methods called on it during these unit tests.
func newTestPostgres(b txBeginner) *Postgres {
	return &Postgres{
		pool:  new(pgxpool.Pool),
		begin: b,
	}
}

func TestWithTx_NilFn(t *testing.T) {
	p := newTestPostgres(&fakeBeginner{})
	err := p.WithTx(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil fn, got nil")
	}
}

func TestWithTx_CommitOnSuccess(t *testing.T) {
	stub := &stubTx{}
	p := newTestPostgres(&fakeBeginner{tx: stub})

	err := p.WithTx(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stub.commitCalled {
		t.Fatal("expected Commit to be called on success")
	}
	if stub.rollbackCalled {
		t.Fatal("expected Rollback NOT to be called on success")
	}
}

func TestWithTx_RollbackOnCallbackError(t *testing.T) {
	stub := &stubTx{}
	p := newTestPostgres(&fakeBeginner{tx: stub})
	callbackErr := errors.New("callback error")

	err := p.WithTx(context.Background(), func(ctx context.Context) error {
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("expected callback error, got %v", err)
	}
	if stub.commitCalled {
		t.Fatal("expected Commit NOT to be called on callback error")
	}
	if !stub.rollbackCalled {
		t.Fatal("expected Rollback to be called on callback error")
	}
}

func TestWithTx_RollbackAndRepanicOnPanic(t *testing.T) {
	stub := &stubTx{}
	p := newTestPostgres(&fakeBeginner{tx: stub})

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected panic to be re-propagated")
		}
		if !stub.rollbackCalled {
			t.Fatal("expected Rollback to be called on panic")
		}
		if stub.commitCalled {
			t.Fatal("expected Commit NOT to be called on panic")
		}
	}()

	_ = p.WithTx(context.Background(), func(ctx context.Context) error {
		panic("test panic")
	})
}

func TestWithTx_CommitErrorPropagated(t *testing.T) {
	commitErr := errors.New("commit failed")
	stub := &stubTx{commitErr: commitErr}
	p := newTestPostgres(&fakeBeginner{tx: stub})

	err := p.WithTx(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error from commit failure, got nil")
	}
	if !errors.Is(err, commitErr) {
		t.Fatalf("expected commit error to be wrapped, got: %v", err)
	}
}

func TestWithTx_BeginErrorPropagated(t *testing.T) {
	beginErr := errors.New("begin failed")
	p := newTestPostgres(&fakeBeginner{err: beginErr})

	err := p.WithTx(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error from begin failure, got nil")
	}
	if !errors.Is(err, beginErr) {
		t.Fatalf("expected begin error to be wrapped, got: %v", err)
	}
}

// TestWithTx_BindsTxIntoContext verifies that the callback receives a context
// carrying the active transaction, so that Queries(ctx) inside the callback
// binds to the tx rather than the pool.
func TestWithTx_BindsTxIntoContext(t *testing.T) {
	stub := &stubTx{}
	p := newTestPostgres(&fakeBeginner{tx: stub})

	var boundTx pgx.Tx
	var ok bool
	err := p.WithTx(context.Background(), func(ctx context.Context) error {
		boundTx, ok = ctx.Value(txKey{}).(pgx.Tx)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected transaction to be bound into callback context")
	}
	if boundTx != pgx.Tx(stub) {
		t.Fatal("expected the begun transaction to be the one bound into context")
	}
}

// TestQueries_UsesTxWhenPresent verifies that Queries binds to the transaction
// stashed in context. It relies only on the context-key lookup, so no live
// database or pool method calls are required.
func TestQueries_UsesTxWhenPresent(t *testing.T) {
	p := newTestPostgres(&fakeBeginner{})

	ctx := context.WithValue(context.Background(), txKey{}, pgx.Tx(&stubTx{}))
	if q := p.Queries(ctx); q == nil {
		t.Fatal("expected non-nil queries bound to tx")
	}
}
