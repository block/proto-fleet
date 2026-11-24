package pairing

import (
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	fleetnodediscovery "github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
	pairingdomain "github.com/block/proto-fleet/server/internal/domain/pairing"
	pairingmocks "github.com/block/proto-fleet/server/internal/domain/pairing/mocks"
)

func TestDiscover_RejectsNonPrivateDirectServerTarget(t *testing.T) {
	for _, target := range []string{"169.254.169.254", "8.8.8.8", "127.0.0.1"} {
		for _, fanOut := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fanOut=%t", target, fanOut), func(t *testing.T) {
				// Arrange
				runner := &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}}
				h := &Handler{pairingSvc: pairingdomain.NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil)}
				if fanOut {
					h.discovery = runner
				}
				req := &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_NetworkScan{NetworkScan: &pb.NetworkScanModeRequest{Target: target, Ports: []string{"4028"}}}}

				// Act
				err := h.Discover(ctxWithPerms(authz.PermMinerPair), connect.NewRequest(req), nil)

				// Assert
				require.True(t, fleeterror.IsInvalidArgumentError(err))
				require.Empty(t, runner.requests)
			})
		}
	}
}

func TestDiscover_AutomaticNodeRangesDoNotRequireServerSubnet(t *testing.T) {
	// Arrange: no server discovery dependencies prove automatic discovery never
	// initializes a server-local scan, even if the client retains an obsolete target.
	runner := &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}}
	client := discoveryClientForHandler(t, &Handler{
		pairingSvc: pairingdomain.NewService(nil, &alreadyPairedLookupStore{}, nil, nil, nil, nil, nil, nil, nil),
		discovery:  runner,
	})
	req := &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_NetworkScan{NetworkScan: &pb.NetworkScanModeRequest{
		Target: "8.8.8.8", Ports: []string{"4028"}, UseFleetNodeLocalSubnet: true,
	}}}

	// Act
	stream, err := client.Discover(t.Context(), connect.NewRequest(req))
	require.NoError(t, err)
	for stream.Receive() {
	}

	// Assert
	require.NoError(t, stream.Err())
	require.Len(t, runner.requests, 1)
	require.True(t, runner.requests[0].GetNetworkScan().GetUseFleetNodeLocalSubnet())
}

func TestDiscover_AutomaticServerOnlyScanUsesServerDiscoveryPorts(t *testing.T) {
	for _, hasDirectory := range []bool{false, true} {
		t.Run(fmt.Sprint("nodeDirectory=", hasDirectory), func(t *testing.T) {
			// Arrange
			caps := pairingmocks.NewMockCapabilitiesProvider(gomock.NewController(t))
			// Server-only automatic scanning must initialize the existing server scan.
			// A missing canonical port list gives a deterministic pre-probe failure.
			caps.EXPECT().GetDefaultDiscoveryPorts(gomock.Any()).Return(nil).Times(1)
			h := &Handler{pairingSvc: pairingdomain.NewService(nil, nil, nil, nil, nil, nil, caps, nil, nil)}
			runner := &stubFleetNodeDiscoveryRunner{}
			if hasDirectory {
				h.discovery = runner
			}
			client := discoveryClientForHandler(t, h)

			// Act
			stream, err := client.Discover(t.Context(), connect.NewRequest(&pb.DiscoverRequest{Mode: &pb.DiscoverRequest_NetworkScan{NetworkScan: &pb.NetworkScanModeRequest{
				Target: "192.168.1.0/24", UseFleetNodeLocalSubnet: true,
			}}}))
			require.NoError(t, err)
			var warnings []string
			for stream.Receive() {
				warnings = append(warnings, stream.Msg().GetWarning())
			}

			// Assert
			require.NoError(t, stream.Err())
			require.Equal(t, []string{"Fleet Server: discovery failed"}, warnings)
			require.Empty(t, runner.requests)
		})
	}
}

func TestDiscover_AutomaticUnconfiguredNodeDoesNotFallBackToServer(t *testing.T) {
	// Arrange
	reg := control.NewRegistry()
	node := reg.Register(7)
	defer node.Unregister()
	node.SetDiscoveryConfig(&gatewaypb.DiscoveryConfig{})
	// No server service: a fallback would panic instead of returning the node's
	// actionable configuration warning.
	runner := credentialAckDiscoveryRunner{fleetnodediscovery.NewService(reg, nil)}
	client := discoveryClientForHandler(t, &Handler{discovery: runner})

	// Act
	stream, err := client.Discover(t.Context(), connect.NewRequest(&pb.DiscoverRequest{Mode: &pb.DiscoverRequest_NetworkScan{NetworkScan: &pb.NetworkScanModeRequest{
		Target: "192.168.1.0/24", UseFleetNodeLocalSubnet: true,
	}}}))
	require.NoError(t, err)
	var warnings []string
	for stream.Receive() {
		warnings = append(warnings, stream.Msg().GetWarning())
	}

	// Assert
	require.NoError(t, stream.Err())
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "configure Fleet Node discovery ranges")
	select {
	case command := <-node.Outgoing:
		t.Fatalf("unconfigured node received command %q", command.GetCommandId())
	default:
	}
}
