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
	"google.golang.org/protobuf/proto"
)

type groupsServer struct {
	mdk.UnimplementedMiningGroupServiceServer
	mdk.UnimplementedMiningServiceServer
	mdk.UnimplementedPoolServiceServer
	disabled             bool
	stops                int
	pools                *mdk.ReplaceMiningGroupPoolsRequest
	poolReads            int
	lostPoolsAck         bool
	intent               *mdk.UpdateMiningGroupIntentRequest
	transfer             *mdk.RequestMiningGroupTransferRequest
	conflict             bool
	deactivation         *mdk.DeactivateMiningGroupsRequest
	defaultGroup         *mdk.MiningGroupRecord
	starts               int
	lostStartAck         bool
	legacyPoolReads      int
	changingDefaultPools bool
	legacyPoolURL        string
	groupRevision        int64
	replaceError         error
}

func (s *groupsServer) DeactivateMiningGroups(_ context.Context, r *mdk.DeactivateMiningGroupsRequest) (*mdk.DeactivateMiningGroupsResponse, error) {
	s.deactivation = r
	if s.conflict {
		return nil, fmt.Errorf("deactivate groups: %w", status.Error(codes.Aborted, "revision changed"))
	}
	s.disabled = true
	return &mdk.DeactivateMiningGroupsResponse{Capability: &mdk.MiningGroupCapability{Enabled: false}}, nil
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
func (s *groupsServer) StartMining(context.Context, *mdk.StartMiningRequest) (*mdk.StartMiningResponse, error) {
	s.starts++
	s.defaultGroup.DesiredMining = true
	s.defaultGroup.Revision++
	if s.lostStartAck {
		s.lostStartAck = false
		return nil, fmt.Errorf("start Default: %w", status.Error(codes.Unavailable, "response lost after commit"))
	}
	return &mdk.StartMiningResponse{}, nil
}
func (s *groupsServer) GetMiningGroup(_ context.Context, request *mdk.GetMiningGroupRequest) (*mdk.GetMiningGroupResponse, error) {
	if request.Id == "g" && s.groupRevision > 0 {
		return &mdk.GetMiningGroupResponse{Group: &mdk.MiningGroupRecord{Id: "g", Revision: s.groupRevision}}, nil
	}
	return &mdk.GetMiningGroupResponse{Group: proto.CloneOf(s.defaultGroup)}, nil
}
func (s *groupsServer) ListMiningGroups(context.Context, *mdk.ListMiningGroupsRequest) (*mdk.ListMiningGroupsResponse, error) {
	out := &mdk.ListMiningGroupsResponse{Groups: []*mdk.MiningGroupRecord{{Id: "g", Revision: 3, DesiredMining: true, PllAlgorithm: "PLL_ALGORITHM_NONE"}}}
	if s.defaultGroup != nil {
		out.Groups = append(out.Groups, proto.CloneOf(s.defaultGroup))
	}
	return out, nil
}
func (s *groupsServer) ListPools(context.Context, *mdk.ListPoolsRequest) (*mdk.ListPoolsResponse, error) {
	s.legacyPoolReads++
	username := "default.worker"
	if s.changingDefaultPools {
		username = fmt.Sprintf("worker-%d", s.legacyPoolReads)
	}
	url := s.legacyPoolURL
	if url == "" {
		url = "stratum+tcp://pool.example:3333"
	}
	return &mdk.ListPoolsResponse{Pools: []*mdk.Pool{{Id: 8, Url: url, Username: username, Enabled: true}}}, nil
}
func (s *groupsServer) ListMiningGroupMemberships(context.Context, *mdk.ListMiningGroupMembershipsRequest) (*mdk.ListMiningGroupMembershipsResponse, error) {
	return &mdk.ListMiningGroupMembershipsResponse{Memberships: []*mdk.MiningGroupMembershipRecord{{GroupId: "g", MacAddress: "02:00:00:00:00:01"}}}, nil
}
func (s *groupsServer) ListMiningGroupPools(context.Context, *mdk.ListMiningGroupPoolsRequest) (*mdk.ListMiningGroupPoolsResponse, error) {
	s.poolReads++
	pools := []*mdk.MiningGroupPoolRecord{{Id: 21, Url: "stratum+tcp://primary.example:3333", Username: "p.worker", Enabled: true}, {Id: 35, Url: "stratum+tcp://backup.example:3333", Username: "b.worker", Priority: 1, Enabled: true}}
	if s.pools != nil {
		pools = append(pools, &mdk.MiningGroupPoolRecord{Id: 42, Priority: 2})
	}
	return &mdk.ListMiningGroupPoolsResponse{Pools: pools}, nil
}
func (s *groupsServer) ReplaceMiningGroupPools(_ context.Context, r *mdk.ReplaceMiningGroupPoolsRequest) (*mdk.ReplaceMiningGroupPoolsResponse, error) {
	if s.conflict {
		return nil, fmt.Errorf("replace pools: %w", status.Error(codes.Aborted, "revision conflict"))
	}
	if s.pools != nil && s.pools.IdempotencyKey == r.IdempotencyKey && !proto.Equal(s.pools, r) {
		return nil, fmt.Errorf("replace pools: %w", status.Error(codes.AlreadyExists, "same key with different request"))
	}
	if s.replaceError != nil {
		return nil, s.replaceError
	}
	s.pools = r
	if s.lostPoolsAck {
		s.lostPoolsAck = false
		return nil, fmt.Errorf("replace pools: %w", status.Error(codes.Unavailable, "response lost after commit"))
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
func newMiningGroupsTestDevice(t *testing.T, stub *groupsServer) *Device {
	t.Helper()
	server := grpc.NewServer()
	mdk.RegisterMiningGroupServiceServer(server, stub)
	mdk.RegisterMiningServiceServer(server, stub)
	mdk.RegisterPoolServiceServer(server, stub)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &Device{clients: []*Client{{conn: conn, mining: mdk.NewMiningServiceClient(conn)}}}
}

func TestMiningGroupsNativeTransport(t *testing.T) {
	stub := &groupsServer{}
	device := newMiningGroupsTestDevice(t, stub)
	disabled, err := device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "deactivate", Key: "disable", Revision: 7})
	require.NoError(t, err)
	require.True(t, disabled.Deactivated)
	require.Equal(t, int64(7), stub.deactivation.ExpectedStateRevision)
	require.Equal(t, "disable", stub.deactivation.IdempotencyKey)
	stub.disabled = false
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
	request := sdk.MiningGroupRequest{Action: "replace_pools", Key: "exact-key", GroupID: "g", Revision: 3, PoolIDs: []int32{21, 35, 0}, PoolNames: []string{"Pool 2", "Pool 3", "Pool 3 (2)"}, Pools: []sdk.MiningGroupPoolInput{
		{URL: "stratum+tcp://pool.example:3333", Username: "p.worker", Password: ""},
		{URL: "stratum+tcp://backup.example:3333", Username: "b.worker", Password: ""},
		{URL: "stratum+tcp://new.example:3333", Username: "new.worker", Password: ""},
	}}
	stub.lostPoolsAck = true
	_, err = device.MiningGroups(t.Context(), request)
	require.Equal(t, codes.Unavailable, status.Code(err))
	replayed, err := device.MiningGroups(t.Context(), request)
	require.NoError(t, err)
	require.EqualValues(t, 4, replayed.Group.Revision)
	require.Equal(t, 1, stub.poolReads, "a replacement must use the IDs in the saved request")
	require.NotNil(t, stub.pools.Pools[0].Password)
	require.Empty(t, *stub.pools.Pools[0].Password)
	require.EqualValues(t, 21, stub.pools.Pools[0].Pool.Id)
	require.EqualValues(t, 35, stub.pools.Pools[1].Pool.Id)
	require.Zero(t, stub.pools.Pools[2].Pool.Id)
	for i, name := range request.PoolNames {
		require.Equal(t, name, stub.pools.Pools[i].Pool.Name)
	}
	require.Equal(t, "exact-key", stub.pools.IdempotencyKey)
	_, err = device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "clear_pools", Key: "clear", GroupID: "g", Revision: 4})
	require.NoError(t, err)
	require.Empty(t, stub.pools.Pools)
	require.Equal(t, 1, stub.poolReads, "a clear must reach the controller without a pool read")
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

func TestResumeDefaultMiningReplaysReadbackAndRespectsLaterStop(t *testing.T) {
	stub := &groupsServer{defaultGroup: &mdk.MiningGroupRecord{Id: "default", Revision: 7}, lostStartAck: true}
	device := newMiningGroupsTestDevice(t, stub)
	request := sdk.MiningGroupRequest{Action: "start", GroupID: "default", Revision: 7, Key: "resume"}
	_, err := device.MiningGroups(t.Context(), request)
	require.Equal(t, codes.Unavailable, status.Code(err))
	receipt, err := device.MiningGroups(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, receipt.Validate(request))
	require.True(t, receipt.Group.Mining)
	require.Equal(t, 1, stub.starts)
	require.Nil(t, stub.intent, "Default must use the legacy API")

	stub.defaultGroup.DesiredMining = false
	stub.defaultGroup.Revision++
	receipt, err = device.MiningGroups(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, receipt.Validate(request))
	require.True(t, receipt.DefaultResumeSuperseded)
	require.False(t, receipt.Group.Mining)
	require.False(t, receipt.RevisionConflict, "Fleet must keep the original resume revision")
	require.Equal(t, 1, stub.starts, "retry must respect the later stop")
}

func TestDefaultPoolEndpointsRemovePrivateURLComponents(t *testing.T) {
	for raw, want := range map[string]string{
		"stratum+tcp://user:secret@pool.example:3333/path?token=secret#secret": "stratum+tcp://pool.example:3333",
		"pool.example:3333?token=secret":                                       "pool.example:3333",
		"datum://pool.example:23334":                                           "datum://pool.example:23334",
		"invalid://user:secret@pool.example:3333":                              "invalid://pool.example:3333",
	} {
		require.Equal(t, want, publicDefaultPoolURL(raw))
	}
}

func TestLegacyPoolIDRecoveryUsesRevisionAndSlots(t *testing.T) {
	stub := &groupsServer{groupRevision: 1, replaceError: status.Error(codes.Unavailable, "different wording")}
	device := newMiningGroupsTestDevice(t, stub)
	request := sdk.MiningGroupRequest{Action: "replace_pools", GroupID: "g", Revision: 1, Key: "old", Pools: []sdk.MiningGroupPoolInput{{URL: "stratum+tcp://pool.example:3333", Username: "worker"}}}
	result, err := device.MiningGroups(t.Context(), request)
	require.NoError(t, err)
	require.True(t, result.RevisionConflict)
	stub.groupRevision++
	result, err = device.MiningGroups(t.Context(), request)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.False(t, result.RevisionConflict)
}

func TestMiningGroupObservationDoesNotReadLegacyPools(t *testing.T) {
	stub := &groupsServer{defaultGroup: &mdk.MiningGroupRecord{Id: "default", Revision: 7}, changingDefaultPools: true}
	device := newMiningGroupsTestDevice(t, stub)
	result, err := device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: "observe"})
	require.NoError(t, err)
	require.Len(t, result.Groups, 2)
	require.Nil(t, result.Groups[1].Pools)
	require.Zero(t, stub.legacyPoolReads)
	_, err = device.MiningGroups(t.Context(), sdk.MiningGroupRequest{Action: sdk.MiningGroupReadDefault})
	require.ErrorContains(t, err, "Default pools changed during observation")
}

func TestDefaultReviewFingerprintDetectsRedactedChanges(t *testing.T) {
	stub := &groupsServer{defaultGroup: &mdk.MiningGroupRecord{Id: "default", Revision: 7}, legacyPoolURL: "stratum+tcp://pool.example:3333/one"}
	device := newMiningGroupsTestDevice(t, stub)
	request := sdk.MiningGroupRequest{Action: sdk.MiningGroupReadDefault, DefaultURLSigningKey: make([]byte, 32)}
	before, err := device.MiningGroups(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, before.Validate(request))
	stub.legacyPoolURL = "stratum+tcp://pool.example:3333/two"
	after, err := device.MiningGroups(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, before.Group.Pools, after.Group.Pools)
	require.NotEqual(t, before.Group.DefaultURLFingerprint, after.Group.DefaultURLFingerprint)
}
