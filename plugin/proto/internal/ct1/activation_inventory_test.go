package ct1

import (
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestActivationInventoryIgnoresPlacement(t *testing.T) {
	at := time.Now()
	wire := activationInventoryMap(at)
	// Distinct boards can report the same physical position.
	module := wire.ContainerMap.Tanks[0].Modules[0]
	module.Slots[1].Hashboard.Location.HashboardPosition = new(uint32(1))
	unlocated := module.Slots[2]
	module.Slots = module.Slots[:2]
	wire.ContainerMap.Tanks = append(wire.ContainerMap.Tanks, &mdk.Tank{Id: 0, Modules: []*mdk.Module{{Id: 0, Slots: []*mdk.Slot{unlocated}}}})
	got, err := activationInventory(wire, at)
	require.NoError(t, err)
	require.Len(t, got.Boards, 288)
	require.Equal(t, at, got.ObservedAt)
	topology, err := snapshot(mapResponse(wire), at)
	require.NoError(t, err)
	require.Len(t, topology.UnlocatedHashboards, 3)
	for _, board := range topology.UnlocatedHashboards {
		require.Equal(t, occupiedHashboard, board.Occupancy)
		require.Zero(t, board.Position)
		require.NotEmpty(t, board.MACAddress)
	}
	for _, board := range topology.Tanks[0].Modules[0].Hashboards {
		require.Equal(t, "unknown", board.Occupancy, "conflicting positions must not create installations")
	}
	request := sdk.MiningGroupRequest{Action: "activation_inventory"}
	require.NoError(t, request.Validate())
	require.True(t, request.ReadOnly())
}

func TestActivationInventoryRejectsUnidentifiedBoards(t *testing.T) {
	for _, kind := range []string{"missing board", "missing mac", "duplicate mac", "duplicate serial", "contradictory occupancy"} {
		t.Run(kind, func(t *testing.T) {
			wire := activationInventoryMap(time.Now())
			slots := wire.ContainerMap.Tanks[0].Modules[0].Slots
			switch kind {
			case "missing board":
				slots[0].Hashboard = nil
			case "missing mac":
				slots[0].Hashboard.MacAddress = ""
			case "duplicate mac":
				slots[0].Hashboard.MacAddress = slots[1].Hashboard.MacAddress
			case "duplicate serial":
				slots[0].Hashboard.Serial = slots[1].Hashboard.Serial
			case "contradictory occupancy":
				slots[0].Occupied = false
			}
			_, err := activationInventory(wire, time.Now())
			require.Error(t, err)
		})
	}
}

func activationInventoryMap(at time.Time) *mdk.GetContainerMapResponse {
	wire := firmwareContainerMap(at)
	for _, tank := range wire.ContainerMap.Tanks {
		for _, module := range tank.Modules {
			for _, slot := range module.Slots {
				slot.Hashboard.Serial = "serial-" + slot.Hashboard.MacAddress
			}
		}
	}
	return wire
}
