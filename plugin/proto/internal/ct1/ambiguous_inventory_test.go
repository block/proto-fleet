package ct1

import (
	"encoding/json"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestSnapshotRetainsUniqueBoardIdentitiesWithDuplicatePositions(t *testing.T) {
	var wire MapResponse
	require.NoError(t, json.Unmarshal([]byte(`{"containerMap":{"tanks":[{"id":3,"modules":[{"id":11,"slots":[{"occupied":true,"hashboard":{"macAddress":"02:00:00:00:00:01","serial":"board-1","moduleId":"module-11","location":{"hashboardPosition":"1"}}},{"occupied":true,"hashboard":{"macAddress":"02:00:00:00:00:02","serial":"board-2","moduleId":"module-11","location":{"hashboardPosition":"1"}}},{"occupied":true,"hashboard":{"macAddress":"02:00:00:00:00:03","serial":"board-3","moduleId":"module-11","location":{"hashboardPosition":"3"}}}]}]}]}}`), &wire))
	got, err := snapshot(wire, time.Now().UTC())
	require.NoError(t, err)
	require.False(t, got.TopologyComplete)
	identities := map[string]string{}
	retain := func(board sdk.ContainerHashboard) {
		if board.Occupancy == "occupied" && board.UnavailableReason == "" {
			_, duplicate := identities[board.Serial]
			require.False(t, duplicate, "each identity must occur once in the accepted inventory")
			identities[board.Serial] = board.MACAddress
		}
	}
	for _, tank := range got.Tanks {
		for _, module := range tank.Modules {
			for _, board := range module.Hashboards {
				retain(board)
			}
		}
	}
	for _, board := range got.UnlocatedHashboards {
		require.Zero(t, board.Position, "an uncertain position must not create a physical placement")
		retain(board)
	}
	require.Equal(t, map[string]string{"board-1": "02:00:00:00:00:01", "board-2": "02:00:00:00:00:02", "board-3": "02:00:00:00:00:03"}, identities)
}
