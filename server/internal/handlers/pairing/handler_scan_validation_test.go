package pairing

import (
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	pairingdomain "github.com/block/proto-fleet/server/internal/domain/pairing"
)

func TestDiscover_RejectsNonPrivateServerTargetRegardlessOfNodeLocalSubnet(t *testing.T) {
	for _, target := range []string{"169.254.169.254", "8.8.8.8", "127.0.0.1"} {
		for _, localSubnet := range []bool{false, true} {
			for _, fanOut := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/localSubnet=%t/fanOut=%t", target, localSubnet, fanOut), func(t *testing.T) {
					// Arrange: explicit ports avoid DB access; a nil stream must never be used.
					runner := &stubFleetNodeDiscoveryRunner{nodeIDs: []int64{7}}
					h := &Handler{pairingSvc: pairingdomain.NewService(nil, nil, nil, nil, nil, nil, nil, nil)}
					if fanOut {
						h.discovery = runner
					}
					req := &pb.DiscoverRequest{Mode: &pb.DiscoverRequest_NetworkScan{NetworkScan: &pb.NetworkScanModeRequest{
						Target: target, Ports: []string{"4028"}, UseFleetNodeLocalSubnet: localSubnet,
					}}}

					// Act
					err := h.Discover(ctxWithPerms(authz.PermMinerPair), connect.NewRequest(req), nil)

					// Assert: the local scan is rejected before remote dispatch or streaming.
					require.Error(t, err)
					require.True(t, fleeterror.IsInvalidArgumentError(err))
					require.Empty(t, runner.requests)
				})
			}
		}
	}
}
