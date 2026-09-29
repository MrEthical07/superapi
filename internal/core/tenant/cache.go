package tenant

import (
	"container/list"
	"sync"
	"time"
)

// Bounds for the in-process tenant validation cache.
const (
	// maxCacheEntries bounds cached results for tenants that exist and are
	// active.
	maxCacheEntries = 10_000
	// maxNegativeCacheEntries bounds cached "unknown or inactive" results. It is
	// kept separate and small so a flood of made-up tenant ids can only churn
	// this segment: it can never push real tenants out of the cache.
	maxNegativeCacheEntries = 1_000
	// minNegativeTTL is the floor for the negative-result lifetime.
	minNegativeTTL = time.Second
)

// negativeTTL is how long an "unknown or inactive" result is cached: a quarter
// of the positive TTL (at least a second, never longer than the positive TTL).
// It is shorter because those results are cheap to recompute, may have been
// caused by a tenant that was just created or re-activated, and are the ones an
// attacker can generate at will.
func negativeTTL(positive time.Duration) time.Duration {
	ttl := positive / 4
	if ttl < minNegativeTTL {
		ttl = minNegativeTTL
	}
	if ttl > positive {
		ttl = positive
	}
	return ttl
}

// cachedTenant is a remembered validation result: the tenant's id (the
// subdomain resolver looks tenants up by slug but must attach the id) and
// whether it may serve requests.
type cachedTenant struct {
	ID     string
	Active bool
}

// validationCache remembers tenant validation results so the database is not
// asked on every request. Keys name the resolver and the value it produced
// (see cacheKey), so an id and a slug that happen to be spelled the same never
// share an entry. It has two bounded LRU segments with per-entry TTL: one for
// active tenants and one, smaller and shorter-lived, for unknown or inactive
// ones.
type validationCache struct {
	active   *ttlLRU
	inactive *ttlLRU
}

func newValidationCache(ttl time.Duration) *validationCache {
	return newValidationCacheSized(ttl, maxCacheEntries, maxNegativeCacheEntries)
}

func newValidationCacheSized(ttl time.Duration, maxActive, maxInactive int) *validationCache {
	return &validationCache{
		active:   newTTLLRU(maxActive, ttl),
		inactive: newTTLLRU(maxInactive, negativeTTL(ttl)),
	}
}

// cacheKey builds the cache key for a value produced by a resolver ("header",
// "subdomain"). Keying by resolver as well as value keeps a header id "acme"
// and a subdomain slug "acme" apart: they name different columns and may point
// at different tenants.
func cacheKey(resolver, value string) string { return resolver + ":" + value }

// get returns the cached result for key and whether there was a live entry.
func (c *validationCache) get(key string) (cachedTenant, bool) {
	if c == nil {
		return cachedTenant{}, false
	}
	if id, ok := c.active.get(key); ok {
		return cachedTenant{ID: id, Active: true}, true
	}
	if id, ok := c.inactive.get(key); ok {
		return cachedTenant{ID: id, Active: false}, true
	}
	return cachedTenant{}, false
}

// put records a validation result. The other segment's entry for key, if any,
// is dropped so a tenant that changed state is never reported from both.
func (c *validationCache) put(key string, v cachedTenant) {
	if c == nil {
		return
	}
	if v.Active {
		c.inactive.remove(key)
		c.active.put(key, v.ID)
		return
	}
	c.active.remove(key)
	c.inactive.put(key, v.ID)
}

// ttlLRU is a fixed-capacity set of keys with a time-to-live per entry. When
// full, the least recently used entry is evicted. A hit refreshes recency but
// not the deadline: an entry lives at most one TTL after it was stored, so a
// tenant deactivated in the database stops being served within a TTL however
// busy it is.
type ttlLRU struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration
	order *list.List // front = most recently used
	items map[string]*list.Element
	now   func() time.Time
}

type lruEntry struct {
	key     string
	value   string
	expires time.Time
}

func newTTLLRU(max int, ttl time.Duration) *ttlLRU {
	if max < 1 {
		max = 1
	}
	return &ttlLRU{
		max:   max,
		ttl:   ttl,
		order: list.New(),
		items: make(map[string]*list.Element, max),
		now:   time.Now,
	}
}

// get returns the value stored for key if it has a live entry, marking it most
// recently used. An expired entry is removed.
func (l *ttlLRU) get(key string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	el, ok := l.items[key]
	if !ok {
		return "", false
	}
	entry := el.Value.(*lruEntry)
	if l.now().After(entry.expires) {
		l.removeElement(el)
		return "", false
	}
	l.order.MoveToFront(el)
	return entry.value, true
}

// put stores key and value with a fresh deadline and evicts the least recently
// used entry if the segment is over capacity.
func (l *ttlLRU) put(key, value string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	expires := l.now().Add(l.ttl)
	if el, ok := l.items[key]; ok {
		entry := el.Value.(*lruEntry)
		entry.value, entry.expires = value, expires
		l.order.MoveToFront(el)
		return
	}
	l.items[key] = l.order.PushFront(&lruEntry{key: key, value: value, expires: expires})
	for l.order.Len() > l.max {
		l.removeElement(l.order.Back())
	}
}

func (l *ttlLRU) remove(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if el, ok := l.items[key]; ok {
		l.removeElement(el)
	}
}

func (l *ttlLRU) removeElement(el *list.Element) {
	l.order.Remove(el)
	delete(l.items, el.Value.(*lruEntry).key)
}

func (l *ttlLRU) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.order.Len()
}
