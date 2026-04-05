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

func (s *stubTx) Begin(ctx context.Context) (pgx.Tx, error)  { return nil, nil }
func (s *stubTx) Conn() *pgx.Conn                            { return nil }
func (s *stubTx) LargeObjects() pgx.LargeObjects             { return pgx.LargeObjects{} }
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

// newTestStore creates a PostgresRelationalStore with a fake beginner and a
// sentinel non-nil pool (so nil-pool guards pass). The fake pool must never
// have any methods called on it during these unit tests.
func newTestStore(b txBeginner) *PostgresRelationalStore {
	return &PostgresRelationalStore{
		pool:  new(pgxpool.Pool),
		begin: b,
	}
}

func TestWithTx_NilFn(t *testing.T) {
	store := newTestStore(&fakeBeginner{})
	err := store.WithTx(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil fn, got nil")
	}
}

func TestWithTx_CommitOnSuccess(t *testing.T) {
	stub := &stubTx{}
	store := newTestStore(&fakeBeginner{tx: stub})

	err := store.WithTx(context.Background(), func(ctx context.Context) error {
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
	store := newTestStore(&fakeBeginner{tx: stub})
	callbackErr := errors.New("callback error")

	err := store.WithTx(context.Background(), func(ctx context.Context) error {
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
	store := newTestStore(&fakeBeginner{tx: stub})

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

	_ = store.WithTx(context.Background(), func(ctx context.Context) error {
		panic("test panic")
	})
}

func TestWithTx_CommitErrorPropagated(t *testing.T) {
	commitErr := errors.New("commit failed")
	stub := &stubTx{commitErr: commitErr}
	store := newTestStore(&fakeBeginner{tx: stub})

	err := store.WithTx(context.Background(), func(ctx context.Context) error {
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
	store := newTestStore(&fakeBeginner{err: beginErr})

	err := store.WithTx(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error from begin failure, got nil")
	}
	if !errors.Is(err, beginErr) {
		t.Fatalf("expected begin error to be wrapped, got: %v", err)
	}
}

func TestExecute_NilOp(t *testing.T) {
	store := newTestStore(&fakeBeginner{})
	err := store.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil operation, got nil")
	}
}

func TestNoopDocumentStore_WithTx_NilFn(t *testing.T) {
	var s NoopDocumentStore
	err := s.WithTx(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil fn, got nil")
	}
}

func TestNoopDocumentStore_Execute_NilOp(t *testing.T) {
	var s NoopDocumentStore
	err := s.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil operation, got nil")
	}
}

func TestNoopDocumentStore_WithTx_CallsFn(t *testing.T) {
	var s NoopDocumentStore
	called := false
	err := s.WithTx(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected fn to be called")
	}
}
