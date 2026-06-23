package fleetmanagement

import (
	"testing"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStateCountsFilter covers how GetMinerStateCounts maps the request
// site scope to the store MinerFilter. The all-sites case must return a
// nil filter so the counts and total stay org-wide; the three scoped
// combos mirror MinerListFilter semantics (site_ids OR, include_unassigned
// independent).
func TestStateCountsFilter(t *testing.T) {
	t.Run("all sites returns nil filter", func(t *testing.T) {
		filter, err := stateCountsFilter(&pb.GetMinerStateCountsRequest{})
		require.NoError(t, err)
		assert.Nil(t, filter)
	})

	t.Run("specific sites", func(t *testing.T) {
		filter, err := stateCountsFilter(&pb.GetMinerStateCountsRequest{SiteIds: []int64{7, 9}})
		require.NoError(t, err)
		require.NotNil(t, filter)
		assert.Equal(t, []int64{7, 9}, filter.SiteIDs)
		assert.False(t, filter.IncludeUnassigned)
	})

	t.Run("unassigned only", func(t *testing.T) {
		filter, err := stateCountsFilter(&pb.GetMinerStateCountsRequest{IncludeUnassigned: true})
		require.NoError(t, err)
		require.NotNil(t, filter)
		assert.Empty(t, filter.SiteIDs)
		assert.True(t, filter.IncludeUnassigned)
	})

	t.Run("specific sites plus unassigned", func(t *testing.T) {
		filter, err := stateCountsFilter(&pb.GetMinerStateCountsRequest{
			SiteIds:           []int64{7},
			IncludeUnassigned: true,
		})
		require.NoError(t, err)
		require.NotNil(t, filter)
		assert.Equal(t, []int64{7}, filter.SiteIDs)
		assert.True(t, filter.IncludeUnassigned)
	})

	t.Run("rejects non-positive site id", func(t *testing.T) {
		_, err := stateCountsFilter(&pb.GetMinerStateCountsRequest{SiteIds: []int64{0}})
		require.Error(t, err)
	})

	t.Run("rejects too many site ids", func(t *testing.T) {
		ids := make([]int64, maxFreeFormFilterValues+1)
		for i := range ids {
			ids[i] = int64(i + 1)
		}
		_, err := stateCountsFilter(&pb.GetMinerStateCountsRequest{SiteIds: ids})
		require.Error(t, err)
	})
}
