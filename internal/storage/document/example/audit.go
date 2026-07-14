// Package example shows how a module wires and uses the optional document
// store. It is illustrative and is NOT registered in the runtime module
// registry (internal/modules/modules.go) — it exists to demonstrate the pattern
// and to keep it compiling. Copy it into a real module when you need document
// persistence; delete it (and the document package) if you do not.
//
// The pattern mirrors the relational layer:
//
//	handler -> service -> repository -> document.Store (Collection / WithTx)
//
// The repository depends on document.Store, maps domain <-> storage models, and
// keeps document types out of its public contract. There is no "if sql else
// mongo" branching: a module that needs documents wires a document.Store in its
// own BindDependencies (see the document package doc) and hands it here.
package example

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MrEthical07/superapi/internal/storage/document"
)

const auditCollection = "audit_events"

// AuditEvent is the domain model for a recorded action.
type AuditEvent struct {
	ID     string    `json:"id"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	At     time.Time `json:"at"`
}

// AuditRepository persists audit events in the document store. It owns
// marshaling between the domain model and the opaque document payload.
type AuditRepository struct {
	docs document.Store
}

// NewAuditRepository constructs the repository over a document store.
func NewAuditRepository(docs document.Store) *AuditRepository {
	return &AuditRepository{docs: docs}
}

// Save writes an event, overwriting any existing event with the same id
// (Replace = upsert). Reads/writes go through Collection(ctx); when called
// inside a service-owned transaction the write joins that transaction.
func (r *AuditRepository) Save(ctx context.Context, event AuditEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal audit event: %w", err)
	}
	return r.docs.Collection(ctx, auditCollection).Replace(ctx, document.Document{
		ID:   event.ID,
		Data: payload,
	})
}

// Get returns a single event by id.
func (r *AuditRepository) Get(ctx context.Context, id string) (AuditEvent, error) {
	doc, err := r.docs.Collection(ctx, auditCollection).Get(ctx, id)
	if err != nil {
		return AuditEvent{}, err
	}
	return decodeAuditEvent(doc)
}

// ListByActor returns all events recorded for an actor.
func (r *AuditRepository) ListByActor(ctx context.Context, actor string) ([]AuditEvent, error) {
	docs, err := r.docs.Collection(ctx, auditCollection).Find(ctx, document.ByFields(map[string]any{"actor": actor}))
	if err != nil {
		return nil, err
	}
	events := make([]AuditEvent, 0, len(docs))
	for _, d := range docs {
		event, err := decodeAuditEvent(d)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func decodeAuditEvent(doc document.Document) (AuditEvent, error) {
	var event AuditEvent
	if err := json.Unmarshal(doc.Data, &event); err != nil {
		return AuditEvent{}, fmt.Errorf("decode audit event: %w", err)
	}
	return event, nil
}

// AuditService is the workflow layer. It owns the write transaction boundary via
// the document store's WithTx, exactly as a relational service owns
// storage.Postgres.WithTx.
type AuditService struct {
	docs document.Store
	repo *AuditRepository
}

// NewAuditService constructs the service and its repository from a store.
func NewAuditService(docs document.Store) *AuditService {
	return &AuditService{docs: docs, repo: NewAuditRepository(docs)}
}

// RecordBatch writes several events inside one unit of work. On a transactional
// backend the batch is atomic (all-or-nothing); on a non-transactional backend
// document.WithTx runs the writes directly. Using the free document.WithTx
// helper keeps this service portable across backends — swap the store in
// BindDependencies and this code is unchanged.
func (s *AuditService) RecordBatch(ctx context.Context, events []AuditEvent) error {
	return document.WithTx(ctx, s.docs, func(txCtx context.Context) error {
		for _, event := range events {
			if err := s.repo.Save(txCtx, event); err != nil {
				return err
			}
		}
		return nil
	})
}

// Get exposes a single-event read through the service.
func (s *AuditService) Get(ctx context.Context, id string) (AuditEvent, error) {
	return s.repo.Get(ctx, id)
}

// ListByActor exposes an actor-scoped read through the service.
func (s *AuditService) ListByActor(ctx context.Context, actor string) ([]AuditEvent, error) {
	return s.repo.ListByActor(ctx, actor)
}
