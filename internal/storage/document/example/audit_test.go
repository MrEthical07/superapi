package example

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MrEthical07/superapi/internal/storage/document"
)

func TestAuditService_RecordBatchAndRead(t *testing.T) {
	store := document.NewInMemoryStore()
	svc := NewAuditService(store)
	ctx := context.Background()

	events := []AuditEvent{
		{ID: "1", Actor: "alice", Action: "login", At: time.Unix(1, 0)},
		{ID: "2", Actor: "bob", Action: "login", At: time.Unix(2, 0)},
		{ID: "3", Actor: "alice", Action: "logout", At: time.Unix(3, 0)},
	}
	if err := svc.RecordBatch(ctx, events); err != nil {
		t.Fatalf("record batch: %v", err)
	}

	got, err := svc.Get(ctx, "2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Actor != "bob" || got.Action != "login" {
		t.Fatalf("unexpected event: %+v", got)
	}

	alice, err := svc.ListByActor(ctx, "alice")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(alice) != 2 {
		t.Fatalf("expected 2 alice events, got %d", len(alice))
	}
}

func TestAuditService_RecordBatchRollback(t *testing.T) {
	store := document.NewInMemoryStore()
	svc := NewAuditService(store)
	ctx := context.Background()

	// Second event has an empty id, which the store rejects — the whole batch
	// must roll back.
	err := svc.RecordBatch(ctx, []AuditEvent{
		{ID: "1", Actor: "alice", Action: "login"},
		{ID: "", Actor: "alice", Action: "broken"},
	})
	if err == nil {
		t.Fatal("expected batch error")
	}

	if _, err := svc.Get(ctx, "1"); !errors.Is(err, document.ErrNotFound) {
		t.Fatalf("expected first event rolled back, got %v", err)
	}
}
