package ct1

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	testJobID  = "0b9f0f4e-1c2d-4e5f-8a6b-7c8d9e0f1a2b"
	testFPA    = "1111111111111111111111111111111111111111111111111111111111111111"
	testFPB    = "2222222222222222222222222222222222222222222222222222222222222222"
	testDigest = "3333333333333333333333333333333333333333333333333333333333333333"
	// testEtcdClusterID is what update support reports: the etcd cluster that
	// holds rollout state. It deliberately differs from testClusterID, the HA
	// cluster UUID in GetHAStatus and the device key, as on real Frigate.
	testEtcdClusterID = "5191f350dcf21ca1"
)

func fingerprintFor(id string) string {
	if id == "b" {
		return testFPB
	}
	return testFPA
}

// updateFake is a scriptable Frigate ServerUpdateService. Its SystemService
// answers only GetHAStatus, counted like every other call, so tests can prove
// which operations probe HA identity.
type updateFake struct {
	mdk.UnimplementedServerUpdateServiceServer
	id     string
	peer   string
	server *grpc.Server
	port   int32

	mu          sync.Mutex
	actors      map[string][]string
	calls       map[string]int
	support     *mdk.GetServerUpdateSupportResponse
	errs        map[string]error
	manifest    *mdk.ServerUpdateManifest
	start       *mdk.StartServerUpdateRequest
	maintenance bool
	hang        map[string]bool
	haCluster   string
}

// updateHAFake is updateFake's SystemService.
type updateHAFake struct {
	mdk.UnimplementedSystemServiceServer
	f *updateFake
}

func (h updateHAFake) GetHAStatus(ctx context.Context, _ *mdk.GetHAStatusRequest) (*mdk.GetHAStatusResponse, error) {
	if err := h.f.enter(ctx, "GetHAStatus"); err != nil {
		return nil, err
	}
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	return &mdk.GetHAStatusResponse{Status: &mdk.HAStatus{
		Enabled: true, ReportingServerId: h.f.id, PeerServerId: h.f.peer, ClusterId: h.f.haCluster,
		IdentityState: mdk.HAIdentityState_HA_IDENTITY_STATE_CONFIRMED, InventoryReady: true,
	}}, nil
}

func (f *updateFake) setHACluster(cluster string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.haCluster = cluster
}

func startUpdateFake(t *testing.T, id, peer string) *updateFake {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	f := &updateFake{id: id, peer: peer, haCluster: testClusterID, port: port, server: grpc.NewServer(), actors: map[string][]string{}, calls: map[string]int{}, errs: map[string]error{}, hang: map[string]bool{}}
	f.support = &mdk.GetServerUpdateSupportResponse{
		InstanceId: id, PeerInstanceId: peer, RunningVersion: "1.0.0", Architecture: "arm64",
		InstallationType: mdk.ServerInstallationType_SERVER_INSTALLATION_TYPE_DEBIAN_PACKAGE, HelperAvailable: true,
		ClusterId: testEtcdClusterID, SpoolFreeBytes: 1 << 30, EtcdMembersTotal: 3, EtcdMembersHealthy: 3,
		TrustedReleaseKeyIds: []string{"sha256:abc"}, SelfCoordinationSupported: true, RemoteTriggerAllowed: true,
	}
	mdk.RegisterServerUpdateServiceServer(f.server, f)
	mdk.RegisterSystemServiceServer(f.server, updateHAFake{f: f})
	go func() { _ = f.server.Serve(listener) }()
	t.Cleanup(f.server.Stop)
	return f
}

func (f *updateFake) enter(ctx context.Context, method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[method]++
	md, _ := metadata.FromIncomingContext(ctx)
	f.actors[method] = append(f.actors[method], md.Get("x-frigate-actor")...)
	if f.maintenance && method == "GetServerUpdateSupport" {
		return status.Error(codes.Unavailable, "frigate is stopped") //nolint:wrapcheck // Test server returns a gRPC status.
	}
	if f.hang[method] {
		// Hold the request open until the client gives up, as a connected but
		// wedged controller would.
		f.mu.Unlock()
		<-ctx.Done()
		f.mu.Lock()
		return status.FromContextError(ctx.Err()).Err() //nolint:wrapcheck // Test server returns a gRPC status.
	}
	return f.errs[method]
}

func (f *updateFake) setHang(method string, hang bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hang[method] = hang
}

func (f *updateFake) setErr(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[method] = err
}

func (f *updateFake) count(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[method]
}

func (f *updateFake) actorsFor(method string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.actors[method]...)
}

func (f *updateFake) rollout() *mdk.ServerUpdateRollout {
	return &mdk.ServerUpdateRollout{
		JobId: testJobID, TargetVersion: "2.0.0", Phase: mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_INSTALLING,
		DriverInstanceId: "a", Cancellable: false, DisruptionStarted: true, Actor: "answered-by-" + f.id,
		UpdatedAt: timestamppb.Now(),
		Instances: []*mdk.ServerUpdateInstanceProgress{{InstanceId: "a", Phase: mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_INSTALLED, LastContact: timestamppb.Now()}, {InstanceId: "b", Phase: mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_STOPPED, ContactStale: true}},
	}
}

func (f *updateFake) GetServerUpdateSupport(ctx context.Context, _ *mdk.GetServerUpdateSupportRequest) (*mdk.GetServerUpdateSupportResponse, error) {
	if err := f.enter(ctx, "GetServerUpdateSupport"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.support, nil
}

func (f *updateFake) BeginServerUpdateStage(ctx context.Context, req *mdk.BeginServerUpdateStageRequest) (*mdk.BeginServerUpdateStageResponse, error) {
	if err := f.enter(ctx, "Begin"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.manifest = req.GetManifest()
	f.mu.Unlock()
	return &mdk.BeginServerUpdateStageResponse{JobId: req.GetManifest().GetJobId(), ManifestSha256: fingerprintFor(f.id), NextOffset: 512, SizeBytes: req.GetManifest().GetSizeBytes()}, nil
}

func (f *updateFake) AppendServerUpdateStage(ctx context.Context, req *mdk.AppendServerUpdateStageRequest) (*mdk.AppendServerUpdateStageResponse, error) {
	if err := f.enter(ctx, "Append"); err != nil {
		return nil, err
	}
	return &mdk.AppendServerUpdateStageResponse{JobId: req.GetJobId(), ManifestSha256: req.GetManifestSha256(), NextOffset: req.GetOffset() + int64(len(req.GetChunk())), SizeBytes: 4096}, nil
}

func (f *updateFake) CommitServerUpdateStage(ctx context.Context, req *mdk.CommitServerUpdateStageRequest) (*mdk.CommitServerUpdateStageResponse, error) {
	if err := f.enter(ctx, "Commit"); err != nil {
		return nil, err
	}
	return &mdk.CommitServerUpdateStageResponse{JobId: req.GetJobId(), BytesStaged: 4096, Sha256: testDigest}, nil
}

func (f *updateFake) AbortServerUpdateStage(ctx context.Context, _ *mdk.AbortServerUpdateStageRequest) (*mdk.AbortServerUpdateStageResponse, error) {
	if err := f.enter(ctx, "Abort"); err != nil {
		return nil, err
	}
	return &mdk.AbortServerUpdateStageResponse{}, nil
}

func (f *updateFake) StartServerUpdate(ctx context.Context, req *mdk.StartServerUpdateRequest) (*mdk.StartServerUpdateResponse, error) {
	if err := f.enter(ctx, "Start"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.start = req
	f.mu.Unlock()
	return &mdk.StartServerUpdateResponse{Rollout: f.rollout()}, nil
}

func (f *updateFake) GetServerUpdateStatus(ctx context.Context, _ *mdk.GetServerUpdateStatusRequest) (*mdk.GetServerUpdateStatusResponse, error) {
	if err := f.enter(ctx, "Status"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return &mdk.GetServerUpdateStatusResponse{Rollout: f.rollout(), ServedByMaintenance: f.maintenance}, nil
}

func (f *updateFake) RetryServerUpdate(ctx context.Context, _ *mdk.RetryServerUpdateRequest) (*mdk.RetryServerUpdateResponse, error) {
	if err := f.enter(ctx, "Retry"); err != nil {
		return nil, err
	}
	return &mdk.RetryServerUpdateResponse{Rollout: f.rollout()}, nil
}

func (f *updateFake) CancelServerUpdate(ctx context.Context, _ *mdk.CancelServerUpdateRequest) (*mdk.CancelServerUpdateResponse, error) {
	if err := f.enter(ctx, "Cancel"); err != nil {
		return nil, err
	}
	return &mdk.CancelServerUpdateResponse{Rollout: f.rollout()}, nil
}

func refusalErr(code codes.Code, reason string) error {
	return (&sdk.FrigateRefusal{Reason: reason, Message: "refused: " + reason, Code: code}).GRPCStatus().Err() //nolint:wrapcheck // Test server returns a gRPC status.
}

func updatePair(t *testing.T) (*Device, *updateFake, *updateFake) {
	t.Helper()
	return updatePairWithKey(t, "ct1:"+testClusterID)
}

func updatePairWithKey(t *testing.T, deviceKey string) (*Device, *updateFake, *updateFake) {
	t.Helper()
	a, b := startUpdateFake(t, "a", "b"), startUpdateFake(t, "b", "a")
	endpoint := func(f *updateFake) sdk.DeviceEndpoint {
		return sdk.DeviceEndpoint{ID: f.id, DeviceKey: deviceKey, MemberIDs: []string{"a", "b"}, Host: "127.0.0.1", Port: 8080, URLScheme: "http", RPCPort: f.port}
	}
	info := sdk.DeviceInfo{Host: "127.0.0.1", Port: 8080, URLScheme: "http", RPCPort: a.port, SerialNumber: "a", Endpoints: []sdk.DeviceEndpoint{endpoint(a), endpoint(b)}}
	d, err := NewDevice(t.Context(), "container", info, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close(t.Context()) })
	return d, a, b
}

// blackHoleListener accepts TCP connections and never speaks, so an RPC on it
// can only end in a deadline.
func blackHoleListener(t *testing.T) int32 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range conns {
			_ = conn.Close()
		}
	})
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)
	port, err := sdk.ParsePort(portText)
	require.NoError(t, err)
	return port
}

// holePair is a device whose first controller "a" is a black hole and whose
// second, "b", is a working fake.
func holePair(t *testing.T) (*Device, *updateFake) {
	t.Helper()
	b := startUpdateFake(t, "b", "a")
	hole := blackHoleListener(t)
	endpoint := func(id string, port int32) sdk.DeviceEndpoint {
		return sdk.DeviceEndpoint{ID: id, DeviceKey: "ct1:" + testClusterID, MemberIDs: []string{"a", "b"}, Host: "127.0.0.1", Port: 8080, URLScheme: "http", RPCPort: port}
	}
	info := sdk.DeviceInfo{Host: "127.0.0.1", Port: 8080, URLScheme: "http", RPCPort: hole, SerialNumber: "a", Endpoints: []sdk.DeviceEndpoint{endpoint("a", hole), endpoint("b", b.port)}}
	d, err := NewDevice(t.Context(), "container", info, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close(t.Context()) })
	return d, b
}

func shortUpdateTimeout(t *testing.T) {
	t.Helper()
	previous := frigateUpdateCallTimeout
	frigateUpdateCallTimeout = 400 * time.Millisecond
	t.Cleanup(func() { frigateUpdateCallTimeout = previous })
}

func waitReady(t *testing.T, c *Client) {
	t.Helper()
	c.conn.Connect()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for c.conn.GetState() != connectivity.Ready {
		require.True(t, c.conn.WaitForStateChange(ctx, c.conn.GetState()), "channel never became READY")
	}
}

func startRequestFor() sdk.FrigateStartRequest {
	return sdk.FrigateStartRequest{JobID: testJobID, TargetVersion: "2.0.0", ExpectedInstanceIDs: []string{"a", "b"}, IdempotencyKey: "k1", ExpectedManifestSHA256: map[string]string{"a": testFPA, "b": testFPB}, Actor: "fleet:alice@example.com"}
}

func TestFrigateUpdateCapabilityIsAdvertised(t *testing.T) {
	require.True(t, Capabilities()[sdk.CapabilityFrigateServerUpdate])
	var _ sdk.DeviceFrigateUpdater = (*Device)(nil)
}

func TestFrigateStageRoutesPerInstanceAndRewritesActor(t *testing.T) {
	d, a, b := updatePair(t)
	manifest := sdk.FrigateManifest{
		JobID: testJobID, InstanceID: "b", ExpectedInstanceIDs: []string{"a", "b"}, TargetVersion: "2.0.0", SHA256: testDigest, SizeBytes: 4096,
		Actor: "fleet:alice@example.com", Coordination: sdk.FrigateCoordinationSelf, ReleaseStatement: []byte("statement"), ReleaseSignature: []byte("signature"),
	}
	progress, err := d.FrigateStageBegin(t.Context(), "b", manifest)
	require.NoError(t, err)
	require.Equal(t, sdk.FrigateStageProgress{JobID: testJobID, ManifestSHA256: testFPB, NextOffset: 512, SizeBytes: 4096}, progress)
	require.Equal(t, 1, b.count("Begin"))
	require.Zero(t, a.count("Begin"))
	require.Equal(t, []string{"proto-fleet:alice@example.com"}, b.actorsFor("Begin"))
	wire := b.manifest
	require.Equal(t, mdk.ServerUpdateCoordination_SERVER_UPDATE_COORDINATION_SELF, wire.GetCoordination())
	require.Equal(t, "proto-fleet:alice@example.com", wire.GetActor())
	require.Equal(t, "b", wire.GetInstanceId())
	require.Equal(t, []byte("statement"), wire.GetReleaseStatement())
	require.Empty(t, wire.GetMaintenanceToken())
	require.Empty(t, wire.GetFlotillaAddress())
	require.Empty(t, wire.GetSiteToken())

	appended, err := d.FrigateStageAppend(t.Context(), "a", sdk.FrigateStageAppend{JobID: testJobID, ManifestSHA256: testFPA, Offset: 512, Chunk: make([]byte, 100), Actor: "fleet:alice@example.com"})
	require.NoError(t, err)
	require.EqualValues(t, 612, appended.NextOffset)
	require.Equal(t, 1, a.count("Append"))
	commit, err := d.FrigateStageCommit(t.Context(), "a", sdk.FrigateStageRef{JobID: testJobID, ManifestSHA256: testFPA, Actor: "fleet:alice@example.com"})
	require.NoError(t, err)
	require.Equal(t, sdk.FrigateStageCommit{JobID: testJobID, BytesStaged: 4096, SHA256: testDigest}, commit)
	require.NoError(t, d.FrigateStageAbort(t.Context(), "a", sdk.FrigateStageRef{JobID: testJobID, ManifestSHA256: testFPA}))
	require.Equal(t, []string{"proto-fleet"}, a.actorsFor("Abort"))
	require.Zero(t, b.count("Append")+b.count("Commit")+b.count("Abort"))
}

func TestFrigateStageRejectsUnknownOrMismatchedInstance(t *testing.T) {
	d, a, b := updatePair(t)
	_, err := d.FrigateStageBegin(t.Context(), "c", sdk.FrigateManifest{JobID: testJobID, InstanceID: "c"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = d.FrigateStageBegin(t.Context(), "a", sdk.FrigateManifest{JobID: testJobID, InstanceID: "b"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Zero(t, a.count("Begin")+b.count("Begin"))
}

func TestFrigateStageErrorsKeepRefusalReason(t *testing.T) {
	d, a, _ := updatePair(t)
	a.setErr("Begin", refusalErr(codes.FailedPrecondition, "UPDATE_STAGE_MISMATCH"))
	_, err := d.FrigateStageBegin(t.Context(), "a", sdk.FrigateManifest{JobID: testJobID, InstanceID: "a"})
	refusal := sdk.AsFrigateRefusal(err)
	require.NotNil(t, refusal)
	require.Equal(t, "UPDATE_STAGE_MISMATCH", refusal.Reason)
	require.Equal(t, codes.FailedPrecondition, refusal.Code)
	// A staging failure is final: the other controller is not asked.
	a.setErr("Append", status.Error(codes.Unavailable, "down"))
	_, err = d.FrigateStageAppend(t.Context(), "a", sdk.FrigateStageAppend{JobID: testJobID, ManifestSHA256: testFPA, Chunk: []byte{1}})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Nil(t, sdk.AsFrigateRefusal(err))
}

func TestFrigateSupportReportsPerInstanceErrors(t *testing.T) {
	d, a, b := updatePair(t)
	supports, err := d.FrigateUpdateSupport(t.Context())
	require.NoError(t, err)
	require.Len(t, supports, 2)
	require.Equal(t, "a", supports[0].InstanceID)
	require.Equal(t, "b", supports[0].PeerInstanceID)
	require.Equal(t, sdk.FrigateInstallationDebianPackage, supports[0].InstallationType)
	require.Equal(t, testEtcdClusterID, supports[0].ClusterID)
	require.True(t, supports[0].SelfCoordinationSupported && supports[0].RemoteTriggerAllowed)
	require.Equal(t, []string{"sha256:abc"}, supports[0].TrustedReleaseKeyIDs)
	require.Empty(t, supports[0].Err+supports[1].Err)

	b.setErr("GetServerUpdateSupport", status.Error(codes.Unavailable, "stopped"))
	supports, err = d.FrigateUpdateSupport(t.Context())
	require.NoError(t, err)
	require.Empty(t, supports[0].Err)
	require.Equal(t, "b", supports[1].InstanceID)
	require.Contains(t, supports[1].Err, "stopped")

	// A controller answering as the wrong instance is reported, not trusted.
	b.setErr("GetServerUpdateSupport", nil)
	b.mu.Lock()
	b.support = &mdk.GetServerUpdateSupportResponse{InstanceId: "a", PeerInstanceId: "b"}
	b.mu.Unlock()
	supports, err = d.FrigateUpdateSupport(t.Context())
	require.NoError(t, err)
	require.Contains(t, supports[1].Err, `reported instance "a"`)
	require.Equal(t, "b", supports[1].InstanceID)
	require.Equal(t, []string{"proto-fleet"}, a.actorsFor("GetServerUpdateSupport")[:1])
}

func TestFrigateStartValidatesThePairBeforeStarting(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*updateFake, *updateFake)
		expect func(*testing.T, error)
	}{
		{"remote trigger disabled", func(a, _ *updateFake) { a.support.RemoteTriggerAllowed = false }, func(t *testing.T, err error) {
			require.Equal(t, "UPDATE_REMOTE_TRIGGER_DISABLED", sdk.AsFrigateRefusal(err).Reason)
		}},
		{"self coordination unsupported", func(_, b *updateFake) { b.support.SelfCoordinationSupported = false }, func(t *testing.T, err error) {
			require.Equal(t, "UPDATE_SELF_COORDINATION_UNSUPPORTED", sdk.AsFrigateRefusal(err).Reason)
		}},
		{"etcd cluster mismatch", func(_, b *updateFake) { b.support.ClusterId = "0000000000000001" }, func(t *testing.T, err error) {
			require.Equal(t, frigateReasonPairMismatch, sdk.AsFrigateRefusal(err).Reason)
		}},
		{"HA cluster is not this device's", func(_, b *updateFake) { b.setHACluster("11111111-1111-4111-8111-111111111111") }, func(t *testing.T, err error) {
			refusal := sdk.AsFrigateRefusal(err)
			require.NotNil(t, refusal, "%v", err)
			require.Equal(t, frigateReasonPairMismatch, refusal.Reason)
			require.Contains(t, refusal.Message, "controller b is not this device's CT1 cluster")
		}},
		{"HA identity unreadable", func(_, b *updateFake) { b.setErr("GetHAStatus", status.Error(codes.Unavailable, "down")) }, func(t *testing.T, err error) {
			require.Nil(t, sdk.AsFrigateRefusal(err), "a transport failure is not a pair mismatch")
			require.Equal(t, codes.Unavailable, status.Code(err))
			require.Contains(t, err.Error(), "cannot verify the HA identity of controller b")
		}},
		{"missing cluster", func(a, _ *updateFake) { a.support.ClusterId = "" }, func(t *testing.T, err error) {
			require.Equal(t, frigateReasonPairMismatch, sdk.AsFrigateRefusal(err).Reason)
		}},
		{"controller unreachable", func(_, b *updateFake) { b.setErr("GetServerUpdateSupport", status.Error(codes.Unavailable, "down")) }, func(t *testing.T, err error) {
			require.Equal(t, codes.Unavailable, status.Code(err))
			require.Contains(t, err.Error(), "cannot be queried")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, a, b := updatePair(t)
			tc.mutate(a, b)
			_, err := d.FrigateUpdateStart(t.Context(), startRequestFor())
			require.Error(t, err)
			tc.expect(t, err)
			require.Zero(t, a.count("Start")+b.count("Start"))
		})
	}
	t.Run("expected instances differ", func(t *testing.T) {
		d, a, b := updatePair(t)
		req := startRequestFor()
		req.ExpectedInstanceIDs = []string{"a", "z"}
		req.ExpectedManifestSHA256 = map[string]string{"a": testFPA}
		_, err := d.FrigateUpdateStart(t.Context(), req)
		require.Equal(t, frigateReasonPairMismatch, sdk.AsFrigateRefusal(err).Reason)
		require.Zero(t, a.count("Start")+b.count("Start"))
	})
}

func TestFrigateStartSendsActorAndRequest(t *testing.T) {
	d, a, b := updatePair(t)
	req := startRequestFor()
	req.ExpectedInstanceIDs = []string{"b", "a"} // order must not matter for pair validation
	req.StageOnly = true
	rollout, err := d.FrigateUpdateStart(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, sdk.FrigatePhaseInstalling, rollout.Phase)
	require.True(t, rollout.DisruptionStarted)
	require.Len(t, rollout.Instances, 2)
	require.Equal(t, sdk.FrigateAgentInstalled, rollout.Instances[0].Phase)
	require.Equal(t, sdk.FrigateAgentStopped, rollout.Instances[1].Phase)
	require.True(t, rollout.Instances[1].ContactStale)
	require.False(t, rollout.UpdatedAt.IsZero())
	require.Equal(t, 1, a.count("Start"))
	require.Zero(t, b.count("Start"))
	// A real pair reports its etcd cluster in update support and its HA cluster
	// UUID in GetHAStatus; both controllers' HA identity is checked once.
	require.NotEqual(t, testClusterID, a.support.GetClusterId())
	require.Equal(t, 1, a.count("GetHAStatus"))
	require.Equal(t, 1, b.count("GetHAStatus"))
	require.Equal(t, []string{"proto-fleet:alice@example.com"}, a.actorsFor("Start"))
	require.True(t, a.start.GetStageOnly())
	require.Equal(t, "k1", a.start.GetIdempotencyKey())
	require.Equal(t, map[string]string{"a": testFPA, "b": testFPB}, a.start.GetExpectedManifestSha256())
}

func TestFrigateStartFallsThroughOnlyOnUnavailable(t *testing.T) {
	d, a, b := updatePair(t)
	a.setErr("Start", status.Error(codes.Unavailable, "down"))
	_, err := d.FrigateUpdateStart(t.Context(), startRequestFor())
	require.NoError(t, err)
	require.Equal(t, 1, a.count("Start"))
	require.Equal(t, 1, b.count("Start"))

	d, a, b = updatePair(t)
	a.setErr("Start", refusalErr(codes.FailedPrecondition, "UPDATE_ACTIVE_JOB"))
	_, err = d.FrigateUpdateStart(t.Context(), startRequestFor())
	require.Equal(t, "UPDATE_ACTIVE_JOB", sdk.AsFrigateRefusal(err).Reason)
	require.Zero(t, b.count("Start"))

	d, a, b = updatePair(t)
	a.setErr("Start", status.Error(codes.Internal, "boom"))
	_, err = d.FrigateUpdateStart(t.Context(), startRequestFor())
	require.Equal(t, codes.Internal, status.Code(err))
	require.Zero(t, b.count("Start"))
}

func TestFrigateStatusToleratesUnavailableAndPrefersLastGood(t *testing.T) {
	d, a, b := updatePair(t)
	a.setErr("Status", status.Error(codes.Unavailable, "stopped"))
	got, err := d.FrigateUpdateStatus(t.Context(), testJobID)
	require.NoError(t, err)
	require.Equal(t, "b", got.AnsweredBy)
	require.Equal(t, "answered-by-b", got.Rollout.Actor)
	require.False(t, got.ServedByMaintenance)
	require.Equal(t, 1, a.count("Status"))

	// The answering controller is asked first next time.
	b.mu.Lock()
	b.maintenance = true
	b.mu.Unlock()
	got, err = d.FrigateUpdateStatus(t.Context(), testJobID)
	require.NoError(t, err)
	require.Equal(t, "b", got.AnsweredBy)
	require.True(t, got.ServedByMaintenance)
	require.Equal(t, 1, a.count("Status"))
	require.Equal(t, 2, b.count("Status"))
	require.Equal(t, []string{"proto-fleet", "proto-fleet"}, b.actorsFor("Status"))

	// Refusals are final, even from the first controller.
	b.setErr("Status", refusalErr(codes.NotFound, "UPDATE_NOT_FOUND"))
	_, err = d.FrigateUpdateStatus(t.Context(), "missing")
	require.Equal(t, "UPDATE_NOT_FOUND", sdk.AsFrigateRefusal(err).Reason)
	require.Equal(t, 1, a.count("Status"))

	// Both stopped: Unavailable with every instance named.
	b.setErr("Status", status.Error(codes.Unavailable, "stopped too"))
	_, err = d.FrigateUpdateStatus(t.Context(), testJobID)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Contains(t, err.Error(), "a: stopped")
	require.Contains(t, err.Error(), "b: stopped too")
}

func TestFrigateStatusDoesNotNeedHAProbes(t *testing.T) {
	// Status, retry and cancel must work while Frigate is stopped, so none of
	// them may probe HA identity.
	d, a, b := updatePair(t)
	a.setErr("GetHAStatus", status.Error(codes.Unavailable, "stopped"))
	b.setErr("GetHAStatus", status.Error(codes.Unavailable, "stopped"))
	a.setErr("GetServerUpdateSupport", status.Error(codes.Unavailable, "stopped"))
	b.setErr("GetServerUpdateSupport", status.Error(codes.Unavailable, "stopped"))
	got, err := d.FrigateUpdateStatus(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, "a", got.AnsweredBy)
	_, err = d.FrigateUpdateRetry(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "r", Actor: "fleet:bob"})
	require.NoError(t, err)
	_, err = d.FrigateUpdateCancel(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "c", Actor: "fleet:bob"})
	require.NoError(t, err)
	require.Equal(t, []string{"proto-fleet:bob"}, a.actorsFor("Retry"))
	require.Equal(t, []string{"proto-fleet:bob"}, a.actorsFor("Cancel"))
	require.Zero(t, a.count("GetHAStatus")+b.count("GetHAStatus"))
}

func TestFrigateRetryAndCancelFallThroughOnUnavailableOnly(t *testing.T) {
	d, a, b := updatePair(t)
	a.setErr("Retry", status.Error(codes.Unavailable, "stopped"))
	a.setErr("Cancel", refusalErr(codes.FailedPrecondition, "UPDATE_NOT_CANCELLABLE"))
	_, err := d.FrigateUpdateRetry(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "r"})
	require.NoError(t, err)
	require.Equal(t, 1, b.count("Retry"))
	// The sticky controller is now b, so Cancel asks b first and succeeds.
	_, err = d.FrigateUpdateCancel(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "c"})
	require.NoError(t, err)
	require.Zero(t, a.count("Cancel"))
	b.setErr("Cancel", refusalErr(codes.FailedPrecondition, "UPDATE_NOT_CANCELLABLE"))
	_, err = d.FrigateUpdateCancel(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "c2"})
	require.Equal(t, "UPDATE_NOT_CANCELLABLE", sdk.AsFrigateRefusal(err).Reason)
	require.Zero(t, a.count("Cancel"))
}

func TestOutgoingActorReplacesExistingValue(t *testing.T) {
	ctx := metadata.AppendToOutgoingContext(t.Context(), "x-frigate-actor", "stale", "other", "kept")
	out := outgoingActor(withActor(ctx, "proto-fleet:carol"))
	md, _ := metadata.FromOutgoingContext(out)
	require.Equal(t, []string{"proto-fleet:carol"}, md.Get("x-frigate-actor"))
	require.Equal(t, []string{"kept"}, md.Get("other"))
	md, _ = metadata.FromOutgoingContext(outgoingActor(t.Context()))
	require.Equal(t, []string{"proto-fleet"}, md.Get("x-frigate-actor"))
}

func TestFrigateSingleNodeDeviceUsesDiscoveredInstanceID(t *testing.T) {
	a := startUpdateFake(t, "solo", "")
	a.support.PeerInstanceId = ""
	a.support.ClusterId = ""
	info := sdk.DeviceInfo{Host: "127.0.0.1", Port: 8080, URLScheme: "http", RPCPort: a.port, SerialNumber: "solo"}
	c, err := NewClient(info, sdk.UsernamePassword{})
	require.NoError(t, err)
	c.memberID = "solo"
	d := &Device{id: "x", info: info, clients: []*Client{c}}
	t.Cleanup(func() { _ = d.Close(t.Context()) })
	req := startRequestFor()
	req.ExpectedInstanceIDs = []string{"solo"}
	req.ExpectedManifestSHA256 = map[string]string{"solo": testFPA}
	_, err = d.FrigateUpdateStart(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, 1, a.count("Start"))
}

func TestFrigateRefusalWithUnavailableCodeIsFinalAndIntact(t *testing.T) {
	const reason = "UPDATE_ETCD_UNHEALTHY"
	calls := map[string]func(*Device) error{
		"Start": func(d *Device) error { _, err := d.FrigateUpdateStart(t.Context(), startRequestFor()); return err },
		"Status": func(d *Device) error {
			_, err := d.FrigateUpdateStatus(t.Context(), testJobID)
			return err
		},
		"Retry": func(d *Device) error {
			_, err := d.FrigateUpdateRetry(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "r"})
			return err
		},
		"Cancel": func(d *Device) error {
			_, err := d.FrigateUpdateCancel(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "c"})
			return err
		},
	}
	for method, call := range calls {
		t.Run(method, func(t *testing.T) {
			d, a, b := updatePair(t)
			a.setErr(method, refusalErr(codes.Unavailable, reason))
			err := call(d)
			refusal := sdk.AsFrigateRefusal(err)
			require.NotNil(t, refusal, "%v", err)
			require.Equal(t, reason, refusal.Reason)
			require.Equal(t, codes.Unavailable, refusal.Code)
			require.Equal(t, "refused: "+reason, refusal.Message)
			require.Equal(t, 1, a.count(method))
			require.Zero(t, b.count(method), "a refusal must not be repeated on the peer")
		})
	}
	t.Run("Start support refusal", func(t *testing.T) {
		d, a, b := updatePair(t)
		b.setErr("GetServerUpdateSupport", refusalErr(codes.Unavailable, "UPDATE_HELPER_UNAVAILABLE"))
		_, err := d.FrigateUpdateStart(t.Context(), startRequestFor())
		refusal := sdk.AsFrigateRefusal(err)
		require.NotNil(t, refusal, "%v", err)
		require.Equal(t, "UPDATE_HELPER_UNAVAILABLE", refusal.Reason)
		require.Equal(t, codes.Unavailable, refusal.Code)
		require.Zero(t, a.count("Start")+b.count("Start"))
	})
}

func TestFrigateSupportErrReasonComesFromTheRefusal(t *testing.T) {
	d, _, b := updatePair(t)
	b.setErr("GetServerUpdateSupport", refusalErr(codes.Unavailable, "UPDATE_HELPER_UNAVAILABLE"))
	supports, err := d.FrigateUpdateSupport(t.Context())
	require.NoError(t, err)
	require.Empty(t, supports[0].ErrReason)
	require.Equal(t, "UPDATE_HELPER_UNAVAILABLE", supports[1].ErrReason)
	require.Equal(t, "refused: UPDATE_HELPER_UNAVAILABLE", supports[1].Err)
	// A plain transport failure carries no reason.
	b.setErr("GetServerUpdateSupport", status.Error(codes.Unavailable, "down"))
	supports, err = d.FrigateUpdateSupport(t.Context())
	require.NoError(t, err)
	require.Empty(t, supports[1].ErrReason)
	require.NotEmpty(t, supports[1].Err)
}

func TestFrigateAllTransportFailuresReturnTheLastCodeWithDetail(t *testing.T) {
	d, a, b := updatePair(t)
	a.setErr("Status", status.Error(codes.Unavailable, "a stopped"))
	b.setErr("Status", status.Error(codes.DeadlineExceeded, "b slow"))
	_, err := d.FrigateUpdateStatus(t.Context(), testJobID)
	require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	require.Nil(t, sdk.AsFrigateRefusal(err))
	require.Contains(t, err.Error(), "a: a stopped")
	require.Contains(t, err.Error(), "b: b slow")

	a.setErr("Retry", status.Error(codes.Unavailable, "a stopped"))
	b.setErr("Retry", status.Error(codes.Unavailable, "b stopped"))
	_, err = d.FrigateUpdateRetry(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "r"})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Contains(t, err.Error(), "a: a stopped")
	require.Contains(t, err.Error(), "b: b stopped")
}

func TestFrigateMutationsPreferReadyControllersOverABlackHole(t *testing.T) {
	shortUpdateTimeout(t)
	d, b := holePair(t)
	waitReady(t, d.clients[1])
	started := time.Now()
	_, err := d.FrigateUpdateRetry(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "r"})
	require.NoError(t, err)
	_, err = d.FrigateUpdateCancel(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "c"})
	require.NoError(t, err)
	got, err := d.FrigateUpdateStatus(t.Context(), testJobID)
	require.NoError(t, err)
	require.Equal(t, "b", got.AnsweredBy)
	require.Less(t, time.Since(started), frigateUpdateCallTimeout, "a READY controller must be tried before a wedged one")
	require.Equal(t, 1, b.count("Retry"))
	require.Equal(t, 1, b.count("Cancel"))
}

func TestFrigateMutationsFallThroughFromAControllerThatNeverConnected(t *testing.T) {
	shortUpdateTimeout(t)
	for name, call := range map[string]func(*Device) error{
		"Retry": func(d *Device) error {
			_, err := d.FrigateUpdateRetry(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "r"})
			return err
		},
		"Cancel": func(d *Device) error {
			_, err := d.FrigateUpdateCancel(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "c"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			d, b := holePair(t)
			// Nothing has connected yet, so the black hole is tried first, its
			// deadline proves nothing was sent, and the peer answers.
			require.NotEqual(t, connectivity.Ready, d.clients[0].conn.GetState())
			require.NoError(t, call(d))
			require.Equal(t, 1, b.count(name))
		})
	}
}

func TestFrigateDeadlineOnAReadyControllerIsFinalForMutationsOnly(t *testing.T) {
	shortUpdateTimeout(t)
	for _, method := range []string{"Start", "Retry", "Cancel"} {
		t.Run(method, func(t *testing.T) {
			d, a, b := updatePair(t)
			waitReady(t, d.clients[0])
			waitReady(t, d.clients[1])
			a.setHang(method, true)
			var err error
			switch method {
			case "Start":
				_, err = d.FrigateUpdateStart(t.Context(), startRequestFor())
			case "Retry":
				_, err = d.FrigateUpdateRetry(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "r"})
			default:
				_, err = d.FrigateUpdateCancel(t.Context(), sdk.FrigateAction{JobID: testJobID, IdempotencyKey: "c"})
			}
			require.Equal(t, codes.DeadlineExceeded, status.Code(err), "%v", err)
			require.Equal(t, 1, a.count(method))
			require.Zero(t, b.count(method), "an ambiguous mutation must be reconciled through status, not repeated")
		})
	}
	t.Run("Status", func(t *testing.T) {
		d, a, b := updatePair(t)
		waitReady(t, d.clients[0])
		a.setHang("Status", true)
		got, err := d.FrigateUpdateStatus(t.Context(), testJobID)
		require.NoError(t, err)
		require.Equal(t, "b", got.AnsweredBy)
		require.Equal(t, 1, a.count("Status"))
		require.Equal(t, 1, b.count("Status"))
	})
}

func TestFrigateStartRefusesUnreachableControllerWithItsCode(t *testing.T) {
	shortUpdateTimeout(t)
	d, b := holePair(t)
	_, err := d.FrigateUpdateStart(t.Context(), startRequestFor())
	require.Equal(t, codes.DeadlineExceeded, status.Code(err), "%v", err)
	require.Contains(t, err.Error(), "a:")
	require.Zero(t, b.count("Start"))
}

func TestFrigatePairClusterDoesNotFailOpen(t *testing.T) {
	const other = "11111111-1111-4111-8111-111111111111"
	tests := []struct {
		name    string
		a, b    string
		refused bool
	}{
		{"both empty", "", "", true},
		{"one empty", testEtcdClusterID, "", true},
		{"differ", testEtcdClusterID, other, true},
		{"shared", testEtcdClusterID, testEtcdClusterID, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, a, b := updatePairWithKey(t, "legacy-device-key")
			a.support.ClusterId, b.support.ClusterId = tc.a, tc.b
			_, err := d.FrigateUpdateStart(t.Context(), startRequestFor())
			if !tc.refused {
				require.NoError(t, err)
				return
			}
			refusal := sdk.AsFrigateRefusal(err)
			require.NotNil(t, refusal, "%v", err)
			require.Equal(t, frigateReasonPairMismatch, refusal.Reason)
			require.Zero(t, a.count("Start")+b.count("Start"))
		})
	}
}

func TestFrigateMutationsRequireAnIdempotencyKeyAndUUIDJob(t *testing.T) {
	d, a, b := updatePair(t)
	req := startRequestFor()
	req.IdempotencyKey = ""
	_, err := d.FrigateUpdateStart(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = d.FrigateUpdateRetry(t.Context(), sdk.FrigateAction{JobID: testJobID})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = d.FrigateUpdateCancel(t.Context(), sdk.FrigateAction{JobID: "job-1", IdempotencyKey: "c"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	for _, f := range []*updateFake{a, b} {
		require.Zero(t, f.count("Start")+f.count("Retry")+f.count("Cancel")+f.count("GetServerUpdateSupport"))
	}
}

func TestFrigateBeginForAnotherInstanceSendsNothing(t *testing.T) {
	d, a, b := updatePair(t)
	m := sdk.FrigateManifest{
		JobID: testJobID, InstanceID: "b", ExpectedInstanceIDs: []string{"a", "b"}, TargetVersion: "2.0.0", SHA256: testDigest, SizeBytes: 4096,
		Coordination: sdk.FrigateCoordinationSelf, ReleaseStatement: []byte("statement"), ReleaseSignature: []byte("signature"),
	}
	// The device refuses a manifest addressed to a different controller before
	// any request leaves, so neither fake sees a Begin.
	_, err := d.FrigateStageBegin(t.Context(), "a", m)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Zero(t, a.count("Begin")+b.count("Begin"))
}
