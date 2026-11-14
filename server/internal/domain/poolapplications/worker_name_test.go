package poolapplications

import (
	"testing"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestEquipmentWorkerNamesPreserveExplicitEmptyAndCustomValues(t *testing.T) {
	for _, caps := range []sdk.Capabilities{{}, {sdk.CapabilityWorkerNameDefaultNone: true}} {
		require.Empty(t, equipmentWorkerName(caps, "", true, "AA:BB:CC:DD:1A:2B"))
		require.Equal(t, "rig-1", equipmentWorkerName(caps, "rig-1", true, "AA:BB:CC:DD:1A:2B"))
	}
	require.Equal(t, "aa:bb:cc:dd:1a:2b", equipmentWorkerName(sdk.Capabilities{}, "", false, "AA:BB:CC:DD:1A:2B"))
	require.Empty(t, equipmentWorkerName(sdk.Capabilities{sdk.CapabilityWorkerNameDefaultNone: true}, "", false, "AA:BB:CC:DD:1A:2B"))
}
