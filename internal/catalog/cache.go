// Package catalog caches product listings.
package catalog

import (
	"sync"
	"time"

	"github.com/ccchimi/storefront-api/internal/store"
)

// Cache keeps product listings keyed by search query for a short TTL.
//
// Every Invalidate bumps a generation counter. A reader grabs the generation
// before going to the database and passes it back to Put; if a checkout
// invalidated the cache in between, the now-stale result is thrown away
// instead of being cached for another TTL.
type Cache struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	gen     uint64
	entries map[string]entry
}

type entry struct {
	products []store.Product
	expires  time.Time
}

func NewCache(ttl time.Duration, now func() time.Time) *Cache {
	if now == nil {
		now = time.Now
	}
	return &Cache{ttl: ttl, now: now, entries: make(map[string]entry)}
}

// Get returns the cached listing for query, if any, along with the current
// generation to hand to Put after a miss.
func (c *Cache) Get(query string) (products []store.Product, gen uint64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, found := c.entries[query]
	if found && c.now().Before(e.expires) {
		return e.products, c.gen, true
	}
	if found {
		delete(c.entries, query)
	}
	return nil, c.gen, false
}

// Put stores a listing read at generation gen. It's a no-op if the cache was
// invalidated since then.
func (c *Cache) Put(query string, gen uint64, products []store.Product) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if gen != c.gen {
		return
	}
	c.entries[query] = entry{products: products, expires: c.now().Add(c.ttl)}
}

// Invalidate drops everything. Called whenever stock changes.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gen++
	clear(c.entries)
}
