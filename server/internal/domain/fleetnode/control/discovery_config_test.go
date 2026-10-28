package control

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryConfigFollowsConnection(t *testing.T) {
	r := NewRegistry()
	old := r.Register(7)
	old.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.1"}})
	require.Equal(t, []string{"10.0.0.1"}, r.DiscoveryConfig(7).GetRanges())
	current := r.Register(7)
	require.Nil(t, r.DiscoveryConfig(7))
	old.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.2"}})
	require.Nil(t, r.DiscoveryConfig(7), "evicted connection cannot replace current configuration")
	current.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{Ranges: []string{"10.0.0.3"}})
	snapshot := r.DiscoveryConfig(7)
	snapshot.Ranges[0] = "10.0.0.4"
	require.Equal(t, []string{"10.0.0.3"}, r.DiscoveryConfig(7).GetRanges())
	current.Unregister()
	require.Nil(t, r.DiscoveryConfig(7))
}
