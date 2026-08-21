package sqlstores_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

// TestSetRackBuildingPositionBulkPlace_RejectsLengthMismatch pins the
// store wrapper's defensive length-guard. The bulk-place SQL relies on
// the caller passing three parallel arrays (rack ids, aisles,
// positions) of equal length — if a caller ever drifts the lengths
// (e.g. by appending to rackIDs but skipping the position bookkeeping
// for one rack), the underlying UNNEST would pair the wrong rack with
// the wrong cell. Service-layer code already keeps the arrays in
// lockstep, but the store must refuse mismatched inputs at the
// boundary rather than passing them through to sqlc.
//
// This test runs without a database connection: the length check is
// the first statement in the wrapper, so the call returns before
// touching the connection pool. Constructing the store with a nil
// connection is therefore safe here.
func TestSetRackBuildingPositionBulkPlace_RejectsLengthMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	store := sqlstores.NewSQLBuildingStore(nil)

	t.Run("aisles shorter than rackIDs", func(t *testing.T) {
		err := store.SetRackBuildingPositionBulkPlace(
			context.Background(),
			int64(1),
			[]int64{10, 11, 12},
			[]int32{0, 1}, // len(aisles) = 2 != len(rackIDs) = 3
			[]int32{0, 1, 2},
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "array length mismatch")
	})

	t.Run("positions shorter than rackIDs", func(t *testing.T) {
		err := store.SetRackBuildingPositionBulkPlace(
			context.Background(),
			int64(1),
			[]int64{10, 11, 12},
			[]int32{0, 1, 2},
			[]int32{0, 1}, // len(positions) = 2 != len(rackIDs) = 3
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "array length mismatch")
	})

	t.Run("empty rackIDs short-circuits before length check", func(t *testing.T) {
		// Zero rackIDs returns nil before the length check fires,
		// matching the no-op contract every bulk wrapper observes.
		err := store.SetRackBuildingPositionBulkPlace(
			context.Background(),
			int64(1),
			nil,
			[]int32{0},
			[]int32{0},
		)
		require.NoError(t, err)
	})
}
