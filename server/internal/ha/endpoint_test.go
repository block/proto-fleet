package ha

import (
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEndpointHealthRequiresLocalAddressAndFreshHeartbeat(t *testing.T) {
	// Arrange
	heartbeat := filepath.Join(t.TempDir(), "endpoint-heartbeat")
	require.NoError(t, os.WriteFile(heartbeat, nil, 0o600))
	loopback := interfaceForAddress(t, netip.MustParseAddr("127.0.0.1"))
	healthy := newEndpointHealth(netip.MustParseAddr("127.0.0.1"), loopback, heartbeat, time.Second)

	// Act and assert
	require.True(t, healthy())
	stale := time.Now().Add(-2 * time.Second)
	require.NoError(t, os.Chtimes(heartbeat, stale, stale))
	require.False(t, healthy())

	future := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(heartbeat, future, future))
	require.False(t, healthy())
	fresh := time.Now()
	require.NoError(t, os.Chtimes(heartbeat, fresh, fresh))

	unassigned := newEndpointHealth(netip.MustParseAddr("192.0.2.1"), loopback, heartbeat, time.Second)
	require.False(t, unassigned())

	wrongInterface := newEndpointHealth(netip.MustParseAddr("127.0.0.1"), "missing-interface", heartbeat, time.Second)
	require.False(t, wrongInterface())
}

func interfaceForAddress(t *testing.T, want netip.Addr) string {
	t.Helper()

	interfaces, err := net.Interfaces()
	require.NoError(t, err)
	for _, networkInterface := range interfaces {
		addresses, err := networkInterface.Addrs()
		require.NoError(t, err)
		for _, address := range addresses {
			prefix, err := netip.ParsePrefix(address.String())
			if err == nil && prefix.Addr() == want {
				return networkInterface.Name
			}
		}
	}
	t.Fatalf("no interface owns %s", want)
	return ""
}
