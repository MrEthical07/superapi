package tenant

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// clock is a settable time source shared by a cache under test.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Unix(1_000_000, 0)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// setClock points both segments of c at now.
func setClock(c *validationCache, now func() time.Time) {
	c.active.now = now
	c.inactive.now = now
}

func sizedCache(ttl time.Duration, maxActive, maxInactive int) (*validationCache, *clock) {
	c := newValidationCacheSized(ttl, maxActive, maxInactive)
	clk := newClock()
	setClock(c, clk.Now)
	return c, clk
}

func mustHit(t *testing.T, c *validationCache, key string, wantActive bool) {
	t.Helper()
	got, ok := c.get(key)
	if !ok {
		t.Fatalf("%q: expected a cache hit", key)
	}
	if got.Active != wantActive {
		t.Fatalf("%q: active=%v, want %v", key, got.Active, wantActive)
	}
	if got.ID != key {
		t.Fatalf("%q: cached id=%q, want the id stored with it", key, got.ID)
	}
}

func mustMiss(t *testing.T, c *validationCache, key string) {
	t.Helper()
	if got, ok := c.get(key); ok {
		t.Fatalf("%q: unexpected cache hit (%+v)", key, got)
	}
}

func TestCacheEvictsLeastRecentlyUsedFirst(t *testing.T) {
	c, _ := sizedCache(time.Minute, 3, 3)
	c.put("a", cachedTenant{ID: "a", Active: true})
	c.put("b", cachedTenant{ID: "b", Active: true})
	c.put("c", cachedTenant{ID: "c", Active: true})

	// Touch "a": "b" is now the least recently used.
	mustHit(t, c, "a", true)
	c.put("d", cachedTenant{ID: "d", Active: true})

	mustMiss(t, c, "b")
	mustHit(t, c, "a", true)
	mustHit(t, c, "c", true)
	mustHit(t, c, "d", true)

	// Order after those hits (oldest first): a, c, d. Adding "e" evicts "a".
	c.put("e", cachedTenant{ID: "e", Active: true})
	mustMiss(t, c, "a")
	for _, k := range []string{"c", "d", "e"} {
		mustHit(t, c, k, true)
	}
	if got := c.active.len(); got != 3 {
		t.Fatalf("size = %d, want it bounded at 3", got)
	}
}

// The old cache dropped every entry when it filled. Filling past capacity must
// evict only the oldest one.
func TestCacheEvictsOneEntryNotEverything(t *testing.T) {
	const capacity = 100
	c, _ := sizedCache(time.Minute, capacity, 10)
	for i := 0; i < capacity; i++ {
		c.put(fmt.Sprintf("t%03d", i), cachedTenant{ID: fmt.Sprintf("t%03d", i), Active: true})
	}
	c.put("overflow", cachedTenant{ID: "overflow", Active: true})

	if got := c.active.len(); got != capacity {
		t.Fatalf("size = %d, want %d", got, capacity)
	}
	mustMiss(t, c, "t000")
	for i := 1; i < capacity; i++ {
		mustHit(t, c, fmt.Sprintf("t%03d", i), true)
	}
	mustHit(t, c, "overflow", true)
}

func TestCacheReinsertRefreshesRecency(t *testing.T) {
	c, _ := sizedCache(time.Minute, 2, 2)
	c.put("a", cachedTenant{ID: "a", Active: true})
	c.put("b", cachedTenant{ID: "b", Active: true})
	c.put("a", cachedTenant{ID: "a", Active: true}) // moves "a" to the front without adding an entry
	c.put("c", cachedTenant{ID: "c", Active: true})

	mustMiss(t, c, "b")
	mustHit(t, c, "a", true)
	mustHit(t, c, "c", true)
}

func TestCacheEntriesExpireAfterTheirTTL(t *testing.T) {
	c, clk := sizedCache(40*time.Second, 10, 10)
	c.put("acme", cachedTenant{ID: "acme", Active: true})

	clk.Advance(39 * time.Second)
	mustHit(t, c, "acme", true)
	clk.Advance(2 * time.Second)
	mustMiss(t, c, "acme")
	if got := c.active.len(); got != 0 {
		t.Fatalf("an expired entry must be removed on lookup, size = %d", got)
	}
}

// A hit refreshes recency but not the deadline, so a busy tenant that was
// deactivated is still re-checked within one TTL.
func TestCacheHitsDoNotExtendTheDeadline(t *testing.T) {
	c, clk := sizedCache(20*time.Second, 10, 10)
	c.put("busy", cachedTenant{ID: "busy", Active: true})
	for i := 0; i < 5; i++ {
		clk.Advance(3 * time.Second)
		mustHit(t, c, "busy", true)
	}
	clk.Advance(6 * time.Second) // 21s after it was stored
	mustMiss(t, c, "busy")
}

func TestCachePutResetsTheDeadline(t *testing.T) {
	c, clk := sizedCache(20*time.Second, 10, 10)
	c.put("acme", cachedTenant{ID: "acme", Active: true})
	clk.Advance(15 * time.Second)
	c.put("acme", cachedTenant{ID: "acme", Active: true})
	clk.Advance(15 * time.Second)
	mustHit(t, c, "acme", true)
}

func TestNegativeResultsExpireSoonerThanPositiveOnes(t *testing.T) {
	if got := negativeTTL(40 * time.Second); got != 10*time.Second {
		t.Fatalf("negativeTTL(40s) = %v, want 10s", got)
	}
	c, clk := sizedCache(40*time.Second, 10, 10)
	c.put("real", cachedTenant{ID: "real", Active: true})
	c.put("ghost", cachedTenant{ID: "ghost"})

	clk.Advance(9 * time.Second)
	mustHit(t, c, "real", true)
	mustHit(t, c, "ghost", false)

	clk.Advance(2 * time.Second) // 11s: past the 10s negative TTL
	mustMiss(t, c, "ghost")
	mustHit(t, c, "real", true)

	clk.Advance(30 * time.Second) // 41s: past the positive TTL too
	mustMiss(t, c, "real")
}

func TestNegativeTTLBounds(t *testing.T) {
	tests := []struct{ positive, want time.Duration }{
		{4 * time.Second, time.Second},
		{2 * time.Second, time.Second},
		{500 * time.Millisecond, 500 * time.Millisecond}, // never longer than the positive TTL
		{time.Hour, 15 * time.Minute},
		{30 * time.Second, 7500 * time.Millisecond},
	}
	for _, tc := range tests {
		if got := negativeTTL(tc.positive); got != tc.want {
			t.Errorf("negativeTTL(%v) = %v, want %v", tc.positive, got, tc.want)
		}
	}
}

// A flood of made-up tenant ids only churns the negative segment: every real
// tenant stays cached, so the database is not hit for them.
func TestFloodOfUnknownTenantsCannotEvictRealOnes(t *testing.T) {
	c, _ := sizedCache(time.Minute, 50, 20)
	real := make([]string, 50)
	for i := range real {
		real[i] = fmt.Sprintf("real-%02d", i)
		c.put(real[i], cachedTenant{ID: real[i], Active: true})
	}

	for i := 0; i < 50_000; i++ {
		c.put(fmt.Sprintf("ghost-%d", i), cachedTenant{ID: fmt.Sprintf("ghost-%d", i)})
	}

	for _, id := range real {
		mustHit(t, c, id, true)
	}
	if got := c.inactive.len(); got != 20 {
		t.Fatalf("negative segment size = %d, want it bounded at 20", got)
	}
}

func TestCacheMovesATenantBetweenSegmentsWhenItsStateChanges(t *testing.T) {
	c, _ := sizedCache(time.Minute, 10, 10)
	c.put("acme", cachedTenant{ID: "acme"})
	mustHit(t, c, "acme", false)

	c.put("acme", cachedTenant{ID: "acme", Active: true}) // tenant was created or re-activated
	mustHit(t, c, "acme", true)
	if c.inactive.len() != 0 {
		t.Fatal("a stale negative entry was left behind")
	}

	c.put("acme", cachedTenant{ID: "acme"}) // and deactivated again
	mustHit(t, c, "acme", false)
	if c.active.len() != 0 {
		t.Fatal("a stale positive entry was left behind")
	}
}

func TestNilCacheIsANoOp(t *testing.T) {
	var c *validationCache
	c.put("acme", cachedTenant{ID: "acme", Active: true})
	mustMiss(t, c, "acme")
}

func TestCacheCapacityFloor(t *testing.T) {
	c, _ := sizedCache(time.Minute, 0, -5)
	c.put("a", cachedTenant{ID: "a", Active: true})
	c.put("b", cachedTenant{ID: "b", Active: true})
	mustHit(t, c, "b", true)
	mustMiss(t, c, "a")
}

// Many goroutines mixing hits, misses, inserts, state flips and expiry. Run
// with -race; the size invariants must hold throughout.
func TestCacheIsSafeUnderConcurrency(t *testing.T) {
	c := newValidationCacheSized(50*time.Millisecond, 64, 16)

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 5_000; i++ {
				key := fmt.Sprintf("t-%d", (g*7+i)%200)
				switch i % 4 {
				case 0:
					c.put(key, cachedTenant{ID: key, Active: true})
				case 1:
					c.put(key, cachedTenant{ID: key})
				default:
					c.get(key)
				}
				if i%997 == 0 {
					time.Sleep(time.Millisecond)
				}
			}
		}(g)
	}
	wg.Wait()

	if a, n := c.active.len(), c.inactive.len(); a > 64 || n > 16 {
		t.Fatalf("bounds exceeded: active=%d (max 64), inactive=%d (max 16)", a, n)
	}
	// The map and the list must agree.
	for name, l := range map[string]*ttlLRU{"active": c.active, "inactive": c.inactive} {
		if len(l.items) != l.order.Len() {
			t.Fatalf("%s: map has %d entries, list has %d", name, len(l.items), l.order.Len())
		}
	}
}

// countingDirectory counts lookups per tenant id.
type countingDirectory struct {
	inner Directory
	mu    sync.Mutex
	byID  map[string]int
}

func (d *countingDirectory) Get(ctx context.Context, id string) (Record, error) {
	d.mu.Lock()
	d.byID[id]++
	d.mu.Unlock()
	return d.inner.Get(ctx, id)
}

func (d *countingDirectory) GetBySlug(ctx context.Context, slug string) (Record, error) {
	d.mu.Lock()
	d.byID["slug:"+slug]++
	d.mu.Unlock()
	return d.inner.GetBySlug(ctx, slug)
}

func (d *countingDirectory) count(id string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.byID[id]
}

// Through the lookup path: a flood of unknown tenants does not push a real
// tenant out of the cache, so the directory is asked about it once.
func TestFloodDoesNotForceLookupsForRealTenants(t *testing.T) {
	dir := &countingDirectory{inner: newDirectory(), byID: map[string]int{}}
	cache := newValidationCacheSized(time.Minute, 100, 10)

	for i := 0; i < 5; i++ {
		id, active, err := lookupTenant(t.Context(), dir, cache, ResolverHeader, "acme")
		if err != nil || !active || id != "acme" {
			t.Fatalf("acme: id=%q active=%v err=%v", id, active, err)
		}
		for j := 0; j < 200; j++ {
			if _, active, err := lookupTenant(t.Context(), dir, cache, ResolverHeader, fmt.Sprintf("ghost-%d-%d", i, j)); err != nil || active {
				t.Fatalf("ghost: active=%v err=%v", active, err)
			}
		}
	}

	acmeLookups := dir.count("acme")
	if acmeLookups != 1 {
		t.Fatalf("the real tenant was looked up %d times, want 1", acmeLookups)
	}
}
