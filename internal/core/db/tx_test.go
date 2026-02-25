package db

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MrEthical07/superapi/internal/core/db/sqlcgen"
)

type fakeTx struct {
	commitCalled   bool
	rollbackCalled bool
	commitErr      error
	rollbackErr    error
}

func (f *fakeTx) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *fakeTx) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, nil
}

func (f *fakeTx) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return nil
}

func (f *fakeTx) Commit(context.Context) error {
	f.commitCalled = true
	return f.commitErr
}

func (f *fakeTx) Rollback(context.Context) error {
	f.rollbackCalled = true
	return f.rollbackErr
}

func TestWithTxResult_SuccessCommits(t *testing.T) {
	tx := &fakeTx{}
	beginCalled := false

	got, err := withTxResult(context.Background(), func(context.Context) (txHandle, error) {
		beginCalled = true
		return tx, nil
	}, func(q *sqlcgen.Queries) (int, error) {
		if q == nil {
			t.Fatalf("queries handle is nil")
		}
		return 42, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Fatalf("result = %d, want 42", got)
	}
	if !beginCalled {
		t.Fatalf("expected begin to be called")
	}
	if !tx.commitCalled {
		t.Fatalf("expected commit to be called")
	}
	if tx.rollbackCalled {
		t.Fatalf("expected rollback not to be called")
	}
}

func TestWithTxResult_CallbackErrorRollsBack(t *testing.T) {
	tx := &fakeTx{}
	callbackErr := errors.New("callback failed")

	_, err := withTxResult(context.Background(), func(context.Context) (txHandle, error) {
		return tx, nil
	}, func(q *sqlcgen.Queries) (struct{}, error) {
		return struct{}{}, callbackErr
	})

	if !errors.Is(err, callbackErr) {
		t.Fatalf("error = %v, want callback error", err)
	}
	if !tx.rollbackCalled {
		t.Fatalf("expected rollback to be called")
	}
	if tx.commitCalled {
		t.Fatalf("expected commit not to be called")
	}
}

func TestWithTxResult_PanicRollsBackAndRepanics(t *testing.T) {
	tx := &fakeTx{}
	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatalf("expected panic")
		}
		if !tx.rollbackCalled {
			t.Fatalf("expected rollback to be called on panic")
		}
		if tx.commitCalled {
			t.Fatalf("expected commit not to be called on panic")
		}
	}()

	_, _ = withTxResult(context.Background(), func(context.Context) (txHandle, error) {
		return tx, nil
	}, func(q *sqlcgen.Queries) (struct{}, error) {
		panic("boom")
	})
}

func TestWithTxResult_CommitFailureReturned(t *testing.T) {
	commitErr := errors.New("commit failed")
	tx := &fakeTx{commitErr: commitErr}

	_, err := withTxResult(context.Background(), func(context.Context) (txHandle, error) {
		return tx, nil
	}, func(q *sqlcgen.Queries) (struct{}, error) {
		return struct{}{}, nil
	})

	if !errors.Is(err, commitErr) {
		t.Fatalf("error = %v, want wrapped commit error", err)
	}
	if !tx.commitCalled {
		t.Fatalf("expected commit to be called")
	}
	if tx.rollbackCalled {
		t.Fatalf("expected rollback not to be called after successful callback")
	}
}

func TestWithTxResult_RollbackFailureDoesNotMaskCallbackError(t *testing.T) {
	tx := &fakeTx{rollbackErr: errors.New("rollback failed")}
	callbackErr := errors.New("callback failed")

	_, err := withTxResult(context.Background(), func(context.Context) (txHandle, error) {
		return tx, nil
	}, func(q *sqlcgen.Queries) (struct{}, error) {
		return struct{}{}, callbackErr
	})

	if !errors.Is(err, callbackErr) {
		t.Fatalf("error = %v, want callback error", err)
	}
}

func TestWithTx_NilPool(t *testing.T) {
	err := WithTx(context.Background(), nil, func(q *sqlcgen.Queries) error { return nil })
	if err == nil {
		t.Fatalf("expected error for nil pool")
	}
}
