package ct1

import (
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPSUConflictPreservesIndependentBoardReadings(t *testing.T) {
	now := time.Now().UTC()
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "mixed PSU claims", true: "duplicate PSU positions"}[duplicate], func(t *testing.T) {
			board := func(mac, psu string) Slot {
				return Slot{Occupied: true, Hashboard: &Board{MACAddress: mac, ModuleID: psu,
					MiningState: miningStateMining, Health: &Health{Timestamp: now, Hashrate: new(100.0)}}}
			}
			modules := []Module{{ID: 1, Slots: []Slot{board("a", "psu-a"), board("b", "psu-b")}}}
			if duplicate {
				modules = []Module{{ID: 1, Slots: []Slot{board("a", "psu-a")}}, {ID: 2, Slots: []Slot{board("b", "psu-a")}}}
			}
			got, err := snapshot(MapResponse{ContainerMap: &ContainerMap{Tanks: []Tank{{ID: 1, Modules: modules}}}}, now)
			require.NoError(t, err)
			var readings int
			for _, module := range got.Tanks[0].Modules {
				require.Empty(t, module.ReportedID)
				require.False(t, module.IdentityConfirmed)
				require.Equal(t, sdk.MinerStatusUnknown, module.State)
				for _, b := range module.Hashboards {
					if b.Occupancy == "occupied" {
						require.Empty(t, b.UnavailableReason)
						require.NotNil(t, b.HashrateHS)
						require.Equal(t, 100e12, *b.HashrateHS)
						readings++
					}
				}
			}
			require.Equal(t, 2, readings)
		})
	}
}
