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

type groupsServer struct {
	mdk.UnimplementedMiningGroupServiceServer
	mdk.UnimplementedMiningServiceServer
	disabled bool
	stops    int
	pools    *mdk.ReplaceMiningGroupPoolsRequest
	intent   *mdk.UpdateMiningGroupIntentRequest
	transfer *mdk.RequestMiningGroupTransferRequest
	conflict bool
}

func (s *groupsServer) GetMiningGroupCapability(context.Context, *mdk.GetMiningGroupCapabilityRequest) (*mdk.GetMiningGroupCapabilityResponse, error) {
	return &mdk.GetMiningGroupCapabilityResponse{Capability: &mdk.MiningGroupCapability{Name: "mining-groups-v1", Version: 1, Enabled: !s.disabled, StateRevision: 1, MaxGroups: 289, AssignmentGranularity: "PHYSICAL_HASHBOARD"}}, nil
}
func (s *groupsServer) GetMiningStatus(context.Context, *mdk.GetMiningStatusRequest) (*mdk.GetMiningStatusResponse, error) {
	return &mdk.GetMiningStatusResponse{Mining: true, ActiveBoards: 3}, nil
}
func (s *groupsServer) StopMining(context.Context, *mdk.StopMiningRequest) (*mdk.StopMiningResponse, error) {
	s.stops++
	return &mdk.StopMiningResponse{}, nil
}
func (s *groupsServer) ListMiningGroups(context.Context, *mdk.ListMiningGroupsRequest) (*mdk.ListMiningGroupsResponse, error) {
	return &mdk.ListMiningGroupsResponse{Groups: []*mdk.MiningGroupRecord{{Id: "g", Revision: 3, DesiredMining: true, PllAlgorithm: "PLL_ALGORITHM_NONE"}}}, nil
}
func (s *groupsServer) ListMiningGroupMemberships(context.Context, *mdk.ListMiningGroupMembershipsRequest) (*mdk.ListMiningGroupMembershipsResponse, error) {
	return &mdk.ListMiningGroupMembershipsResponse{Memberships: []*mdk.MiningGroupMembershipRecord{{GroupId: "g", MacAddress: "02:00:00:00:00:01"}}}, nil
}
func (s *groupsServer) ListMiningGroupPools(context.Context, *mdk.ListMiningGroupPoolsRequest) (*mdk.ListMiningGroupPoolsResponse, error) {
	return &mdk.ListMiningGroupPoolsResponse{Pools: []*mdk.MiningGroupPoolRecord{{Id: 1, Url: "stratum+tcp://primary.example:3333", Username: "p.worker", Enabled: true}, {Id: 2, Url: "stratum+tcp://backup.example:3333", Username: "b.worker", Priority: 1, Enabled: true}}}, nil
}
func (s *groupsServer) ReplaceMiningGroupPools(_ context.Context, r *mdk.ReplaceMiningGroupPoolsRequest) (*mdk.ReplaceMiningGroupPoolsResponse, error) {
	s.pools = r
	if s.conflict {
		return nil, fmt.Errorf("replace pools: %w", status.Error(codes.Aborted, "revision conflict"))
	}
	return &mdk.ReplaceMiningGroupPoolsResponse{Group: &mdk.MiningGroupRecord{Id: r.GroupId, Revision: r.ExpectedRevision + 1}}, nil
}
func (s *groupsServer) UpdateMiningGroupIntent(_ context.Context, r *mdk.UpdateMiningGroupIntentRequest) (*mdk.UpdateMiningGroupIntentResponse, error) {
	s.intent = r
	return &mdk.UpdateMiningGroupIntentResponse{Group: &mdk.MiningGroupRecord{Id: r.Id, Revision: r.ExpectedRevision + 1, DesiredMining: r.DesiredMining}}, nil
}
func (s *groupsServer) RequestMiningGroupTransfer(_ context.Context, r *mdk.RequestMiningGroupTransferRequest) (*mdk.RequestMiningGroupTransferResponse, error) {
	s.transfer = r
	return &mdk.RequestMiningGroupTransferResponse{Operation: &mdk.MiningGroupOperationRecord{Id: "operation", Phase: "QUIESCING", StopBlockers: []*mdk.MiningGroupStopBlocker{{MacAddress: r.MacAddresses[0]}}}}, nil
}
func TestMiningGroupsNativeTransport(t *testing.T) {
	server := grpc.NewServer()
	stub := &groupsServer{}
	mdk.RegisterMiningGroupServiceServer(server, stub)
	mdk.RegisterMiningServiceServer(server, stub)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	device := &Device{clients: []*Client{{conn: conn, mining: mdk.NewMiningServiceClient(conn)}}}
	mining, err := device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "mining_status"})
	require.NoError(t, err)
	require.Equal(t, &sdk.MiningGroupMiningStatus{Mining: true, ActiveBoards: 3}, mining.MiningStatus)
	stopped, err := device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "stop_legacy", Key: "stop"})
	require.NoError(t, err)
	require.True(t, stopped.StopRequested)
	require.Zero(t, stub.stops, "an enabled default group must not be stopped by a legacy retry")
	stub.disabled = true
	stopped, err = device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "stop_legacy", Key: "stop"})
	require.NoError(t, err)
	require.True(t, stopped.StopRequested)
	require.Equal(t, 1, stub.stops)
	stub.disabled = false
	state, err := device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "observe"})
	require.NoError(t, err)
	require.True(t, state.Supported)
	require.Len(t, state.Groups, 1)
	require.False(t, state.Groups[0].CustomPolicy)
	require.Len(t, state.Groups[0].Pools, 2)
	require.Equal(t, "b.worker", state.Groups[0].Pools[1].Username)
	_, err = device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "replace_pools", Key: "exact-key", GroupID: "g", Revision: 3, Pools: []sdk.MiningGroupPoolInput{{URL: "stratum+tcp://pool.example:3333", Username: "p.worker", Password: ""}}})
	require.NoError(t, err)
	require.NotNil(t, stub.pools.Pools[0].Password)
	require.Empty(t, *stub.pools.Pools[0].Password)
	require.Equal(t, "exact-key", stub.pools.IdempotencyKey)
	_, err = device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "start", Key: "start", GroupID: "g", Revision: 4})
	require.NoError(t, err)
	require.Equal(t, []string{"desired_mining"}, stub.intent.UpdateMask.Paths)
	require.Nil(t, stub.intent.RequestedBoardWatts)
	result, err := device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "transfer", Key: "transfer", GroupID: "g", Revision: 5, SourceID: "default", SourceRevision: 7, MACs: []string{"02:00:00:00:00:01"}})
	require.NoError(t, err)
	require.Equal(t, "QUIESCING", result.Operation.Phase)
	require.Equal(t, []string{"02:00:00:00:00:01"}, stub.transfer.MacAddresses)
	require.EqualValues(t, 7, stub.transfer.SourceExpectedRevision)
	stub.conflict = true
	result, err = device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "replace_pools", Key: "conflict", GroupID: "g", Revision: 1, Pools: []sdk.MiningGroupPoolInput{{URL: "stratum+tcp://pool.example:3333", Username: "p.worker"}}})
	require.NoError(t, err)
	require.True(t, result.RevisionConflict)
}
