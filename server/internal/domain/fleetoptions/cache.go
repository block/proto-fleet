// Package fleetoptions provides a per-organization cache of the option
// arrays surfaced by ListMinerStateSnapshots (available models and
// firmware versions). The cache is shared across services: fleetmanagement
// reads it to populate list responses, while pairing and fleetmanagement
// invalidate it after obvious membership changes.
package fleetoptions

import (
	"time"

	lru "github.com/hashicorp/golang-lru/v2/expirable"
)

// DefaultTTL bounds how long model / firmware dropdown values can remain
// stale when a mutation relies on time-based expiry instead of explicit
// invalidation.
const DefaultTTL = 60 * time.Second

// Options is the cached payload for one organization.
type Options struct {
	Models           []string
	FirmwareVersions []string
}

// Cache is a goroutine-safe per-org cache with TTL eviction.
type Cache struct {
	lru *lru.LRU[int64, Options]
}

// NewCache builds a Cache with the given TTL and maximum entry count.
func NewCache(ttl time.Duration, size int) *Cache {
	return &Cache{
		lru: lru.NewLRU[int64, Options](size, nil, ttl),
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

// Put stores opts under orgID, replacing any prior entry.
func (c *Cache) Put(orgID int64, opts Options) {
	if c == nil {
		return
	}
	c.lru.Add(orgID, opts)
}

// Invalidate removes any cached entry for orgID. Safe to call when no
// entry exists.
func (c *Cache) Invalidate(orgID int64) {
	if c == nil {
		return
	}
	c.lru.Remove(orgID)
}
