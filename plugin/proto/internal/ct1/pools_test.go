package ct1

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
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

// Implements the firmware contract: reads never disclose passwords, and an
// empty UpdatePool password preserves the stored value.
type poolServer struct {
	mdk.UnimplementedPoolServiceServer
	mdk.UnimplementedMiningGroupServiceServer
	pools             []*mdk.Pool
	passwords         map[int64]string
	nextID            int64
	enabledGroups     bool
	writes            int
	loseReply         bool
	ignoreWrite       bool
	modeError         error
	modeErrorAtRead   int
	reads             int
	modeReads         int
	loseDelete        bool
	ignoreDelete      bool
	enableAfterWrite  bool
	runtimeOverride   bool
	runtimeAfterWrite bool
}

func (s *poolServer) GetMiningGroupCapability(context.Context, *mdk.GetMiningGroupCapabilityRequest) (*mdk.GetMiningGroupCapabilityResponse, error) {
	s.modeReads++
	if s.modeError != nil && (s.modeErrorAtRead == 0 || s.modeReads == s.modeErrorAtRead) {
		return nil, s.modeError
	}
	return &mdk.GetMiningGroupCapabilityResponse{Capability: &mdk.MiningGroupCapability{Enabled: s.enabledGroups}}, nil
}
func (s *poolServer) ListPools(context.Context, *mdk.ListPoolsRequest) (*mdk.ListPoolsResponse, error) {
	s.reads++
	result := &mdk.ListPoolsResponse{}
	for _, p := range s.pools {
		result.Pools = append(result.Pools, proto.CloneOf(p))
	}
	return result, nil
}

func (s *poolServer) GetPoolDiagnostics(context.Context, *mdk.GetPoolDiagnosticsRequest) (*mdk.GetPoolDiagnosticsResponse, error) {
	result := &mdk.GetPoolDiagnosticsResponse{}
	if s.runtimeOverride || (s.runtimeAfterWrite && s.writes > 0) {
		result.Pools = []*mdk.PoolConnectionDiagnostics{{PoolId: -1, PoolName: "Native DATUM"}}
		return result, nil
	}
	for _, pool := range s.pools {
		if pool.Enabled {
			result.WeightedMode = result.WeightedMode || pool.ShareWeight != nil
			result.Pools = append(result.Pools, &mdk.PoolConnectionDiagnostics{PoolId: pool.Id, PoolName: pool.Name})
		}
	}
	return result, nil
}
func (s *poolServer) CreatePool(_ context.Context, r *mdk.CreatePoolRequest) (*mdk.CreatePoolResponse, error) {
	s.writes++
	if err := s.validate(r.Pool); err != nil {
		return nil, err
	}
	s.nextID++
	p := proto.CloneOf(r.Pool)
	p.Id = s.nextID
	s.pools = append(s.pools, p)
	s.passwords[p.Id] = r.Password
	s.enabledGroups = s.enabledGroups || s.enableAfterWrite
	if s.loseReply {
		s.loseReply = false
		return nil, fmt.Errorf("pool test: %w", status.Error(codes.Unavailable, "lost reply: "+r.Password))
	}
	return &mdk.CreatePoolResponse{Pool: proto.CloneOf(p)}, nil
}
func (s *poolServer) UpdatePool(_ context.Context, r *mdk.UpdatePoolRequest) (*mdk.UpdatePoolResponse, error) {
	s.writes++
	for i, p := range s.pools {
		if p.Id == r.Pool.Id {
			if slices.Equal(r.UpdateMask.GetPaths(), []string{"enabled"}) {
				p.Enabled = r.Pool.Enabled
				s.enabledGroups = s.enabledGroups || s.enableAfterWrite
				return &mdk.UpdatePoolResponse{Pool: proto.CloneOf(p)}, nil
			}
			if err := s.validate(r.Pool); err != nil {
				return nil, err
			}
			if !s.ignoreWrite {
				s.pools[i] = proto.CloneOf(r.Pool)
				if r.Password != "" {
					s.passwords[p.Id] = r.Password
				}
			}
			s.enabledGroups = s.enabledGroups || s.enableAfterWrite
			if s.loseReply {
				s.loseReply = false
				return nil, fmt.Errorf("pool test: %w", status.Error(codes.Unavailable, "lost reply: "+r.Password))
			}
			return &mdk.UpdatePoolResponse{Pool: proto.CloneOf(s.pools[i])}, nil
		}
	}
	return nil, fmt.Errorf("pool test: %w", status.Error(codes.NotFound, "pool missing"))
}
func (s *poolServer) DeletePool(_ context.Context, r *mdk.DeletePoolRequest) (*mdk.DeletePoolResponse, error) {
	s.writes++
	if !s.ignoreDelete {
		s.pools = slices.DeleteFunc(s.pools, func(p *mdk.Pool) bool { return p.Id == r.Id })
		delete(s.passwords, r.Id)
	}
	s.enabledGroups = s.enabledGroups || s.enableAfterWrite
	if s.loseDelete {
		s.loseDelete = false
		return nil, fmt.Errorf("pool test: %w", status.Error(codes.Unavailable, "lost delete reply"))
	}
	return &mdk.DeletePoolResponse{}, nil
}

func TestWholeContainerClearPools(t *testing.T) {
	for _, scenario := range []string{"last pool", "disabled pool", "already empty", "groups enabled", "mode changes", "ignored delete", "lost delete reply", "runtime override"} {
		t.Run(scenario, func(t *testing.T) {
			stub := &poolServer{
				pools:     []*mdk.Pool{{Id: 1, Name: "Primary", Url: "stratum+tcp://pool.example", Username: "worker", Enabled: true}},
				passwords: map[int64]string{1: "private"},
			}
			switch scenario {
			case "disabled pool":
				stub.pools[0].Enabled = false
			case "already empty":
				stub.pools, stub.passwords = nil, map[int64]string{}
			case "groups enabled":
				stub.enabledGroups = true
			case "mode changes":
				stub.enableAfterWrite = true
			case "ignored delete":
				stub.ignoreDelete = true
			case "lost delete reply":
				stub.loseDelete = true
			case "runtime override":
				stub.runtimeAfterWrite = true
			}
			d := poolTestDevice(t, stub)
			err := d.UpdateMiningPools(t.Context(), nil)
			if scenario == "last pool" || scenario == "disabled pool" || scenario == "already empty" {
				require.NoError(t, err)
				stored, err := d.GetStoredMiningPools(t.Context())
				require.NoError(t, err)
				require.Empty(t, stored)
				runtime, err := d.GetMiningPools(t.Context())
				require.NoError(t, err)
				require.Empty(t, runtime)
				require.Empty(t, stub.passwords)
			} else {
				require.Error(t, err)
				if scenario == "groups enabled" {
					require.Zero(t, stub.writes)
					require.Equal(t, sdk.WriteNotDispatched, sdk.WriteEvidence(err).GetReason())
				} else {
					require.Nil(t, sdk.WriteEvidence(err), "partial or uncertain clears must never be classified as not dispatched")
				}
			}
		})
	}
}

// Mirrors the pinned firmware's enabled-stack and unique-name constraints.
// See Frigate 6acbeba0 pkg/api/pool_service.go and pkg/pools/validate.go.
func (s *poolServer) validate(want *mdk.Pool) error {
	enabled := 0
	if want.Enabled {
		enabled++
	}
	for _, p := range s.pools {
		if p.Id == want.Id {
			continue
		}
		if p.Name == want.Name || (p.Enabled && want.Enabled && (p.Priority == want.Priority || strings.EqualFold(p.Url, want.Url))) {
			return fmt.Errorf("pool test: %w", status.Error(codes.InvalidArgument, "duplicate name, priority or URL"))
		}
		if p.Enabled {
			enabled++
		}
	}
	if enabled > 3 {
		return fmt.Errorf("pool test: %w", status.Error(codes.InvalidArgument, "too many enabled pools"))
	}
	return nil
}

func poolTestDevice(t *testing.T, s *poolServer) *Device {
	return &Device{clients: []*Client{poolTestClient(t, s, nil)}}
}

func poolTestClient(t *testing.T, s *poolServer, ha *haServer) *Client {
	t.Helper()
	server := grpc.NewServer()
	mdk.RegisterPoolServiceServer(server, s)
	mdk.RegisterMiningGroupServiceServer(server, s)
	if ha != nil {
		mdk.RegisterSystemServiceServer(server, ha)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	c := &Client{conn: conn, system: mdk.NewSystemServiceClient(conn)}
	if ha != nil {
		c.memberID = ha.id
	}
	return c
}

func TestWholeContainerPasswordUpdatesKeepPoolsEnabled(t *testing.T) {
	for _, test := range []struct {
		name             string
		modeErrorAtRead  int
		loseReply        bool
		enableAfterWrite bool
		wantWrites       int
	}{
		{name: "success", wantWrites: 2},
		{name: "mode check fails before first write", modeErrorAtRead: 2},
		{name: "mode check fails before second write", modeErrorAtRead: 3, wantWrites: 1},
		{name: "mode check fails before readback", modeErrorAtRead: 4, wantWrites: 2},
		{name: "lost update reply", loseReply: true, wantWrites: 1},
		{name: "mode changes after first write", enableAfterWrite: true, wantWrites: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := &poolServer{
				passwords: map[int64]string{1: "old-primary", 2: "old-backup"},
				pools: []*mdk.Pool{
					{Id: 1, Name: "Primary", Url: "stratum+tcp://primary.example", Username: "primary", Enabled: true},
					{Id: 2, Name: "Backup", Url: "stratum+tcp://backup.example", Username: "backup", Priority: 1, Enabled: true},
				},
				modeErrorAtRead: test.modeErrorAtRead, loseReply: test.loseReply, enableAfterWrite: test.enableAfterWrite,
			}
			if test.modeErrorAtRead > 0 {
				stub.modeError = status.Error(codes.Unavailable, "mode check unavailable")
			}
			desired := []sdk.MiningPoolConfig{
				{Priority: 0, URL: "stratum+tcp://primary.example", WorkerName: "primary", Password: "new-primary"},
				{Priority: 1, URL: "stratum+tcp://backup.example", WorkerName: "backup", Password: "new-backup"},
			}
			err := poolTestDevice(t, stub).UpdateMiningPools(t.Context(), desired)
			if test.modeErrorAtRead > 0 || test.loseReply || test.enableAfterWrite {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, stub.pools, 2)
			for i, pool := range stub.pools {
				require.True(t, pool.Enabled, "password updates must leave existing pools enabled")
				require.EqualValues(t, i+1, pool.Id, "password updates retain existing pools")
			}
			require.Equal(t, test.wantWrites, stub.writes, "only credential updates are needed; failures stop further writes")
			if err != nil {
				require.Equal(t, test.wantWrites == 0, sdk.WriteEvidence(err).GetReason() == sdk.WriteNotDispatched, "preparation evidence must not hide a prior pool write")
			}
			wantPasswords := map[int64]string{1: "old-primary", 2: "old-backup"}
			if test.wantWrites >= 1 {
				wantPasswords[2] = "new-backup"
			}
			if test.wantWrites == 2 {
				wantPasswords[1] = "new-primary"
			}
			require.Equal(t, wantPasswords, stub.passwords)
		})
	}
}

func TestWholeContainerPoolStackReconcilesBackupsCredentialsAndRemovals(t *testing.T) {
	stub := &poolServer{nextID: 10, passwords: map[int64]string{1: "old-primary", 2: "old-backup", 3: "unused"}, pools: []*mdk.Pool{
		{Id: 1, Name: "Primary", Url: "stratum+tcp://old.example", Username: "old", Enabled: true},
		{Id: 2, Name: "Backup", Url: "stratum+tcp://old-backup.example", Username: "old", Priority: 1, Enabled: true},
		{Id: 3, Name: "Unused", Url: "stratum+tcp://unused.example", Username: "old", Priority: 2},
	}}
	device := poolTestDevice(t, stub)
	desired := []sdk.MiningPoolConfig{{Priority: 0, URL: "stratum+tcp://primary.example", WorkerName: "p.primary", Password: "new-secret"}, {Priority: 1, URL: "stratum+tcp://backup.example", WorkerName: "p.backup", Password: ""}}
	require.NoError(t, device.UpdateMiningPools(t.Context(), desired))
	actual, err := device.GetMiningPools(t.Context())
	require.NoError(t, err)
	require.Equal(t, []sdk.ConfiguredPool{{Priority: 0, URL: desired[0].URL, Username: desired[0].WorkerName}, {Priority: 1, URL: desired[1].URL, Username: desired[1].WorkerName}}, actual)
	require.Len(t, stub.pools, 2)
	for _, p := range stub.pools {
		require.True(t, p.Enabled)
		require.Equal(t, desired[p.Priority].Password, stub.passwords[p.Id])
	}
	require.NotContains(t, stub.passwords, int64(2), "empty password must recreate the old credential")
	stored, err := device.GetStoredMiningPools(t.Context())
	require.NoError(t, err)
	for _, p := range stored {
		require.Nil(t, p.Password, "write acknowledgements are not password readback")
	}
}

func TestWholeContainerPoolStackRejectsGroupsAndWeightedModeBeforeWriting(t *testing.T) {
	for _, groups := range []bool{true, false} {
		t.Run(map[bool]string{true: "groups", false: "weighted"}[groups], func(t *testing.T) {
			weight := 1.0
			stub := &poolServer{enabledGroups: groups, passwords: map[int64]string{}, pools: []*mdk.Pool{{Id: 1, Enabled: true, ShareWeight: &weight}}}
			device := poolTestDevice(t, stub)
			err := device.UpdateMiningPools(t.Context(), []sdk.MiningPoolConfig{{URL: "stratum+tcp://pool.example", WorkerName: "payout"}})
			require.Error(t, err)
			require.Equal(t, codes.FailedPrecondition, status.Code(err))
			evidence := sdk.WriteEvidence(err)
			require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
			require.Equal(t, string(sdk.WriteReasonInvalidRequest), evidence.GetMetadata()["reason"])
			require.Zero(t, stub.writes)
		})
	}
}

func TestWholeContainerPoolStackStopsAfterLostReplyAndReconcilesOnExplicitRetry(t *testing.T) {
	stub := &poolServer{passwords: map[int64]string{}, loseReply: true}
	device := poolTestDevice(t, stub)
	desired := []sdk.MiningPoolConfig{{Priority: 0, URL: "stratum+tcp://pool.example", WorkerName: "payout", Password: "private-secret"}, {Priority: 1, URL: "stratum+tcp://backup.example", WorkerName: "backup", Password: "backup-secret"}}
	err := device.UpdateMiningPools(t.Context(), desired)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
	require.Equal(t, 1, stub.writes, "a lost acknowledgement stops the sequence without transport replay")
	require.NoError(t, device.UpdateMiningPools(t.Context(), desired))
	require.Len(t, stub.pools, 2, "retry starts from firmware readback and does not accumulate duplicate slots")
}

func TestWholeContainerPoolStackRequiresReadbackMatch(t *testing.T) {
	stub := &poolServer{passwords: map[int64]string{}, ignoreWrite: true, pools: []*mdk.Pool{{Id: 1, Name: "Primary", Url: "stratum+tcp://old.example", Username: "old", Enabled: true}}}
	device := poolTestDevice(t, stub)
	err := device.UpdateMiningPools(t.Context(), []sdk.MiningPoolConfig{{URL: "stratum+tcp://pool.example", WorkerName: "payout", Password: "secret"}})
	require.ErrorContains(t, err, "readback")
}

func TestWholeContainerPoolStackSwapsURLsAndAvoidsNameCollisions(t *testing.T) {
	stub := &poolServer{nextID: 10, passwords: map[int64]string{}, pools: []*mdk.Pool{
		{Id: 1, Name: "Fleet pool 3", Url: "stratum+tcp://primary.example", Username: "old", Enabled: true},
		{Id: 2, Name: "Backup", Url: "stratum+tcp://backup.example", Username: "old", Priority: 1, Enabled: true},
	}}
	device := poolTestDevice(t, stub)
	desired := []sdk.MiningPoolConfig{
		{Priority: 0, URL: "stratum+tcp://backup.example", WorkerName: "new", Password: "secret"},
		{Priority: 1, URL: "stratum+tcp://primary.example", WorkerName: "new", Password: "secret"},
		{Priority: 2, URL: "stratum+tcp://extra.example", WorkerName: "new", Password: "secret"},
	}
	require.NoError(t, device.UpdateMiningPools(t.Context(), desired))
}

func TestWholeContainerPoolStackRequiresKnownMode(t *testing.T) {
	for _, code := range []codes.Code{codes.Unimplemented, codes.Unavailable} {
		t.Run(code.String(), func(t *testing.T) {
			stub := &poolServer{modeError: status.Error(code, "unknown mode")}
			err := poolTestDevice(t, stub).UpdateMiningPools(t.Context(), []sdk.MiningPoolConfig{{URL: "stratum+tcp://pool.example", WorkerName: "worker"}})
			require.Error(t, err)
			require.Equal(t, code, status.Code(err), "preserve transport failures")
			evidence := sdk.WriteEvidence(err)
			require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
			require.Equal(t, string(sdk.WriteReasonReadFailed), evidence.GetMetadata()["reason"])
			require.Zero(t, stub.writes)
		})
	}
}

func TestWholeContainerPoolStackStopsWhenModeChanges(t *testing.T) {
	stub := &poolServer{passwords: map[int64]string{}, enableAfterWrite: true}
	err := poolTestDevice(t, stub).UpdateMiningPools(t.Context(), []sdk.MiningPoolConfig{
		{Priority: 0, URL: "stratum+tcp://pool.example", WorkerName: "worker"},
		{Priority: 1, URL: "stratum+tcp://backup.example", WorkerName: "worker"},
	})
	require.ErrorContains(t, err, "mining groups")
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, 1, stub.writes)
}

func TestWholeContainerPoolStackLostDeleteReply(t *testing.T) {
	stub := &poolServer{nextID: 10, passwords: map[int64]string{1: "old"}, loseDelete: true, pools: []*mdk.Pool{
		{Id: 1, Name: "Primary", Url: "stratum+tcp://pool.example", Username: "worker", Enabled: true},
	}}
	device := poolTestDevice(t, stub)
	desired := []sdk.MiningPoolConfig{{URL: "stratum+tcp://pool.example", WorkerName: "worker"}}
	require.Error(t, device.UpdateMiningPools(t.Context(), desired))
	require.Empty(t, stub.pools)
	require.Equal(t, 2, stub.writes)
	require.NoError(t, device.UpdateMiningPools(t.Context(), desired))
	require.Len(t, stub.pools, 1)
	require.Empty(t, stub.passwords[stub.pools[0].Id])
}

func TestWholeContainerPoolStackPinsHAControllerUntilExplicitRetry(t *testing.T) {
	a, b := &haServer{id: "a", peer: "b"}, &haServer{id: "b", peer: "a"}
	a.primary.Store(true)
	primary := &poolServer{passwords: map[int64]string{}, loseReply: true}
	standby := &poolServer{passwords: map[int64]string{}}
	d := &Device{clients: []*Client{poolTestClient(t, primary, a), poolTestClient(t, standby, b)}, info: sdk.DeviceInfo{Endpoints: []sdk.DeviceEndpoint{
		{ID: "a", DeviceKey: "ct1:" + testClusterID}, {ID: "b", DeviceKey: "ct1:" + testClusterID},
	}}}
	desired := []sdk.MiningPoolConfig{
		{Priority: 0, URL: "stratum+tcp://pool.example", WorkerName: "worker", Password: "secret"},
		{Priority: 1, URL: "stratum+tcp://backup.example", WorkerName: "worker", Password: "secret"},
	}
	require.Error(t, d.UpdateMiningPools(t.Context(), desired))
	require.Equal(t, 1, primary.writes)
	require.Zero(t, standby.writes)
	require.Zero(t, standby.reads)
	require.Zero(t, standby.modeReads)
	// A new admitted attempt probes fresh HA state. The fake pair shares the
	// persisted rows, just as the firmware controllers share their database.
	a.primary.Store(false)
	b.primary.Store(true)
	standby.pools, standby.passwords, standby.nextID = primary.pools, primary.passwords, primary.nextID
	primaryReads, primaryModes := primary.reads, primary.modeReads
	require.NoError(t, d.UpdateMiningPools(t.Context(), desired))
	require.Equal(t, 1, primary.writes)
	require.Equal(t, primaryReads, primary.reads)
	require.Equal(t, primaryModes, primary.modeReads)
	require.Equal(t, 2, standby.reads, "initial read and final verification stay on selected controller")
}

func TestWholeContainerPoolStackRejectsRuntimeOverrides(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(map[bool]string{true: "before writes", false: "after writes"}[before], func(t *testing.T) {
			stub := &poolServer{passwords: map[int64]string{}, runtimeOverride: before, runtimeAfterWrite: !before}
			err := poolTestDevice(t, stub).UpdateMiningPools(t.Context(), []sdk.MiningPoolConfig{{URL: "stratum+tcp://pool.example", WorkerName: "worker"}})
			require.ErrorContains(t, err, "runtime pools differ")
			if before {
				require.Zero(t, stub.writes)
				evidence := sdk.WriteEvidence(err)
				require.Equal(t, sdk.WriteNotDispatched, evidence.GetReason())
				require.Equal(t, string(sdk.WriteReasonStaleBinding), evidence.GetMetadata()["reason"])
			} else {
				require.Equal(t, 1, stub.writes)
				require.Nil(t, sdk.WriteEvidence(err), "runtime validation cannot hide a prior write")
			}
		})
	}
}
