package document

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
)

// txKey is the context key under which an in-flight in-memory transaction is
// carried by WithTx so that Collection binds writes to that transaction.
type txKey struct{}

// InMemoryStore is a dependency-free Store implementation backed by in-process
// maps. It is suitable for tests, examples, and local development. It is not a
// production datastore; swap in a real backend (e.g. Mongo) implementing Store.
//
// Transactions use copy-on-write staging: writes inside WithTx are buffered and
// applied atomically on commit, and discarded on error or panic.
type InMemoryStore struct {
	mu   sync.RWMutex
	data map[string]map[string][]byte // collection -> id -> payload
}

// NewInMemoryStore creates an empty in-memory document store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{data: make(map[string]map[string][]byte)}
}

// Collection returns a handle bound to the transaction in ctx, or to the store.
func (s *InMemoryStore) Collection(ctx context.Context, name string) Collection {
	if ctx != nil {
		if tx, ok := ctx.Value(txKey{}).(*memoryTx); ok {
			return &memoryCollection{store: s, tx: tx, name: name}
		}
	}
	return &memoryCollection{store: s, name: name}
}

// WithTx runs fn inside a staged transaction. Buffered writes are committed
// atomically on success, and discarded on error or panic (which re-propagates).
func (s *InMemoryStore) WithTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if s == nil {
		return errors.New("document store is not configured")
	}
	if fn == nil {
		return errors.New("nil transaction callback")
	}

	tx := &memoryTx{writes: make(map[string]map[string]*[]byte)}
	txCtx := context.WithValue(ctx, txKey{}, tx)

	committed := false
	defer func() {
		if rec := recover(); rec != nil {
			// Staged writes are simply dropped; nothing to roll back in-place.
			panic(rec)
		}
		_ = committed
	}()

	if err := fn(txCtx); err != nil {
		return err
	}

	s.commit(tx)
	committed = true
	return nil
}

// commit applies a transaction's staged writes to the backing store atomically.
func (s *InMemoryStore) commit(tx *memoryTx) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for collection, ids := range tx.writes {
		for id, payload := range ids {
			if payload == nil {
				// nil marks a delete.
				if col := s.data[collection]; col != nil {
					delete(col, id)
				}
				continue
			}
			col := s.data[collection]
			if col == nil {
				col = make(map[string][]byte)
				s.data[collection] = col
			}
			col[id] = *payload
		}
	}
}

// memoryTx buffers staged writes: collection -> id -> (*payload; nil = delete).
type memoryTx struct {
	writes map[string]map[string]*[]byte
}

func (t *memoryTx) stage(collection, id string, payload *[]byte) {
	col := t.writes[collection]
	if col == nil {
		col = make(map[string]*[]byte)
		t.writes[collection] = col
	}
	col[id] = payload
}

func (t *memoryTx) staged(collection, id string) (*[]byte, bool) {
	if col := t.writes[collection]; col != nil {
		p, ok := col[id]
		return p, ok
	}
	return nil, false
}

type memoryCollection struct {
	store *InMemoryStore
	tx    *memoryTx // nil outside a transaction
	name  string
}

func (c *memoryCollection) Get(_ context.Context, id string) (Document, error) {
	if c.tx != nil {
		if payload, ok := c.tx.staged(c.name, id); ok {
			if payload == nil {
				return Document{}, ErrNotFound
			}
			return Document{ID: id, Data: cloneBytes(*payload)}, nil
		}
	}

	c.store.mu.RLock()
	defer c.store.mu.RUnlock()
	col := c.store.data[c.name]
	payload, ok := col[id]
	if !ok {
		return Document{}, ErrNotFound
	}
	return Document{ID: id, Data: cloneBytes(payload)}, nil
}

func (c *memoryCollection) Put(_ context.Context, doc Document) error {
	if doc.ID == "" {
		return errors.New("document id is required")
	}
	payload := cloneBytes(doc.Data)
	if c.tx != nil {
		c.tx.stage(c.name, doc.ID, &payload)
		return nil
	}

	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	col := c.store.data[c.name]
	if col == nil {
		col = make(map[string][]byte)
		c.store.data[c.name] = col
	}
	col[doc.ID] = payload
	return nil
}

func (c *memoryCollection) Delete(_ context.Context, id string) error {
	if c.tx != nil {
		// Honor a not-found check against the committed store plus staged state.
		if _, err := c.getForDelete(id); err != nil {
			return err
		}
		c.tx.stage(c.name, id, nil)
		return nil
	}

	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	col := c.store.data[c.name]
	if _, ok := col[id]; !ok {
		return ErrNotFound
	}
	delete(col, id)
	return nil
}

// getForDelete checks existence within a transaction, considering staged writes.
func (c *memoryCollection) getForDelete(id string) (struct{}, error) {
	if payload, ok := c.tx.staged(c.name, id); ok {
		if payload == nil {
			return struct{}{}, ErrNotFound
		}
		return struct{}{}, nil
	}
	c.store.mu.RLock()
	defer c.store.mu.RUnlock()
	if _, ok := c.store.data[c.name][id]; !ok {
		return struct{}{}, ErrNotFound
	}
	return struct{}{}, nil
}

func (c *memoryCollection) Find(_ context.Context, filter Filter) ([]Document, error) {
	// Build the effective view: committed store overlaid with staged writes.
	view := make(map[string][]byte)

	c.store.mu.RLock()
	for id, payload := range c.store.data[c.name] {
		view[id] = payload
	}
	c.store.mu.RUnlock()

	if c.tx != nil {
		if staged := c.tx.writes[c.name]; staged != nil {
			for id, payload := range staged {
				if payload == nil {
					delete(view, id)
					continue
				}
				view[id] = *payload
			}
		}
	}

	ids := make([]string, 0, len(view))
	for id := range view {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]Document, 0, len(ids))
	for _, id := range ids {
		payload := view[id]
		if !matchesFilter(payload, filter) {
			continue
		}
		out = append(out, Document{ID: id, Data: cloneBytes(payload)})
	}
	return out, nil
}

// matchesFilter reports whether a JSON payload matches all filter conditions by
// exact top-level field equality. Non-JSON payloads match only the empty filter.
func matchesFilter(payload []byte, filter Filter) bool {
	if len(filter) == 0 {
		return true
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return false
	}
	for key, want := range filter {
		got, ok := decoded[key]
		if !ok {
			return false
		}
		if !jsonEqual(got, want) {
			return false
		}
	}
	return true
}

// jsonEqual compares two values as they would appear after JSON round-tripping,
// so callers can filter with native Go values (e.g. int, string) against
// json.Unmarshal's float64/string results.
func jsonEqual(got, want any) bool {
	wantJSON, err := json.Marshal(want)
	if err != nil {
		return false
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		return false
	}
	return string(gotJSON) == string(wantJSON)
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
