// Package fleetoptions provides a per-organization cache of the option
// arrays surfaced by ListMinerStateSnapshots (available models and
// firmware versions). The cache is shared across services: fleetmanagement
// reads it to populate list responses, while pairing and telemetry
// invalidate it when their writes change the underlying data.
package fleetoptions

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2/expirable"
)

// Options is the cached payload for one organization.
type Options struct {
	Models           []string
	FirmwareVersions []string
}

// Cache is a goroutine-safe per-org cache with TTL eviction. The TTL acts
// as a safety net; freshness on the hot path comes from explicit
// Invalidate calls at known mutation sites.
//
// Each org tracks an invalidation epoch ("generation") so that a cold-
// cache fetch racing with a concurrent Invalidate cannot reinsert a
// pre-mutation result. Hydrators capture Generation before reading the
// store and use PutIfGeneration on completion; if Invalidate ran in
// between, the put is skipped and the next request re-fetches.
type Cache struct {
	lru *lru.LRU[int64, Options]

	// gensMu guards gens. A separate map (rather than embedding the
	// generation in the cache value) is required because Invalidate must
	// bump the epoch even when no entry exists yet — an in-flight cold
	// fetch may still be about to write.
	gensMu sync.Mutex
	gens   map[int64]uint64
}

// NewCache builds a Cache with the given TTL and maximum entry count.
func NewCache(ttl time.Duration, size int) *Cache {
	return &Cache{
		lru:  lru.NewLRU[int64, Options](size, nil, ttl),
		gens: make(map[int64]uint64),
	}
}

// Get returns the cached options for orgID and whether they were present
// (and unexpired).
func (c *Cache) Get(orgID int64) (Options, bool) {
	if c == nil {
		return Options{}, false
	}
	return c.lru.Get(orgID)
}

// Put stores opts under orgID unconditionally, replacing any prior
// entry. Use only in contexts where no concurrent Invalidate is
// possible (test setup, eager warmup). The production hydration path
// must use Generation + PutIfGeneration to avoid caching results that
// were invalidated mid-fetch.
func (c *Cache) Put(orgID int64, opts Options) {
	if c == nil {
		return
	}
	c.lru.Add(orgID, opts)
}

// Generation returns the current invalidation epoch for orgID.
// Callers about to perform an asynchronous fetch should record this
// value before reading the underlying store, then pass it to
// PutIfGeneration on completion.
func (c *Cache) Generation(orgID int64) uint64 {
	if c == nil {
		return 0
	}
	c.gensMu.Lock()
	defer c.gensMu.Unlock()
	return c.gens[orgID]
}

// PutIfGeneration writes opts under orgID only when the recorded
// epoch for orgID still matches expected. Returns true on write,
// false when an Invalidate raced ahead — callers should treat false
// as "result discarded; next request re-fetches".
func (c *Cache) PutIfGeneration(orgID int64, opts Options, expected uint64) bool {
	if c == nil {
		return false
	}
	c.gensMu.Lock()
	defer c.gensMu.Unlock()
	if c.gens[orgID] != expected {
		return false
	}
	c.lru.Add(orgID, opts)
	return true
}

// Invalidate removes any cached entry for orgID and bumps the org's
// generation so any concurrently-running fetch's PutIfGeneration will
// be discarded. Safe to call when no entry exists.
func (c *Cache) Invalidate(orgID int64) {
	if c == nil {
		return
	}
	c.gensMu.Lock()
	c.gens[orgID]++
	c.gensMu.Unlock()
	c.lru.Remove(orgID)
}
