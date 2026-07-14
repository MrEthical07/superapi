package document

import (
	"context"
	"errors"
	"testing"
)

func doc(id, data string) Document {
	return Document{ID: id, Data: []byte(data)}
}

func TestInMemoryStore_PutGetDelete(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	col := s.Collection(ctx, "audit")

	if err := col.Put(ctx, doc("a", `{"action":"login"}`)); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := col.Get(ctx, "a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Data) != `{"action":"login"}` {
		t.Fatalf("data=%s", got.Data)
	}

	if err := col.Delete(ctx, "a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := col.Get(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestInMemoryStore_GetMissing(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	if _, err := s.Collection(ctx, "c").Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestInMemoryStore_DeleteMissing(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	if err := s.Collection(ctx, "c").Delete(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestInMemoryStore_PutRequiresID(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	if err := s.Collection(ctx, "c").Put(ctx, Document{Data: []byte("{}")}); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestInMemoryStore_Find(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	col := s.Collection(ctx, "events")
	_ = col.Put(ctx, doc("1", `{"kind":"a","n":1}`))
	_ = col.Put(ctx, doc("2", `{"kind":"b","n":2}`))
	_ = col.Put(ctx, doc("3", `{"kind":"a","n":3}`))

	all, err := col.Find(ctx, nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("find all: len=%d err=%v", len(all), err)
	}
	// Results are id-sorted.
	if all[0].ID != "1" || all[2].ID != "3" {
		t.Fatalf("find not id-sorted: %v", []string{all[0].ID, all[1].ID, all[2].ID})
	}

	filtered, err := col.Find(ctx, Filter{"kind": "a"})
	if err != nil || len(filtered) != 2 {
		t.Fatalf("find filtered: len=%d err=%v", len(filtered), err)
	}

	// Native int filter must match json float64 payloads.
	byN, err := col.Find(ctx, Filter{"n": 2})
	if err != nil || len(byN) != 1 || byN[0].ID != "2" {
		t.Fatalf("find by n: %+v err=%v", byN, err)
	}
}

func TestInMemoryStore_IsolationClone(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	col := s.Collection(ctx, "c")

	original := []byte(`{"v":1}`)
	_ = col.Put(ctx, Document{ID: "x", Data: original})
	original[2] = 'X' // mutate the caller's slice after Put

	got, _ := col.Get(ctx, "x")
	if string(got.Data) != `{"v":1}` {
		t.Fatalf("store did not clone payload on write: %s", got.Data)
	}
	got.Data[2] = 'Y' // mutate the returned slice
	again, _ := col.Get(ctx, "x")
	if string(again.Data) != `{"v":1}` {
		t.Fatalf("store did not clone payload on read: %s", again.Data)
	}
}

func TestInMemoryStore_WithTxCommit(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	err := s.WithTx(ctx, func(txCtx context.Context) error {
		col := s.Collection(txCtx, "c")
		if err := col.Put(txCtx, doc("a", `{"ok":true}`)); err != nil {
			return err
		}
		// Writes are visible within the transaction.
		if _, err := col.Get(txCtx, "a"); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withtx: %v", err)
	}

	// Committed after the transaction.
	if _, err := s.Collection(ctx, "c").Get(ctx, "a"); err != nil {
		t.Fatalf("expected committed doc, got %v", err)
	}
}

func TestInMemoryStore_WithTxRollbackOnError(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()
	sentinel := errors.New("boom")

	err := s.WithTx(ctx, func(txCtx context.Context) error {
		_ = s.Collection(txCtx, "c").Put(txCtx, doc("a", `{}`))
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}

	// Nothing committed.
	if _, err := s.Collection(ctx, "c").Get(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected rollback (ErrNotFound), got %v", err)
	}
}

func TestInMemoryStore_WithTxIsolatedUntilCommit(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	// Pre-seed a committed doc.
	_ = s.Collection(ctx, "c").Put(ctx, doc("seed", `{"v":0}`))

	_ = s.WithTx(ctx, func(txCtx context.Context) error {
		_ = s.Collection(txCtx, "c").Put(txCtx, doc("staged", `{"v":1}`))
		// A non-tx read must not see the staged write yet.
		if _, err := s.Collection(ctx, "c").Get(ctx, "staged"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("staged write leaked outside tx: %v", err)
		}
		return nil
	})

	if _, err := s.Collection(ctx, "c").Get(ctx, "staged"); err != nil {
		t.Fatalf("expected staged doc committed, got %v", err)
	}
}

func TestInMemoryStore_WithTxRollbackOnPanic(t *testing.T) {
	s := NewInMemoryStore()
	ctx := context.Background()

	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("expected panic to re-propagate")
		}
		// Nothing committed after a panic.
		if _, err := s.Collection(ctx, "c").Get(ctx, "a"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected rollback on panic, got %v", err)
		}
	}()

	_ = s.WithTx(ctx, func(txCtx context.Context) error {
		_ = s.Collection(txCtx, "c").Put(txCtx, doc("a", `{}`))
		panic("boom")
	})
}

func TestInMemoryStore_NilCallback(t *testing.T) {
	s := NewInMemoryStore()
	if err := s.WithTx(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil callback")
	}
}

// Ensure the in-memory store satisfies the Store interface.
var _ Store = (*InMemoryStore)(nil)
