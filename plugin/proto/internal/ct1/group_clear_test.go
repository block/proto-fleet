package ct1

import (
	"context"
	"fmt"
	"net"
	"testing"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type clearedGroupServer struct {
	groupsServer
	mining                 bool
	storedPools            bool
	runtimePools           bool
	missing                bool
	changed                bool
	reads                  int
	diagnosticsUnavailable bool
}

func (s *clearedGroupServer) ListMiningGroups(context.Context, *mdk.ListMiningGroupsRequest) (*mdk.ListMiningGroupsResponse, error) {
	s.reads++
	revision := int64(3)
	if s.changed {
		revision += int64(s.reads)
	}
	return &mdk.ListMiningGroupsResponse{Groups: []*mdk.MiningGroupRecord{{Id: "g", Revision: revision, DesiredMining: s.mining}}}, nil
}
func (s *clearedGroupServer) ListMiningGroupPools(ctx context.Context, r *mdk.ListMiningGroupPoolsRequest) (*mdk.ListMiningGroupPoolsResponse, error) {
	if s.storedPools {
		return s.groupsServer.ListMiningGroupPools(ctx, r)
	}
	return &mdk.ListMiningGroupPoolsResponse{}, nil
}
func (s *clearedGroupServer) GetMiningGroupPoolDiagnostics(context.Context, *mdk.GetMiningGroupPoolDiagnosticsRequest) (*mdk.GetMiningGroupPoolDiagnosticsResponse, error) {
	if s.diagnosticsUnavailable {
		return nil, fmt.Errorf("group diagnostics: %w", status.Error(codes.Unimplemented, "diagnostics not implemented"))
	}
	out := &mdk.GetMiningGroupPoolDiagnosticsResponse{}
	if !s.missing {
		out.ObservedMacAddresses = []string{"02:00:00:00:00:01"}
	}
	if s.runtimePools {
		out.Pools = []*mdk.PoolConnectionDiagnostics{{PoolId: 1}}
	}
	return out, nil
}
func TestMiningGroupClearAndRuntimeVerification(t *testing.T) {
	for _, scenario := range []string{"cleared", "stored pools", "runtime pools", "missing board", "mining", "changing revision", "unsupported diagnostics"} {
		t.Run(scenario, func(t *testing.T) {
			stub := &clearedGroupServer{diagnosticsUnavailable: scenario == "unsupported diagnostics", storedPools: scenario == "stored pools", runtimePools: scenario == "runtime pools", missing: scenario == "missing board", mining: scenario == "mining", changed: scenario == "changing revision"}
			server := grpc.NewServer()
			mdk.RegisterMiningGroupServiceServer(server, stub)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(server.Stop)
			conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			device := &Device{clients: []*Client{{conn: conn}}}
			clearRequest := sdk.MiningGroupRequest{Action: "clear_pools", GroupID: "g", Revision: 3, Key: "clear-key"}
			receipt, err := device.MiningGroups(t.Context(), clearRequest)
			require.NoError(t, err)
			require.NoError(t, receipt.Validate(clearRequest))
			require.Empty(t, stub.pools.Pools)
			require.Equal(t, "clear-key", stub.pools.IdempotencyKey)
			require.EqualValues(t, 3, stub.pools.ExpectedRevision)
			stop := sdk.MiningGroupRequest{Action: "stop", GroupID: "g", Revision: 4, Key: "stop-key"}
			receipt, err = device.MiningGroups(t.Context(), stop)
			require.NoError(t, err)
			require.NoError(t, receipt.Validate(stop))
			require.False(t, stub.intent.DesiredMining)
			verify := sdk.MiningGroupRequest{Action: "verify_cleared", GroupID: "g", MACs: []string{"02:00:00:00:00:01"}}
			result, err := device.MiningGroups(t.Context(), verify)
			if scenario == "unsupported diagnostics" {
				var unsupported sdk.SDKError
				require.ErrorAs(t, err, &unsupported)
				require.Equal(t, sdk.ErrCodeUnsupportedCapability, unsupported.Code)
				return
			}
			if scenario == "changing revision" {
				require.ErrorContains(t, err, "changed during observation")
				return
			}
			if scenario == "missing board" {
				require.ErrorContains(t, err, "verification unavailable")
				return
			}
			require.NoError(t, err)
			require.NoError(t, result.Validate(verify))
			require.Equal(t, scenario == "cleared", *result.PoolsCleared)
		})
	}
}

func TestMiningGroupClearPreflightChecksBothControllers(t *testing.T) {
	for _, scenario := range []string{"peer unsupported", "peer observes board", "missing board with configured pools"} {
		t.Run(scenario, func(t *testing.T) {
			primary := &clearedGroupServer{storedPools: true, runtimePools: true, missing: true}
			standby := &clearedGroupServer{diagnosticsUnavailable: scenario == "peer unsupported", missing: scenario == "missing board with configured pools"}
			a := startHAGroupServer(t, "a", "b", primary)
			b := startHAGroupServer(t, "b", "a", standby)
			a.primary.Store(true)
			d, err := NewDevice(t.Context(), "container", haInfo(a, b), sdk.UsernamePassword{})
			require.NoError(t, err)
			defer d.Close(t.Context())
			result, err := d.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "verify_cleared", GroupID: "g", MACs: []string{"02:00:00:00:00:01"}})
			switch scenario {
			case "peer unsupported":
				var unsupported sdk.SDKError
				require.ErrorAs(t, err, &unsupported)
				require.Equal(t, sdk.ErrCodeUnsupportedCapability, unsupported.Code)
			case "missing board with configured pools":
				require.ErrorContains(t, err, "selected board runtime pool verification unavailable")
			default:
				require.NoError(t, err)
				require.NotNil(t, result.PoolsCleared)
				require.False(t, *result.PoolsCleared)
			}
			require.Nil(t, primary.pools)
			require.Nil(t, standby.pools)
		})
	}
}
