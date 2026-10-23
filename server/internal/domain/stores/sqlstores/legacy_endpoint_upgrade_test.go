package sqlstores_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnpairedLegacyUpgradeDropsUnknownSameHostPeerRoute(t *testing.T) {
	_, store, org := ct1HAStore(t)
	legacy := ct1HADiscovery(org, "a", "10.0.0.11")
	legacy.Endpoints, legacy.RpcPort = nil, 0
	legacy.Port, legacy.UrlScheme = "8443", "https"
	before := saveCT1HA(t, store, "legacy", legacy)

	// The server cannot know what the old zero RPC port meant. Do not retain it
	// as another identity at the same host after scanning an explicit listener.
	upgraded := saveCT1HA(t, store, "scan-b", ct1HADiscovery(org, "b", "10.0.0.11"))
	require.Equal(t, before.DeviceIdentifier, upgraded.DeviceIdentifier)
	require.Len(t, upgraded.Endpoints, 1)
	require.Equal(t, "b", upgraded.Endpoints[0].Id)
	require.Equal(t, "10.0.0.11", upgraded.Endpoints[0].Host)
	require.EqualValues(t, 9095, upgraded.Endpoints[0].RpcPort)
	require.Equal(t, "8443", upgraded.Port)
	require.Equal(t, "https", upgraded.UrlScheme)

	// A peer can rejoin only after its own address is scanned.
	rescanned := saveCT1HA(t, store, "scan-a", ct1HADiscovery(org, "a", "10.0.0.12"))
	require.Len(t, rescanned.Endpoints, 2)
	require.Equal(t, "a", rescanned.Endpoints[0].Id)
	require.Equal(t, "10.0.0.12", rescanned.Endpoints[0].Host)
	require.EqualValues(t, 9095, rescanned.Endpoints[0].RpcPort)
}
