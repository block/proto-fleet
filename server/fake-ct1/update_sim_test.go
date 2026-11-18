package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The HA cluster UUID and the etcd cluster ID are different identifiers, as on
// real Frigate.
const (
	testHAClusterID   = "7900e1a3-5be9-4a6b-b640-eaf3d959333b"
	testEtcdClusterID = "5191f350dcf21ca1"
)

const testTargetVersion = "9.9.9"

// updateHarness runs both pair members on real gRPC listeners.
type updateHarness struct {
	t       *testing.T
	sim     *updateSim
	key     *ecdsa.PrivateKey
	states  [2]*ContainerState
	conns   [2]*grpc.ClientConn
	servers [2]*grpc.Server
}

func newUpdateHarness(t *testing.T, mutate func(*updateSimConfig)) *updateHarness {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := updateSimConfig{Primary: "ct1-a", EtcdClusterID: testEtcdClusterID, StepDelay: 25 * time.Millisecond, TrustedKeys: []*ecdsa.PublicKey{&key.PublicKey}}
	if mutate != nil {
		mutate(&cfg)
	}
	h := &updateHarness{t: t, key: key}
	for i, id := range []string{"ct1-a", "ct1-b"} {
		h.states[i] = NewContainerState(ContainerConfig{InstanceID: "pair", IdentityNamespace: "pair", ServerID: id, Hostname: id, Version: "1.0.0", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 1, AsicsPerBoard: 4, Seed: 1})
	}
	h.sim = newHAPair(h.states[0], h.states[1], haPairSettings{Update: cfg, ClusterID: testHAClusterID})
	t.Cleanup(h.sim.close)
	for i, state := range h.states {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		server := newGRPCServer(state)
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(server.Stop)
		conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		h.servers[i], h.conns[i] = server, conn
	}
	return h
}

func (h *updateHarness) update(i int) mdk.ServerUpdateServiceClient {
	return mdk.NewServerUpdateServiceClient(h.conns[i])
}

// statement signs a release statement for pkg with the harness key.
func (h *updateHarness) statement(pkg []byte, version string) (statement, signature []byte) {
	h.t.Helper()
	sum := sha256.Sum256(pkg)
	statement, err := json.Marshal(releaseStatement{Schema: releaseStatementSchema, Package: "frigate", Version: version, Architecture: "arm64", Repository: "example/frigate", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(pkg)), IssuedAt: "2026-01-01T00:00:00Z"})
	if err != nil {
		h.t.Fatal(err)
	}
	digest := sha256.Sum256(statement)
	signature, err = ecdsa.SignASN1(rand.Reader, h.key, digest[:])
	if err != nil {
		h.t.Fatal(err)
	}
	return statement, signature
}

func (h *updateHarness) manifest(job, instance string, pkg []byte) *mdk.ServerUpdateManifest {
	statement, signature := h.statement(pkg, testTargetVersion)
	sum := sha256.Sum256(pkg)
	return &mdk.ServerUpdateManifest{
		JobId: job, InstanceId: instance, ExpectedInstanceIds: []string{"ct1-a", "ct1-b"}, TargetVersion: testTargetVersion,
		Sha256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(pkg)), Actor: "fleet:tester",
		Coordination: mdk.ServerUpdateCoordination_SERVER_UPDATE_COORDINATION_SELF, ReleaseStatement: statement, ReleaseSignature: signature,
	}
}

// stage sends pkg to one instance in chunks of the given size. Begin goes to
// the instance's own connection; the byte calls go through the other member's
// connection with x-frigate-server-id to prove header routing.
func (h *updateHarness) stage(job string, instanceIndex int, pkg []byte, chunk int) string {
	h.t.Helper()
	ctx := context.Background()
	instance := h.states[instanceIndex].ServerID
	begin, err := h.update(instanceIndex).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: h.manifest(job, instance, pkg)})
	if err != nil {
		h.t.Fatal(err)
	}
	client := h.update(1 - instanceIndex)
	ctx = metadata.AppendToOutgoingContext(ctx, serverIDHeader, instance)
	for off := int(begin.GetNextOffset()); off < len(pkg); off += chunk {
		end := min(off+chunk, len(pkg))
		if _, err := client.AppendServerUpdateStage(ctx, &mdk.AppendServerUpdateStageRequest{JobId: job, ManifestSha256: begin.GetManifestSha256(), Offset: int64(off), Chunk: pkg[off:end]}); err != nil {
			h.t.Fatal(err)
		}
	}
	if _, err := client.CommitServerUpdateStage(ctx, &mdk.CommitServerUpdateStageRequest{JobId: job, ManifestSha256: begin.GetManifestSha256()}); err != nil {
		h.t.Fatal(err)
	}
	return begin.GetManifestSha256()
}

func (h *updateHarness) stageBoth(job string, pkg []byte) map[string]string {
	fingerprints := map[string]string{}
	for i := range h.states {
		fingerprints[h.states[i].ServerID] = h.stage(job, i, pkg, 700)
	}
	return fingerprints
}

func reasonOf(err error) string {
	st, ok := status.FromError(err)
	if !ok {
		return ""
	}
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.GetDomain() == frigateRefusalDomain {
			return info.GetReason()
		}
	}
	return ""
}

func requireRefusal(t *testing.T, err error, code codes.Code, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s/%s, got success", code, reason)
	}
	if status.Code(err) != code || reasonOf(err) != reason {
		t.Fatalf("expected %s/%s, got %s/%s: %v", code, reason, status.Code(err), reasonOf(err), err)
	}
}

func startRequest(job string, fingerprints map[string]string) *mdk.StartServerUpdateRequest {
	return &mdk.StartServerUpdateRequest{JobId: job, TargetVersion: testTargetVersion, ExpectedInstanceIds: []string{"ct1-a", "ct1-b"}, IdempotencyKey: "key-" + job, ExpectedManifestSha256: fingerprints}
}

func (h *updateHarness) waitPhase(job string, want mdk.ServerUpdateRolloutPhase) *mdk.GetServerUpdateStatusResponse {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := h.update(0).GetServerUpdateStatus(context.Background(), &mdk.GetServerUpdateStatusRequest{JobId: job})
		if err == nil && resp.GetRollout().GetPhase() == want {
			return resp
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("rollout never reached %s: %v %v", want, resp, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func testPackage(size int) []byte {
	pkg := make([]byte, size)
	for i := range pkg {
		pkg[i] = byte(i * 7)
	}
	return pkg
}

func TestHAPairIdentityAndRoles(t *testing.T) {
	h := newUpdateHarness(t, nil)
	ctx := context.Background()
	var clusters []string
	for i, role := range []mdk.HARole{mdk.HARole_HA_ROLE_PRIMARY, mdk.HARole_HA_ROLE_STANDBY} {
		system := mdk.NewSystemServiceClient(h.conns[i])
		ha, err := system.GetHAStatus(ctx, &mdk.GetHAStatusRequest{})
		if err != nil {
			t.Fatal(err)
		}
		report := ha.GetStatus()
		clusters = append(clusters, report.GetClusterId())
		if report.GetReportingServerId() != h.states[i].ServerID || report.GetPeerServerId() != h.states[1-i].ServerID || len(report.GetMembers()) != 2 {
			t.Fatalf("unexpected HA report: %v", report)
		}
		current, err := system.GetSystemStatus(ctx, &mdk.GetSystemStatusRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if current.GetStatus().GetHaRole() != role || current.GetStatus().GetContainerWritePrimary() != (role == mdk.HARole_HA_ROLE_PRIMARY) {
			t.Fatalf("instance %d role = %v", i, current.GetStatus())
		}
	}
	if clusters[0] != clusters[1] || clusters[0] == "" {
		t.Fatalf("cluster IDs differ: %v", clusters)
	}
	// A failover is visible on both members.
	h.sim.setPrimary("ct1-b")
	current, err := mdk.NewSystemServiceClient(h.conns[1]).GetSystemStatus(ctx, &mdk.GetSystemStatusRequest{})
	if err != nil || current.GetStatus().GetHaRole() != mdk.HARole_HA_ROLE_PRIMARY {
		t.Fatalf("failover not reflected: %v %v", current, err)
	}
}

func TestSingleNodeHasNoHAStatus(t *testing.T) {
	state := NewContainerState(ContainerConfig{ServerID: "solo", Tanks: 1, ModulesPerTank: 1, SlotsPerModule: 1, AsicsPerBoard: 4, Seed: 1})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newGRPCServer(state)
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := mdk.NewSystemServiceClient(conn).GetHAStatus(context.Background(), &mdk.GetHAStatusRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("single node must not report HA: %v", err)
	}
	if _, err := mdk.NewServerUpdateServiceClient(conn).GetServerUpdateSupport(context.Background(), &mdk.GetServerUpdateSupportRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("single node must not serve updates: %v", err)
	}
}

func TestStagingIsResumableWithExactOffsets(t *testing.T) {
	h := newUpdateHarness(t, nil)
	ctx := context.Background()
	pkg := testPackage(3000)
	manifest := h.manifest("job-resume", "ct1-a", pkg)
	// A controller refuses a manifest addressed to its peer, staging nothing.
	_, err := h.update(1).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: manifest})
	requireRefusal(t, err, codes.InvalidArgument, "UPDATE_MANIFEST_INVALID")
	if _, err := h.update(1).AppendServerUpdateStage(metadata.AppendToOutgoingContext(ctx, serverIDHeader, "ct1-a"), &mdk.AppendServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: "x", Chunk: pkg[:10]}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("nothing should be staged on the peer: %v", err)
	}
	// The x-frigate-server-id header addresses the peer instance explicitly.
	begin, err := h.update(1).BeginServerUpdateStage(metadata.AppendToOutgoingContext(ctx, serverIDHeader, "ct1-a"), &mdk.BeginServerUpdateStageRequest{Manifest: manifest})
	if err != nil || begin.GetNextOffset() != 0 || begin.GetSizeBytes() != 3000 {
		t.Fatalf("begin = %v, %v", begin, err)
	}
	fp := begin.GetManifestSha256()
	scoped := metadata.AppendToOutgoingContext(ctx, serverIDHeader, "ct1-a")
	if _, err := h.update(0).AppendServerUpdateStage(ctx, &mdk.AppendServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: fp, Offset: 0, Chunk: pkg[:1000]}); err != nil {
		t.Fatal(err)
	}
	// A gap, a rewind with different bytes, and an oversize chunk are refused.
	if _, err := h.update(0).AppendServerUpdateStage(scoped, &mdk.AppendServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: fp, Offset: 2000, Chunk: pkg[2000:2100]}); status.Code(err) != codes.InvalidArgument || !strings.Contains(err.Error(), "next offset 1000") {
		t.Fatalf("gap = %v", err)
	}
	if _, err := h.update(0).AppendServerUpdateStage(scoped, &mdk.AppendServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: fp, Offset: 0, Chunk: pkg[1:101]}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("rewrite = %v", err)
	}
	if _, err := h.update(0).AppendServerUpdateStage(scoped, &mdk.AppendServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: fp, Offset: 1000, Chunk: make([]byte, maxStageChunkBytes+1)}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversize = %v", err)
	}
	// A repeated identical chunk (lost reply) is acknowledged without advancing.
	again, err := h.update(0).AppendServerUpdateStage(scoped, &mdk.AppendServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: fp, Offset: 0, Chunk: pkg[:1000]})
	if err != nil || again.GetNextOffset() != 1000 {
		t.Fatalf("repeat = %v, %v", again, err)
	}
	// Resuming after a client restart reports the acknowledged offset.
	resumed, err := h.update(0).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: manifest})
	if err != nil || resumed.GetNextOffset() != 1000 || resumed.GetManifestSha256() != fp {
		t.Fatalf("resume = %v, %v", resumed, err)
	}
	// An early commit and a wrong fingerprint are refused.
	if _, err := h.update(0).CommitServerUpdateStage(scoped, &mdk.CommitServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: fp}); err == nil {
		t.Fatal("commit of a partial package succeeded")
	}
	if _, err := h.update(0).AppendServerUpdateStage(scoped, &mdk.AppendServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: "bad", Offset: 1000, Chunk: pkg[1000:2000]}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("wrong fingerprint = %v", err)
	}
	for off := 1000; off < len(pkg); off += 1000 {
		if _, err := h.update(0).AppendServerUpdateStage(scoped, &mdk.AppendServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: fp, Offset: int64(off), Chunk: pkg[off:min(off+1000, len(pkg))]}); err != nil {
			t.Fatal(err)
		}
	}
	commit, err := h.update(0).CommitServerUpdateStage(scoped, &mdk.CommitServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: fp})
	sum := sha256.Sum256(pkg)
	if err != nil || commit.GetBytesStaged() != 3000 || commit.GetSha256() != hex.EncodeToString(sum[:]) {
		t.Fatalf("commit = %v, %v", commit, err)
	}
	// A different manifest cannot take over the job.
	other := h.manifest("job-resume", "ct1-a", pkg)
	other.Actor = "someone else"
	_, err = h.update(0).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: other})
	requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_STAGE_MISMATCH")
	// A wrong fingerprint cannot withdraw it, but a committed stage whose
	// rollout never started is withdrawn, and repeating that is a no-op.
	if _, err := h.update(0).AbortServerUpdateStage(scoped, &mdk.AbortServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: "bad"}); err == nil {
		t.Fatal("withdrew a committed stage with the wrong fingerprint")
	}
	for range 2 {
		if _, err := h.update(0).AbortServerUpdateStage(scoped, &mdk.AbortServerUpdateStageRequest{JobId: "job-resume", ManifestSha256: fp}); err != nil {
			t.Fatalf("withdraw committed stage: %v", err)
		}
	}
	if again, err := h.update(0).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: other}); err != nil || again.GetNextOffset() != 0 {
		t.Fatalf("restage after withdrawal = %v, %v", again, err)
	}
	// An incomplete stage can be aborted and restarted from zero.
	partial := h.manifest("job-abort", "ct1-b", pkg)
	pb, err := h.update(1).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: partial})
	if err != nil {
		t.Fatal(err)
	}
	bscoped := metadata.AppendToOutgoingContext(ctx, serverIDHeader, "ct1-b")
	if _, err := h.update(1).AbortServerUpdateStage(bscoped, &mdk.AbortServerUpdateStageRequest{JobId: "job-abort", ManifestSha256: pb.GetManifestSha256()}); err != nil {
		t.Fatal(err)
	}
	restart, err := h.update(1).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: partial})
	if err != nil || restart.GetNextOffset() != 0 {
		t.Fatalf("restart = %v, %v", restart, err)
	}
}

func TestAbortKeepsCommittedStageWhileItsRolloutRuns(t *testing.T) {
	h := newUpdateHarness(t, func(c *updateSimConfig) { c.StepDelay = 100 * time.Millisecond })
	ctx := metadata.AppendToOutgoingContext(context.Background(), actorHeader, "proto-fleet:tester")
	pkg := testPackage(2500)
	fingerprints := h.stageBoth("job-running", pkg)
	if _, err := h.update(0).StartServerUpdate(ctx, startRequest("job-running", fingerprints)); err != nil {
		t.Fatal(err)
	}
	abort := func() error {
		scoped := metadata.AppendToOutgoingContext(ctx, serverIDHeader, "ct1-a")
		_, err := h.update(0).AbortServerUpdateStage(scoped, &mdk.AbortServerUpdateStageRequest{JobId: "job-running", ManifestSha256: fingerprints["ct1-a"]})
		return err
	}
	requireRefusal(t, abort(), codes.FailedPrecondition, "UPDATE_ACTIVE_JOB")
	h.waitPhase("job-running", mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED)
	if err := abort(); err != nil {
		t.Fatalf("abort after the rollout finished = %v", err)
	}
}

func TestManifestValidation(t *testing.T) {
	h := newUpdateHarness(t, nil)
	ctx := context.Background()
	pkg := testPackage(100)
	cases := map[string]func(*mdk.ServerUpdateManifest){
		"missing job":        func(m *mdk.ServerUpdateManifest) { m.JobId = "" },
		"bad digest":         func(m *mdk.ServerUpdateManifest) { m.Sha256 = "XYZ" },
		"no statement":       func(m *mdk.ServerUpdateManifest) { m.ReleaseStatement = nil },
		"flotilla field":     func(m *mdk.ServerUpdateManifest) { m.FlotillaAddress = "flotilla:443" },
		"maintenance token":  func(m *mdk.ServerUpdateManifest) { m.MaintenanceToken = "t" },
		"instance not named": func(m *mdk.ServerUpdateManifest) { m.ExpectedInstanceIds = []string{"ct1-b"} },
		"legacy with sig": func(m *mdk.ServerUpdateManifest) {
			m.Coordination = mdk.ServerUpdateCoordination_SERVER_UPDATE_COORDINATION_UNSPECIFIED
		},
	}
	for name, mutate := range cases {
		m := h.manifest("job-"+strings.ReplaceAll(name, " ", "-"), "ct1-a", pkg)
		mutate(m)
		_, err := h.update(0).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: m})
		if name == "missing job" || name == "bad digest" || name == "no statement" || name == "flotilla field" || name == "maintenance token" || name == "instance not named" || name == "legacy with sig" {
			requireRefusal(t, err, codes.InvalidArgument, "UPDATE_MANIFEST_INVALID")
		}
	}
	h.sim.setFaults(func(f *updateFaults) { f.SelfCoordinationUnsupported = true })
	_, err := h.update(0).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: h.manifest("job-unsupported", "ct1-a", pkg)})
	requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_SELF_COORDINATION_UNSUPPORTED")
}

func TestSupportReportsInstanceAndCluster(t *testing.T) {
	h := newUpdateHarness(t, nil)
	ctx := context.Background()
	a, err := h.update(0).GetServerUpdateSupport(ctx, &mdk.GetServerUpdateSupportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.update(0).GetServerUpdateSupport(metadata.AppendToOutgoingContext(ctx, serverIDHeader, "ct1-b"), &mdk.GetServerUpdateSupportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if a.GetInstanceId() != "ct1-a" || a.GetPeerInstanceId() != "ct1-b" || b.GetInstanceId() != "ct1-b" || b.GetPeerInstanceId() != "ct1-a" {
		t.Fatalf("instances: %v %v", a, b)
	}
	if a.GetClusterId() != testEtcdClusterID || b.GetClusterId() != testEtcdClusterID || !a.GetSelfCoordinationSupported() || !a.GetRemoteTriggerAllowed() || !a.GetHelperAvailable() ||
		a.GetRunningVersion() != "1.0.0" || len(a.GetTrustedReleaseKeyIds()) != 1 || !strings.HasPrefix(a.GetTrustedReleaseKeyIds()[0], "sha256:") || a.GetEtcdMembersHealthy() != 3 {
		t.Fatalf("support = %v", a)
	}
	wantID, err := releaseKeyID(&h.key.PublicKey)
	if err != nil || a.GetTrustedReleaseKeyIds()[0] != wantID {
		t.Fatalf("key id %v != %s", a.GetTrustedReleaseKeyIds(), wantID)
	}
}

func TestSelfCoordinatedRolloutAndMaintenanceMode(t *testing.T) {
	h := newUpdateHarness(t, func(c *updateSimConfig) { c.StepDelay = 120 * time.Millisecond })
	ctx := metadata.AppendToOutgoingContext(context.Background(), actorHeader, "proto-fleet:tester")
	pkg := testPackage(2500)
	fingerprints := h.stageBoth("job-ok", pkg)
	req := startRequest("job-ok", fingerprints)
	started, err := h.update(1).StartServerUpdate(ctx, &mdk.StartServerUpdateRequest{JobId: req.JobId, TargetVersion: req.TargetVersion, ExpectedInstanceIds: req.ExpectedInstanceIds, IdempotencyKey: req.IdempotencyKey, ExpectedManifestSha256: req.ExpectedManifestSha256})
	if err != nil {
		t.Fatal(err)
	}
	rollout := started.GetRollout()
	if rollout.GetPhase() != mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFYING || !rollout.GetCancellable() || rollout.GetActor() != "proto-fleet:tester" ||
		rollout.GetFromVersion() != "1.0.0" || rollout.GetPackageSha256() == "" || !strings.HasPrefix(rollout.GetReleaseKeyId(), "sha256:") || len(rollout.GetInstances()) != 2 {
		t.Fatalf("started = %v", rollout)
	}
	// The same key and request returns the same rollout; a different request conflicts.
	replay, err := h.update(0).StartServerUpdate(ctx, req)
	if err != nil || replay.GetRollout().GetJobId() != "job-ok" {
		t.Fatalf("replay = %v, %v", replay, err)
	}
	conflict := startRequest("job-ok", fingerprints)
	conflict.StageOnly = true
	_, err = h.update(0).StartServerUpdate(ctx, conflict)
	requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_IDEMPOTENCY_CONFLICT")
	// A second job while one is active is refused.
	_, err = h.update(0).StartServerUpdate(ctx, &mdk.StartServerUpdateRequest{JobId: "other", TargetVersion: testTargetVersion, ExpectedInstanceIds: req.ExpectedInstanceIds, IdempotencyKey: "other"})
	requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_ACTIVE_JOB")

	phases := map[mdk.ServerUpdateRolloutPhase]bool{}
	sawMaintenance := false
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := h.update(0).GetServerUpdateStatus(ctx, &mdk.GetServerUpdateStatusRequest{})
		if err != nil {
			t.Fatal(err)
		}
		phase := resp.GetRollout().GetPhase()
		phases[phase] = true
		if resp.GetServedByMaintenance() {
			sawMaintenance = true
			// Frigate may restart between the poll and these calls, so a surprising
			// answer only fails the test if maintenance is still active afterwards.
			stillStopped := func() bool {
				again, err := h.update(0).GetServerUpdateStatus(ctx, &mdk.GetServerUpdateStatusRequest{})
				return err == nil && again.GetServedByMaintenance()
			}
			// Other services are Unavailable; the peer answers status too.
			_, sysErr := mdk.NewSystemServiceClient(h.conns[1]).GetSystemInfo(ctx, &mdk.GetSystemInfoRequest{})
			if status.Code(sysErr) != codes.Unavailable && stillStopped() {
				t.Fatalf("system service during maintenance = %v", sysErr)
			}
			_, supportErr := h.update(1).GetServerUpdateSupport(ctx, &mdk.GetServerUpdateSupportRequest{})
			if status.Code(supportErr) != codes.Unavailable && stillStopped() {
				t.Fatalf("support during maintenance = %v", supportErr)
			}
			peer, peerErr := h.update(1).GetServerUpdateStatus(ctx, &mdk.GetServerUpdateStatusRequest{JobId: "job-ok"})
			if (peerErr != nil || !peer.GetServedByMaintenance()) && stillStopped() {
				t.Fatalf("peer status = %v, %v", peer, peerErr)
			}
			if !resp.GetRollout().GetDisruptionStarted() || resp.GetRollout().GetCancellable() {
				t.Fatalf("stopped rollout flags = %v", resp.GetRollout())
			}
		}
		if phase == mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rollout stuck at %v", resp.GetRollout())
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, want := range []mdk.ServerUpdateRolloutPhase{
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFYING, mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STOPPING,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_INSTALLING, mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_MIGRATING,
		mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_STARTING, mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED,
	} {
		if !phases[want] {
			t.Errorf("phase %s was never observed", want)
		}
	}
	if !sawMaintenance {
		t.Fatal("maintenance mode was never observed")
	}
	final, err := h.update(0).GetServerUpdateStatus(ctx, &mdk.GetServerUpdateStatusRequest{JobId: "job-ok"})
	if err != nil || final.GetServedByMaintenance() {
		t.Fatalf("final = %v, %v", final, err)
	}
	executors := 0
	for _, instance := range final.GetRollout().GetInstances() {
		if instance.GetPhase() != mdk.FrigateUpdateAgentPhase_FRIGATE_UPDATE_AGENT_PHASE_STARTED || instance.GetRunningVersion() != testTargetVersion || instance.GetManifestSha256() != fingerprints[instance.GetInstanceId()] || instance.GetContactStale() {
			t.Fatalf("instance = %v", instance)
		}
		if instance.GetMigrationExecuted() {
			executors++
		}
	}
	if executors != 1 {
		t.Fatalf("migration executors = %d", executors)
	}
	info, err := mdk.NewSystemServiceClient(h.conns[1]).GetSystemInfo(ctx, &mdk.GetSystemInfoRequest{})
	if err != nil || info.GetInfo().GetVersion() != testTargetVersion {
		t.Fatalf("version after update = %v, %v", info, err)
	}
	support, err := h.update(0).GetServerUpdateSupport(ctx, &mdk.GetServerUpdateSupportRequest{})
	if err != nil || support.GetRunningVersion() != testTargetVersion || support.GetActiveJobId() != "" {
		t.Fatalf("support after update = %v, %v", support, err)
	}
	// Terminal rollouts stay readable by empty job id.
	last, err := h.update(1).GetServerUpdateStatus(ctx, &mdk.GetServerUpdateStatusRequest{})
	if err != nil || last.GetRollout().GetJobId() != "job-ok" {
		t.Fatalf("latest = %v, %v", last, err)
	}
	_, err = h.update(1).GetServerUpdateStatus(ctx, &mdk.GetServerUpdateStatusRequest{JobId: "missing"})
	requireRefusal(t, err, codes.NotFound, "UPDATE_NOT_FOUND")
}

func TestStageOnlyVerifiesWithoutStopping(t *testing.T) {
	h := newUpdateHarness(t, nil)
	ctx := context.Background()
	pkg := testPackage(600)
	req := startRequest("job-verify", h.stageBoth("job-verify", pkg))
	req.StageOnly = true
	if _, err := h.update(0).StartServerUpdate(ctx, req); err != nil {
		t.Fatal(err)
	}
	final := h.waitPhase("job-verify", mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_VERIFIED)
	if final.GetRollout().GetDisruptionStarted() || final.GetServedByMaintenance() || !final.GetRollout().GetStageOnly() {
		t.Fatalf("stage-only rollout = %v", final)
	}
	if h.sim.inMaintenance() {
		t.Fatal("stage-only update entered maintenance")
	}
	info, err := mdk.NewSystemServiceClient(h.conns[0]).GetSystemInfo(ctx, &mdk.GetSystemInfoRequest{})
	if err != nil || info.GetInfo().GetVersion() != "1.0.0" {
		t.Fatalf("stage-only changed version: %v %v", info, err)
	}
}

func TestMigrateFailureIsRetryable(t *testing.T) {
	h := newUpdateHarness(t, func(c *updateSimConfig) { c.Faults.MigrateFailures = 1 })
	ctx := context.Background()
	pkg := testPackage(600)
	req := startRequest("job-retry", h.stageBoth("job-retry", pkg))
	if _, err := h.update(0).StartServerUpdate(ctx, req); err != nil {
		t.Fatal(err)
	}
	failed := h.waitPhase("job-retry", mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_FAILED)
	r := failed.GetRollout()
	if r.GetErrorCode() != "UPDATE_MIGRATE_FAILED" || !r.GetRetryable() || r.GetCancellable() || !r.GetDisruptionStarted() || !failed.GetServedByMaintenance() {
		t.Fatalf("failed rollout = %v", failed)
	}
	// Frigate stays stopped, and cancel is refused after the point of no return.
	_, err := mdk.NewSystemServiceClient(h.conns[0]).GetSystemInfo(ctx, &mdk.GetSystemInfoRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("system service after failure = %v", err)
	}
	_, err = h.update(1).CancelServerUpdate(ctx, &mdk.CancelServerUpdateRequest{JobId: "job-retry"})
	requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_NOT_CANCELLABLE")
	retried, err := h.update(1).RetryServerUpdate(ctx, &mdk.RetryServerUpdateRequest{JobId: "job-retry", IdempotencyKey: "retry-1"})
	if err != nil || retried.GetRollout().GetErrorCode() != "" || retried.GetRollout().GetAutomaticRetries() != 1 {
		t.Fatalf("retry = %v, %v", retried, err)
	}
	again, err := h.update(0).RetryServerUpdate(ctx, &mdk.RetryServerUpdateRequest{JobId: "job-retry", IdempotencyKey: "retry-1"})
	if err != nil || again.GetRollout().GetAutomaticRetries() != 1 {
		t.Fatalf("idempotent retry = %v, %v", again, err)
	}
	h.waitPhase("job-retry", mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED)
	_, err = h.update(0).RetryServerUpdate(ctx, &mdk.RetryServerUpdateRequest{JobId: "job-retry", IdempotencyKey: "retry-2"})
	requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_NOT_RETRYABLE")
}

func TestCancelBeforeStop(t *testing.T) {
	h := newUpdateHarness(t, func(c *updateSimConfig) { c.StepDelay = time.Hour })
	ctx := context.Background()
	pkg := testPackage(600)
	req := startRequest("job-cancel", h.stageBoth("job-cancel", pkg))
	if _, err := h.update(0).StartServerUpdate(ctx, req); err != nil {
		t.Fatal(err)
	}
	cancelled, err := h.update(1).CancelServerUpdate(ctx, &mdk.CancelServerUpdateRequest{JobId: "job-cancel", IdempotencyKey: "c1"})
	if err != nil || cancelled.GetRollout().GetPhase() != mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_CANCELLED || cancelled.GetRollout().GetDisruptionStarted() {
		t.Fatalf("cancel = %v, %v", cancelled, err)
	}
	// A job ID cannot be reused after it ran; another job may start.
	restart := startRequest("job-cancel", nil)
	restart.IdempotencyKey = "restart"
	_, err = h.update(0).StartServerUpdate(ctx, restart)
	requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_ACTIVE_JOB")
	req2 := startRequest("job-after", h.stageBoth("job-after", pkg))
	if _, err := h.update(0).StartServerUpdate(ctx, req2); err != nil {
		t.Fatal(err)
	}
}

func TestStartRefusals(t *testing.T) {
	pkg := testPackage(600)
	cases := []struct {
		name   string
		tune   func(*updateSimConfig)
		change func(*updateHarness, *mdk.StartServerUpdateRequest)
		code   codes.Code
		reason string
	}{
		{name: "remote trigger", tune: func(c *updateSimConfig) { c.Faults.RemoteTriggerDisabled = true }, code: codes.FailedPrecondition, reason: "UPDATE_REMOTE_TRIGGER_DISABLED"},
		{name: "self coordination", tune: func(c *updateSimConfig) { c.Faults.SelfCoordinationUnsupported = true }, code: codes.FailedPrecondition, reason: "UPDATE_SELF_COORDINATION_UNSUPPORTED"},
		{name: "etcd", tune: func(c *updateSimConfig) { c.Faults.EtcdUnhealthy = true }, code: codes.Unavailable, reason: "UPDATE_ETCD_UNHEALTHY"},
		{name: "helper", tune: func(c *updateSimConfig) { c.Faults.HelperUnavailable = true }, code: codes.Unavailable, reason: "UPDATE_HELPER_UNAVAILABLE"},
		{name: "window", tune: func(c *updateSimConfig) { c.Faults.OutsideWindow = true }, code: codes.FailedPrecondition, reason: "UPDATE_OUTSIDE_WINDOW"},
		{name: "firmware active", tune: func(c *updateSimConfig) { c.Faults.FirmwareActive = true }, code: codes.FailedPrecondition, reason: "UPDATE_FIRMWARE_ACTIVE"},
		{name: "space", tune: func(c *updateSimConfig) { c.SpoolFreeBytes = 10 }, code: codes.FailedPrecondition, reason: "UPDATE_INSUFFICIENT_SPACE"},
		{name: "no keys", tune: func(c *updateSimConfig) { c.TrustedKeys = nil }, code: codes.FailedPrecondition, reason: "UPDATE_NO_TRUSTED_KEYS"},
		{name: "not staged", change: func(_ *updateHarness, r *mdk.StartServerUpdateRequest) { r.JobId = "never-staged" }, code: codes.FailedPrecondition, reason: "UPDATE_NOT_STAGED"},
		{name: "wrong version", change: func(_ *updateHarness, r *mdk.StartServerUpdateRequest) { r.TargetVersion = "1.2.3" }, code: codes.FailedPrecondition, reason: "UPDATE_STAGE_MISMATCH"},
		{name: "wrong instances", change: func(_ *updateHarness, r *mdk.StartServerUpdateRequest) { r.ExpectedInstanceIds = []string{"ct1-a"} }, code: codes.FailedPrecondition, reason: "UPDATE_STAGE_MISMATCH"},
		{name: "wrong fingerprint", change: func(_ *updateHarness, r *mdk.StartServerUpdateRequest) {
			r.ExpectedManifestSha256 = map[string]string{"ct1-a": "deadbeef"}
		}, code: codes.FailedPrecondition, reason: "UPDATE_STAGE_MISMATCH"},
		{name: "malformed", change: func(_ *updateHarness, r *mdk.StartServerUpdateRequest) { r.JobId = "" }, code: codes.InvalidArgument, reason: "UPDATE_MANIFEST_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newUpdateHarness(t, func(c *updateSimConfig) {
				if tc.tune != nil {
					tc.tune(c)
				}
			})
			// Staging must succeed even when the fault only affects Start.
			faults := h.sim.cfg.Faults
			h.sim.setFaults(func(f *updateFaults) { *f = updateFaults{} })
			fingerprints := h.stageBoth("job", pkg)
			h.sim.setFaults(func(f *updateFaults) { *f = faults })
			req := startRequest("job", fingerprints)
			if tc.change != nil {
				tc.change(h, req)
			}
			_, err := h.update(0).StartServerUpdate(context.Background(), req)
			requireRefusal(t, err, tc.code, tc.reason)
		})
	}
}

func TestStartRefusesBadSignatures(t *testing.T) {
	pkg := testPackage(600)
	t.Run("untrusted key", func(t *testing.T) {
		h := newUpdateHarness(t, nil)
		other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		fingerprints := h.stageBoth("job", pkg)
		h.sim.mu.Lock()
		h.sim.trusted = map[string]*ecdsa.PublicKey{}
		id, _ := releaseKeyID(&other.PublicKey)
		h.sim.trusted[id] = &other.PublicKey
		h.sim.mu.Unlock()
		_, err = h.update(0).StartServerUpdate(context.Background(), startRequest("job", fingerprints))
		requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_SIGNATURE_INVALID")
	})
	t.Run("tampered statement", func(t *testing.T) {
		h := newUpdateHarness(t, nil)
		ctx := context.Background()
		for i, instance := range []string{"ct1-a", "ct1-b"} {
			m := h.manifest("job", instance, pkg)
			m.ReleaseStatement = []byte(strings.Replace(string(m.ReleaseStatement), "frigate", "frigatx", 1))
			begin, err := h.update(i).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: m})
			if err != nil {
				t.Fatal(err)
			}
			scoped := metadata.AppendToOutgoingContext(ctx, serverIDHeader, instance)
			if _, err := h.update(i).AppendServerUpdateStage(scoped, &mdk.AppendServerUpdateStageRequest{JobId: "job", ManifestSha256: begin.GetManifestSha256(), Chunk: pkg}); err != nil {
				t.Fatal(err)
			}
			if _, err := h.update(i).CommitServerUpdateStage(scoped, &mdk.CommitServerUpdateStageRequest{JobId: "job", ManifestSha256: begin.GetManifestSha256()}); err != nil {
				t.Fatal(err)
			}
		}
		_, err := h.update(0).StartServerUpdate(ctx, startRequest("job", nil))
		requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_SIGNATURE_INVALID")
	})
	t.Run("statement for different package", func(t *testing.T) {
		h := newUpdateHarness(t, nil)
		ctx := context.Background()
		for i, instance := range []string{"ct1-a", "ct1-b"} {
			m := h.manifest("job", instance, pkg)
			m.ReleaseStatement, m.ReleaseSignature = h.statement(testPackage(601), testTargetVersion)
			begin, err := h.update(i).BeginServerUpdateStage(ctx, &mdk.BeginServerUpdateStageRequest{Manifest: m})
			if err != nil {
				t.Fatal(err)
			}
			scoped := metadata.AppendToOutgoingContext(ctx, serverIDHeader, instance)
			if _, err := h.update(i).AppendServerUpdateStage(scoped, &mdk.AppendServerUpdateStageRequest{JobId: "job", ManifestSha256: begin.GetManifestSha256(), Chunk: pkg}); err != nil {
				t.Fatal(err)
			}
			if _, err := h.update(i).CommitServerUpdateStage(scoped, &mdk.CommitServerUpdateStageRequest{JobId: "job", ManifestSha256: begin.GetManifestSha256()}); err != nil {
				t.Fatal(err)
			}
		}
		_, err := h.update(0).StartServerUpdate(ctx, startRequest("job", nil))
		requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_STAGE_MISMATCH")
	})
}

func TestCooldownAfterSuccess(t *testing.T) {
	h := newUpdateHarness(t, func(c *updateSimConfig) { c.Faults.Cooldown = time.Hour })
	ctx := context.Background()
	pkg := testPackage(600)
	if _, err := h.update(0).StartServerUpdate(ctx, startRequest("job-1", h.stageBoth("job-1", pkg))); err != nil {
		t.Fatal(err)
	}
	h.waitPhase("job-1", mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED)
	fingerprints := h.stageBoth("job-2", pkg)
	_, err := h.update(0).StartServerUpdate(ctx, startRequest("job-2", fingerprints))
	requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_COOLDOWN")
}

func TestStaleContactAndDriverReboot(t *testing.T) {
	h := newUpdateHarness(t, func(c *updateSimConfig) {
		c.StepDelay = 80 * time.Millisecond
		c.Faults.StaleContact, c.Faults.DriverReboot = true, true
	})
	ctx := context.Background()
	pkg := testPackage(600)
	started, err := h.update(0).StartServerUpdate(ctx, startRequest("job", h.stageBoth("job", pkg)))
	if err != nil {
		t.Fatal(err)
	}
	if started.GetRollout().GetDriverInstanceId() != "ct1-a" {
		t.Fatalf("initial driver = %q", started.GetRollout().GetDriverInstanceId())
	}
	stale := h.waitPhase("job", mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_INSTALLING).GetRollout()
	if stale.GetDriverInstanceId() != "ct1-b" {
		t.Fatalf("driver did not move: %v", stale)
	}
	for _, instance := range stale.GetInstances() {
		if want := instance.GetInstanceId() != stale.GetDriverInstanceId(); instance.GetContactStale() != want {
			t.Fatalf("contact_stale on %s = %v", instance.GetInstanceId(), instance.GetContactStale())
		}
		if instance.GetContactStale() && time.Since(instance.GetLastContact().AsTime()) < 30*time.Second {
			t.Fatalf("stale instance last_contact is recent: %v", instance.GetLastContact())
		}
	}
	h.waitPhase("job", mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED)
}

func TestLegacyStreamingStage(t *testing.T) {
	h := newUpdateHarness(t, nil)
	ctx := context.Background()
	pkg := testPackage(1500)
	sum := sha256.Sum256(pkg)
	stream, err := h.update(0).StageServerUpdate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &mdk.ServerUpdateManifest{JobId: "legacy", InstanceId: "ct1-a", ExpectedInstanceIds: []string{"ct1-a", "ct1-b"}, TargetVersion: testTargetVersion, Sha256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(pkg))}
	if err := stream.Send(&mdk.StageServerUpdateRequest{Payload: &mdk.StageServerUpdateRequest_Manifest{Manifest: manifest}}); err != nil {
		t.Fatal(err)
	}
	for off := 0; off < len(pkg); off += 500 {
		if err := stream.Send(&mdk.StageServerUpdateRequest{Payload: &mdk.StageServerUpdateRequest_Chunk{Chunk: pkg[off : off+500]}}); err != nil {
			t.Fatal(err)
		}
	}
	reply, err := stream.CloseAndRecv()
	if err != nil || reply.GetBytesStaged() != 1500 || reply.GetSha256() != hex.EncodeToString(sum[:]) {
		t.Fatalf("legacy stage = %v, %v", reply, err)
	}
}

func TestTrustedKeyFilesAndPairSettings(t *testing.T) {
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "release.pub")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CT1_HA_PAIR", "1")
	t.Setenv("CT1_UPDATE_TRUSTED_KEY_FILES", path)
	t.Setenv("CT1_UPDATE_FAULT_MIGRATE_FAILURES", "2")
	t.Setenv("CT1_UPDATE_FAULT_STALE_CONTACT", "true")
	t.Setenv("CT1_UPDATE_STEP", "50ms")
	t.Setenv("CT1_HA_PRIMARY", "b")
	t.Setenv("CT1_HA_FAULT_PEER_UNREACHABLE", "1")
	settings, enabled, err := haPairSettingsFromEnv(ContainerConfig{ServerID: "node"}, 9095, 8080)
	if err != nil || !enabled {
		t.Fatalf("settings = %v, %v, %v", settings, enabled, err)
	}
	if settings.PeerServerID != "node-b" || settings.PeerGRPCPort != 9096 || settings.PeerHTTPPort != 8081 || settings.Update.Primary != "node-b" ||
		len(settings.Update.TrustedKeys) != 1 || settings.Update.Faults.MigrateFailures != 2 || !settings.Update.Faults.StaleContact || settings.Update.StepDelay != 50*time.Millisecond || !settings.PeerUnreachable {
		t.Fatalf("settings = %+v", settings)
	}
	t.Setenv("CT1_HA_PRIMARY", "c")
	if _, _, err := haPairSettingsFromEnv(ContainerConfig{ServerID: "node"}, 9095, 8080); err == nil {
		t.Fatal("invalid primary accepted")
	}
	t.Setenv("CT1_HA_PAIR", "")
	if _, enabled, _ := haPairSettingsFromEnv(ContainerConfig{ServerID: "node"}, 9095, 8080); enabled {
		t.Fatal("pair enabled without CT1_HA_PAIR")
	}
}

// TestRolloutSuccessDoesNotDeadlockStatusReads reads the system status the
// way the REST handler does (state lock first, then the HA role) while the
// rollout finishes, which once took the two locks in opposite orders.
func TestRolloutSuccessDoesNotDeadlockStatusReads(t *testing.T) {
	for range 20 {
		h := newUpdateHarness(t, func(c *updateSimConfig) { c.StepDelay = time.Millisecond })
		ctx := t.Context()
		const job = "6f9c7c1e-3f3d-4b55-9c11-2a1c3a8d4e01"
		fingerprints := h.stageBoth(job, testPackage(64))
		stop := make(chan struct{})
		var readers sync.WaitGroup
		for _, state := range h.states {
			readers.Add(1)
			go func() {
				defer readers.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					state.mu.RLock()
					_ = state.systemStatusDTO()
					state.mu.RUnlock()
				}
			}()
		}
		if _, err := h.update(0).StartServerUpdate(ctx, startRequest(job, fingerprints)); err != nil {
			t.Fatal(err)
		}
		// A deadlock leaves the status call blocked, so each read has a deadline.
		deadline := time.Now().Add(10 * time.Second)
		for {
			readCtx, cancel := context.WithTimeout(ctx, time.Second)
			resp, err := h.update(0).GetServerUpdateStatus(readCtx, &mdk.GetServerUpdateStatusRequest{JobId: job})
			cancel()
			if err == nil && resp.GetRollout().GetPhase() == mdk.ServerUpdateRolloutPhase_SERVER_UPDATE_ROLLOUT_PHASE_SUCCEEDED {
				break
			}
			if time.Now().After(deadline) {
				close(stop)
				t.Fatalf("the rollout and a status read deadlocked: %v %v", resp, err)
			}
			time.Sleep(time.Millisecond)
		}
		close(stop)
		readers.Wait()
		for _, state := range h.states {
			state.mu.RLock()
			version := state.Version
			state.mu.RUnlock()
			if version != testTargetVersion {
				t.Fatalf("version = %q", version)
			}
		}
	}
}

func TestStartRefusedWhileHashboardFlashRuns(t *testing.T) {
	h := newUpdateHarness(t, nil)
	fingerprints := h.stageBoth("job", testPackage(600))
	peer := h.states[1]
	peer.mu.Lock()
	job := &mdk.FirmwareUpdate{TaskId: "flash-1", StartedAt: timestamppb.New(peer.now())}
	peer.firmware.jobs["flash-1"] = job
	peer.mu.Unlock()

	_, err := h.update(0).StartServerUpdate(context.Background(), startRequest("job", fingerprints))
	requireRefusal(t, err, codes.FailedPrecondition, "UPDATE_FIRMWARE_ACTIVE")
	if !strings.Contains(status.Convert(err).Message(), "ct1-b/flash-1") {
		t.Fatalf("refusal does not name the running flash: %v", err)
	}

	peer.mu.Lock()
	job.CompletedAt = timestamppb.New(peer.now())
	peer.mu.Unlock()
	if _, err := h.update(0).StartServerUpdate(context.Background(), startRequest("job", fingerprints)); err != nil {
		t.Fatalf("Start after the flash finished: %v", err)
	}
}
