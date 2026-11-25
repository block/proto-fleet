package sdk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPluginWorkerNameDefaults(t *testing.T) {
	for _, mac := range []string{"AA:BB:CC:DD:1A:2B", "AA-BB-CC-DD-1A-2B", "aabb.ccdd.1a2b"} {
		require.Equal(t, "aa:bb:cc:dd:1a:2b", Capabilities{}.DefaultWorkerName(mac))
		require.Empty(t, (Capabilities{CapabilityWorkerNameDefaultNone: true}).DefaultWorkerName(mac))
	}
}
