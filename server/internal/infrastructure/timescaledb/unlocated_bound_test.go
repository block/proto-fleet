package timescaledb

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

// Repeated partial polls that each introduce fresh identities must not grow the
// retained placement-free inventory without bound.
func TestBoundedUnlocatedInventoryCapsAccumulation(t *testing.T) {
	base := time.Now().UTC()
	previous := make([]sdk.ContainerHashboard, 0, maxRetainedUnlocatedHashboards*2)
	for i := 0; i < maxRetainedUnlocatedHashboards*2; i++ {
		previous = append(previous, sdk.ContainerHashboard{
			Serial: fmt.Sprintf("retained-%d", i), ObservedAt: base.Add(time.Duration(i) * time.Second),
		})
	}
	current := []sdk.ContainerHashboard{{Serial: "fresh", ObservedAt: base.Add(time.Hour)}}

	got := boundedUnlocatedInventory(current, previous, map[string]bool{"serial:fresh": true})
	require.Len(t, got, maxRetainedUnlocatedHashboards)
	require.Equal(t, "fresh", got[0].Serial, "the fresh poll is never evicted")
	// The most-recently observed retained identity survives; the stalest is dropped.
	require.Equal(t, "retained-2047", got[1].Serial, "the freshest retained identity survives")
	require.Equal(t, "retained-1025", got[len(got)-1].Serial, "the stalest kept identity sits at the cap boundary")
}

// A board confirmed in a physical slot, and boards re-observed this poll, drop out
// of the retained set instead of duplicating.
func TestBoundedUnlocatedInventoryDropsAccounted(t *testing.T) {
	base := time.Now().UTC()
	current := []sdk.ContainerHashboard{{Serial: "still-unlocated", ObservedAt: base}}
	previous := []sdk.ContainerHashboard{
		{Serial: "still-unlocated", ObservedAt: base.Add(-time.Minute)},
		{Serial: "now-placed", ObservedAt: base.Add(-time.Minute)},
		{Serial: "kept", ObservedAt: base.Add(-time.Minute)},
		{ObservedAt: base.Add(-time.Minute)}, // no identity: cannot dedupe, dropped
	}
	accounted := map[string]bool{"serial:still-unlocated": true, "serial:now-placed": true}

	got := boundedUnlocatedInventory(current, previous, accounted)
	require.Len(t, got, 2)
	require.Equal(t, "still-unlocated", got[0].Serial)
	require.Equal(t, "kept", got[1].Serial)
}
