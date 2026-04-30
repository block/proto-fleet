package fleetoptions_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/block/proto-fleet/server/internal/domain/fleetoptions"
)

func TestCache_GetAfterPutReturnsHit(t *testing.T) {
	c := fleetoptions.NewCache(time.Minute, 16)
	want := fleetoptions.Options{
		Models:           []string{"S19 Pro"},
		FirmwareVersions: []string{"1.0.0"},
	}

	c.Put(42, want)
	got, ok := c.Get(42)

	assert.True(t, ok)
	assert.Equal(t, want, got)
}

func TestCache_GetWithoutPutReturnsMiss(t *testing.T) {
	c := fleetoptions.NewCache(time.Minute, 16)

	_, ok := c.Get(7)

	assert.False(t, ok)
}

func TestCache_InvalidateRemovesEntry(t *testing.T) {
	c := fleetoptions.NewCache(time.Minute, 16)
	c.Put(1, fleetoptions.Options{Models: []string{"x"}})

	c.Invalidate(1)
	_, ok := c.Get(1)

	assert.False(t, ok)
}

func TestCache_InvalidateMissingOrgIsNoop(t *testing.T) {
	c := fleetoptions.NewCache(time.Minute, 16)

	assert.NotPanics(t, func() { c.Invalidate(999) })
}

func TestCache_TTLExpiryEvictsEntry(t *testing.T) {
	c := fleetoptions.NewCache(20*time.Millisecond, 16)
	c.Put(1, fleetoptions.Options{Models: []string{"x"}})

	time.Sleep(40 * time.Millisecond)
	_, ok := c.Get(1)

	assert.False(t, ok)
}

func TestCache_DifferentOrgsHaveIndependentEntries(t *testing.T) {
	c := fleetoptions.NewCache(time.Minute, 16)
	a := fleetoptions.Options{Models: []string{"a"}}
	b := fleetoptions.Options{Models: []string{"b"}}

	c.Put(1, a)
	c.Put(2, b)

	got1, ok1 := c.Get(1)
	got2, ok2 := c.Get(2)
	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, a, got1)
	assert.Equal(t, b, got2)

	c.Invalidate(1)
	_, ok1 = c.Get(1)
	got2, ok2 = c.Get(2)
	assert.False(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, b, got2)
}

func TestCache_NilReceiverIsSafe(t *testing.T) {
	var c *fleetoptions.Cache

	assert.NotPanics(t, func() {
		c.Put(1, fleetoptions.Options{})
		c.Invalidate(1)
		_, ok := c.Get(1)
		assert.False(t, ok)
		_ = c.Generation(1)
		ok = c.PutIfGeneration(1, fleetoptions.Options{}, 0)
		assert.False(t, ok)
	})
}

func TestCache_GenerationBumpsOnInvalidate(t *testing.T) {
	c := fleetoptions.NewCache(time.Minute, 16)

	assert.Equal(t, uint64(0), c.Generation(1))
	c.Invalidate(1)
	assert.Equal(t, uint64(1), c.Generation(1))
	c.Invalidate(1)
	assert.Equal(t, uint64(2), c.Generation(1))
	// Different orgs are independent.
	assert.Equal(t, uint64(0), c.Generation(2))
}

func TestCache_PutIfGenerationDiscardsStaleAfterInvalidate(t *testing.T) {
	c := fleetoptions.NewCache(time.Minute, 16)
	want := fleetoptions.Options{Models: []string{"S19"}}

	gen := c.Generation(1)
	c.Invalidate(1) // races ahead of the fetch

	ok := c.PutIfGeneration(1, want, gen)
	assert.False(t, ok, "put with stale generation must be a no-op")

	_, hit := c.Get(1)
	assert.False(t, hit, "stale put must not have written to the cache")
}

func TestCache_PutIfGenerationWritesWhenGenerationMatches(t *testing.T) {
	c := fleetoptions.NewCache(time.Minute, 16)
	want := fleetoptions.Options{Models: []string{"S19"}}

	gen := c.Generation(1)
	ok := c.PutIfGeneration(1, want, gen)
	assert.True(t, ok)

	got, hit := c.Get(1)
	assert.True(t, hit)
	assert.Equal(t, want, got)
}
