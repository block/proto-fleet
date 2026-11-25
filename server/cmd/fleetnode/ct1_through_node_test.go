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
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	"github.com/block/proto-fleet/sdk/mdk/firmware/firmwaretest"
	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/remotenode"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// These tests join the parts of a CT1 container behind a Fleet Node: Fleet's
// handle for the container, the node's command handler, the Proto plugin
// process and the CT1 simulator. Only the control stream between Fleet and the
// node is replaced: each command goes from the handle to the handler directly.

const ct1NodeClusterID = "7900e1a3-5be9-4a6b-b640-eaf3d959333b"

var ct1NodeBinaries struct {
	sync.Once
	simulator, pluginDir string
	err                  error
}

// buildCT1NodeBinaries builds the simulator and the Proto plugin once.
func buildCT1NodeBinaries(t *testing.T) (simulator, pluginDir string) {
	t.Helper()
	b := &ct1NodeBinaries
	b.Do(func() {
		root, err := filepath.Abs("../../..")
		if err != nil {
			b.err = err
			return
		}
		dir, err := os.MkdirTemp("", "ct1-through-node-*") //nolint:usetesting // Shared by every test in the package.
		if err != nil {
			b.err = err
			return
		}
		b.simulator, b.pluginDir = filepath.Join(dir, "fake-ct1"), filepath.Join(dir, "plugins")
		if b.err = os.Mkdir(b.pluginDir, 0o750); b.err != nil {
			return
		}
		for _, build := range []struct{ source, target, workspace string }{
			{"server/fake-ct1", b.simulator, "off"}, // The simulator is a separate module.
			{"plugin/proto", filepath.Join(b.pluginDir, "proto-plugin"), filepath.Join(root, "go.work")},
		} {
			cmd := exec.Command("go", "build", "-o", build.target, ".")
			cmd.Dir = filepath.Join(root, build.source)
			cmd.Env = append(os.Environ(), "GOWORK="+build.workspace)
			if output, err := cmd.CombinedOutput(); err != nil {
				b.err = fmt.Errorf("build %s: %w: %s", build.source, err, output)
				return
			}
		}
	})
	require.NoError(t, b.err)
	return b.simulator, b.pluginDir
}

// ct1NodeArtifacts is Fleet's artifact download for the node. It serves an
// artifact only to the command that Fleet granted it to.
type ct1NodeArtifacts struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler
	mu        sync.Mutex
	files     map[string][]byte
	granted   map[string][]control.ArtifactExpectation
	downloads []*pb.DownloadCommandArtifactRequest
}

func (a *ct1NodeArtifacts) grant(commandID string, artifacts []control.ArtifactExpectation) func() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.granted[commandID] = artifacts
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		delete(a.granted, commandID)
	}
}

func (a *ct1NodeArtifacts) DownloadCommandArtifact(_ context.Context, req *connect.Request[pb.DownloadCommandArtifactRequest], stream *connect.ServerStream[pb.DownloadCommandArtifactResponse]) error {
	ref := req.Msg.GetArtifact()
	a.mu.Lock()
	a.downloads = append(a.downloads, req.Msg)
	data, ok := a.files[ref.GetArtifactId()]
	allowed := false
	for _, e := range a.granted[req.Msg.GetCommandId()] {
		allowed = allowed || (e.Direction == control.ArtifactDirectionDownload && e.Purpose == ref.GetPurpose() &&
			e.ArtifactID == ref.GetArtifactId() && e.DeviceIdentifier == req.Msg.GetDeviceIdentifier())
	}
	a.mu.Unlock()
	if !ok || !allowed {
		return connect.NewError(connect.CodePermissionDenied, errors.New("the command has no grant for this artifact"))
	}
	if err := stream.Send(&pb.DownloadCommandArtifactResponse{Part: &pb.DownloadCommandArtifactResponse_Header{
		Header: &pb.CommandArtifactDownloadHeader{Artifact: ref},
	}}); err != nil {
		return fmt.Errorf("send artifact header: %w", err)
	}
	const chunk = 256 << 10
	for offset := 0; offset < len(data); offset += chunk {
		if err := stream.Send(&pb.DownloadCommandArtifactResponse{Part: &pb.DownloadCommandArtifactResponse_Chunk{
			Chunk: &pb.CommandArtifactChunk{Data: data[offset:min(offset+chunk, len(data))]},
		}}); err != nil {
			return fmt.Errorf("send artifact chunk: %w", err)
		}
	}
	return nil
}

func (a *ct1NodeArtifacts) downloadsOf(purpose pb.CommandArtifactPurpose) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, d := range a.downloads {
		if d.GetArtifact().GetPurpose() == purpose {
			count++
		}
	}
	return count
}

// ct1NodeLink carries each Fleet command to the node's handler and returns its
// acknowledgement. It keeps the checks that the control stream makes on both
// sides: the node's command protocol version and the message contracts.
type ct1NodeLink struct {
	t         *testing.T
	run       *RunCmd
	client    gatewayClient
	artifacts *ct1NodeArtifacts
	mu        sync.Mutex
	writes    []commandtype.Type
}

func (l *ct1NodeLink) SendCommand(ctx context.Context, _ int64, version pb.CommandProtocolVersion, cmd *pb.ControlCommand) (*pb.ControlAck, error) {
	if version > latestCommandProtocolVersion {
		return nil, control.ErrCommandProtocolUpgradeRequired
	}
	if err := protovalidate.Validate(cmd); err != nil {
		return nil, fmt.Errorf("fleet sent a command outside the contract: %w", err)
	}
	var wire pb.AgentCommand
	if err := proto.Unmarshal(cmd.GetPayload(), &wire); err != nil {
		return nil, fmt.Errorf("decode agent command: %w", err)
	}
	capture := &captureAcker{}
	l.run.handleMinerCommand(ctx, l.client, capture, cmd.GetCommandId(), wire.GetMinerCommand(), discardLogger(l.t))
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.acks) != 1 {
		return nil, fmt.Errorf("the node sent %d acknowledgements", len(capture.acks))
	}
	if err := protovalidate.Validate(capture.acks[0]); err != nil {
		return nil, fmt.Errorf("the node sent an acknowledgement outside the contract: %w", err)
	}
	return capture.acks[0], nil
}

func (l *ct1NodeLink) SendCommandWithArtifactResults(ctx context.Context, fleetNodeID int64, version pb.CommandProtocolVersion, cmd *pb.ControlCommand, artifacts []control.ArtifactExpectation) (*pb.ControlAck, []*pb.CommandArtifactRef, error) {
	defer l.artifacts.grant(cmd.GetCommandId(), artifacts)()
	ack, err := l.SendCommand(ctx, fleetNodeID, version, cmd)
	return ack, nil, err
}

func (l *ct1NodeLink) writeGate(ctx context.Context, kind commandtype.Type, write func(context.Context) error) error {
	l.mu.Lock()
	l.writes = append(l.writes, kind)
	l.mu.Unlock()
	return write(ctx)
}

func (l *ct1NodeLink) gatedWrites() []commandtype.Type {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]commandtype.Type(nil), l.writes...)
}

// ct1BehindNode is a simulated CT1 container that Fleet reaches through a node.
type ct1BehindNode struct {
	miner     *remotenode.Miner
	link      *ct1NodeLink
	artifacts *ct1NodeArtifacts
	driver    sdk.Driver
	info      sdk.DeviceInfo
}

// startCT1BehindNode starts the simulator with env and returns Fleet's handle
// for it. With pair set, the container is an HA pair of controllers.
func startCT1BehindNode(t *testing.T, pair bool, env ...string) *ct1BehindNode {
	t.Helper()
	simulator, pluginDir := buildCT1NodeBinaries(t)
	// Reserve four free ports, and release them just before the simulator starts.
	ports := make([]int32, 4)
	reserved := make([]net.Listener, len(ports))
	for i := range ports {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		reserved[i] = l
		_, text, err := net.SplitHostPort(l.Addr().String())
		require.NoError(t, err)
		number, err := strconv.ParseInt(text, 10, 32)
		require.NoError(t, err)
		ports[i] = int32(number)
	}
	port := func(i int) string { return strconv.Itoa(int(ports[i])) }
	process := exec.CommandContext(t.Context(), simulator)
	process.Env = append(os.Environ(), "HTTP_PORT="+port(0), "GRPC_PORT="+port(1), "SERVER_ID=pair-a", "CT1_DEMO_MODULE_STATES=false")
	config := remotenode.Config{
		DeviceClass: commonpb.DeviceClass_DEVICE_CLASS_CONTAINER, DeviceIdentifier: "ct1-behind-node", DriverName: "proto",
		IPAddress: "127.0.0.1", Port: port(0), URLScheme: "http", RPCPort: ports[1], SerialNumber: "pair-a",
		FleetNodeID: 7, OrgID: 1,
	}
	if pair {
		process.Env = append(process.Env, "CT1_HA_PAIR=1", "CT1_HA_PEER_SERVER_ID=pair-b", "CT1_HA_PEER_HTTP_PORT="+port(2), "CT1_HA_PEER_GRPC_PORT="+port(3), "CT1_HA_CLUSTER_ID="+ct1NodeClusterID)
		for i, id := range []string{"pair-a", "pair-b"} {
			config.Endpoints = append(config.Endpoints, &commonpb.DeviceEndpoint{
				Id: id, DeviceKey: "ct1:" + ct1NodeClusterID, MemberIds: []string{"pair-a", "pair-b"},
				Host: "127.0.0.1", Port: ports[2*i], UrlScheme: "http", RpcPort: ports[2*i+1],
			})
		}
	}
	process.Env = append(process.Env, env...)
	for _, l := range reserved {
		require.NoError(t, l.Close())
	}
	require.NoError(t, process.Start())
	t.Cleanup(func() { _ = process.Process.Kill(); _ = process.Wait() })
	listening := ports[:2]
	if pair {
		listening = ports
	}
	for _, p := range listening {
		require.Eventually(t, func() bool {
			conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(p))), time.Second)
			if err != nil {
				return false
			}
			return conn.Close() == nil
		}, 20*time.Second, 50*time.Millisecond, "the simulator did not listen on port %d", p)
	}

	manager := plugins.NewManager(&plugins.Config{
		Enabled: true, PluginsDir: pluginDir, MaxStartupTimeSeconds: 10, ShutdownGracePeriodSeconds: 5, LogLevel: "error",
	})
	t.Cleanup(func() { require.NoError(t, manager.Shutdown(context.Background())) })
	require.NoError(t, manager.LoadPlugins(t.Context()))
	plugin, ok := manager.GetPluginByDriverName("proto")
	require.True(t, ok)

	artifacts := &ct1NodeArtifacts{files: map[string][]byte{}, granted: map[string][]control.ArtifactExpectation{}}
	path, handler := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(artifacts)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	gateway := httptest.NewServer(mux)
	t.Cleanup(gateway.Close)

	link := &ct1NodeLink{
		t:         t,
		run:       &RunCmd{driverGetter: fakeDriverGetter{d: plugin.Driver}, minerSecrets: nodeSecretProvider{}, firmwareTempRoot: t.TempDir()},
		client:    fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(gateway.Client(), gateway.URL),
		artifacts: artifacts,
	}
	config.Sender, config.WriteGate = link, link.writeGate
	miner, err := remotenode.New(config)
	require.NoError(t, err)
	info := sdk.DeviceInfo{DeviceClass: sdk.DeviceClassContainer, Host: "127.0.0.1", Port: ports[0], URLScheme: "http", RPCPort: ports[1], SerialNumber: "pair-a"}
	return &ct1BehindNode{miner: miner, link: link, artifacts: artifacts, driver: plugin.Driver, info: info}
}

// ct1ReleaseKey returns a new release signing key and the path of its PEM
// public key.
func ct1ReleaseKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	keyFile := filepath.Join(t.TempDir(), "release.pub")
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600))
	return key, keyFile
}

// ct1SignedPackage returns a package of size bytes with its digest, and a
// release statement for it that key signs.
func ct1SignedPackage(t *testing.T, key *ecdsa.PrivateKey, size int, statement map[string]any) (pkg []byte, digest string, signed, signature []byte) {
	t.Helper()
	pkg = make([]byte, size)
	for i := range pkg {
		pkg[i] = byte(i*13 + 5)
	}
	sum := sha256.Sum256(pkg)
	digest = hex.EncodeToString(sum[:])
	statement["schema"], statement["sha256"], statement["size"], statement["issued_at"] = "frigate-release-statement/v1", digest, size, "2026-01-01T00:00:00Z"
	signed, err := json.Marshal(statement)
	require.NoError(t, err)
	statementDigest := sha256.Sum256(signed)
	signature, err = ecdsa.SignASN1(rand.Reader, key, statementDigest[:])
	require.NoError(t, err)
	return pkg, digest, signed, signature
}

func TestCT1SimulatorControllerSoftwareUpdateThroughNode(t *testing.T) {
	const job, targetVersion, actor = "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be1", "9.9.9", "fleet:tester@example.com"
	key, keyFile := ct1ReleaseKey(t)
	container := startCT1BehindNode(t, true, "CT1_TANKS=1", "CT1_MODULES_PER_TANK=1", "CT1_UPDATE_TRUSTED_KEY_FILES="+keyFile, "CT1_UPDATE_STEP=150ms")
	updater := container.miner.FrigateRelay()
	instances := []string{"pair-a", "pair-b"}

	// A read does not go through the write gate.
	var before []sdk.FrigateInstanceSupport
	var err error
	require.Eventually(t, func() bool {
		before, err = updater.FrigateUpdateSupport(t.Context())
		return err == nil && len(before) == 2 && before[0].Err == "" && before[1].Err == ""
	}, 20*time.Second, 100*time.Millisecond, "the controllers did not report update support: %v", err)
	require.Empty(t, container.link.gatedWrites())
	require.Len(t, before[0].TrustedReleaseKeyIDs, 1)

	// A signed release. The package is larger than two copy chunks.
	pkg, digest, statement, signature := ct1SignedPackage(t, key, 2<<20+4321, map[string]any{
		"package": "frigate", "version": targetVersion, "architecture": "arm64", "repository": "example/frigate", "min_installed_version": "",
	})
	ref := interfaces.FrigatePackageRef{ID: "5a0c7a50-7a2b-4f0e-9d44-2f6f3f3a1b10", Filename: "frigate_9.9.9_linux_arm64.deb", SizeBytes: int64(len(pkg)), SHA256: digest}
	container.artifacts.files[ref.ID] = pkg

	// Fleet begins each copy, then the node copies the package to both controllers.
	targets := make([]interfaces.FrigateStageTarget, 0, len(instances))
	fingerprints := map[string]string{}
	for _, instance := range instances {
		manifest := sdk.FrigateManifest{
			JobID: job, InstanceID: instance, ExpectedInstanceIDs: instances, TargetVersion: targetVersion, SHA256: digest, SizeBytes: int64(len(pkg)),
			Actor: actor, Coordination: sdk.FrigateCoordinationSelf, ReleaseStatement: statement, ReleaseSignature: signature,
		}
		progress, err := updater.FrigateStageBegin(t.Context(), instance, manifest)
		require.NoError(t, err)
		require.Zero(t, progress.NextOffset)
		fingerprints[instance] = progress.ManifestSHA256
		targets = append(targets, interfaces.FrigateStageTarget{InstanceID: instance, Manifest: manifest, ManifestSHA256: progress.ManifestSHA256})
	}
	outcomes, err := interfaces.FrigatePackageStager(container.miner).FrigateStagePackage(t.Context(), ref, targets)
	require.NoError(t, err)
	require.Len(t, outcomes, 2)
	for i, outcome := range outcomes {
		require.NoError(t, outcome.Err, instances[i])
		require.Equal(t, sdk.FrigateStageCommit{JobID: job, BytesStaged: int64(len(pkg)), SHA256: digest}, outcome.Commit, instances[i])
	}
	require.Equal(t, 1, container.artifacts.downloadsOf(pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE), "one download serves both controllers")

	// A second copy command finds both packages complete and sends no bytes.
	for i := range targets {
		progress, err := updater.FrigateStageBegin(t.Context(), targets[i].InstanceID, targets[i].Manifest)
		require.NoError(t, err)
		require.EqualValues(t, len(pkg), progress.NextOffset, "the controller keeps the staged package")
	}

	rollout, err := updater.FrigateUpdateStart(t.Context(), sdk.FrigateStartRequest{
		JobID: job, TargetVersion: targetVersion, ExpectedInstanceIDs: instances, IdempotencyKey: "start-" + job, ExpectedManifestSHA256: fingerprints, Actor: actor,
	})
	require.NoError(t, err)
	require.Equal(t, digest, rollout.PackageSHA256)

	// The controllers stop Frigate during the update. Status stays available.
	maintenance := false
	require.Eventually(t, func() bool {
		status, err := updater.FrigateUpdateStatus(t.Context(), job)
		if err != nil {
			return false
		}
		maintenance = maintenance || status.ServedByMaintenance
		return status.Rollout.Phase == sdk.FrigatePhaseSucceeded
	}, 40*time.Second, 50*time.Millisecond)
	require.True(t, maintenance, "status was never served by the maintenance responder")
	after, err := updater.FrigateUpdateSupport(t.Context())
	require.NoError(t, err)
	require.Len(t, after, 2)
	for _, instance := range after {
		require.Equal(t, targetVersion, instance.RunningVersion, instance.InstanceID)
	}

	// A refusal from the controllers reaches Fleet unchanged.
	_, err = updater.FrigateUpdateStatus(t.Context(), "8b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be8")
	refusal := sdk.AsFrigateRefusal(err)
	require.NotNil(t, refusal, "%v", err)
	require.Equal(t, "UPDATE_NOT_FOUND", refusal.Reason)
	require.Equal(t, codes.NotFound, refusal.Code)

	// Each change went through the write gate as a controller software update.
	writes := container.link.gatedWrites()
	require.NotEmpty(t, writes)
	for _, kind := range writes {
		require.Equal(t, commandtype.FrigateServerUpdate, kind)
	}
	require.Len(t, writes, 6, "two begins, one copy, two begins, one start")
}

func TestCT1SimulatorProtoOSPackageUpdateThroughNode(t *testing.T) {
	const tag, version, actor = "v0.6.2-internal.14", "0.6.2~internal.14", "fleet:tester@example.com"
	frigateKey, frigateKeyFile := ct1ReleaseKey(t)
	protoOSKey, protoOSKeyFile := ct1ReleaseKey(t)
	container := startCT1BehindNode(t, true, "CT1_TANKS=1", "CT1_MODULES_PER_TANK=1", "CT1_UPDATE_STEP=150ms",
		"CT1_UPDATE_TRUSTED_KEY_FILES="+frigateKeyFile, "CT1_PROTOOS_TRUSTED_KEY_FILES="+protoOSKeyFile)
	updater := container.miner.FrigateRelay()
	instances := []string{"pair-a", "pair-b"}

	var supports []sdk.FrigateInstanceSupport
	var err error
	require.Eventually(t, func() bool {
		supports, err = updater.FrigateUpdateSupport(t.Context())
		return err == nil && len(supports) == 2 && supports[0].Err == "" && supports[1].Err == ""
	}, 20*time.Second, 100*time.Millisecond, "the controllers did not report update support: %v", err)
	for _, s := range supports {
		require.True(t, s.ProtoOS.SelfSupported && s.RemoteTriggerAllowed, s.InstanceID)
		require.Contains(t, s.ProtoOS.ReleaseRepositories, "btc-mining/proto-fleet-internal")
	}

	// stage begins a copy on each controller and then copies the package to
	// all of them with one stage command, as Fleet does.
	stage := func(job, tag string, key *ecdsa.PrivateKey, on []string) {
		t.Helper()
		pkg, digest, statement, signature := ct1SignedPackage(t, key, 1<<20+777, map[string]any{
			"package": "proto-os", "version": sdk.ProtoOSDebianVersion(tag), "architecture": "all",
			"repository": "btc-mining/proto-fleet-internal", "min_installed_version": "",
		})
		ref := interfaces.FrigatePackageRef{ID: "pkg-" + job, Filename: sdk.ProtoOSAssetName(tag), SizeBytes: int64(len(pkg)), SHA256: digest}
		container.artifacts.files[ref.ID] = pkg
		targets := make([]interfaces.FrigateStageTarget, 0, len(on))
		for _, instance := range on {
			manifest := sdk.FrigateManifest{
				JobID: job, InstanceID: instance, ExpectedInstanceIDs: instances, ReleaseTag: tag, AssetName: sdk.ProtoOSAssetName(tag),
				TargetVersion: sdk.ProtoOSDebianVersion(tag), SHA256: digest, SizeBytes: int64(len(pkg)), Actor: actor,
				Coordination: sdk.FrigateCoordinationSelf, Package: sdk.FrigatePackageProtoOS, ReleaseStatement: statement, ReleaseSignature: signature,
			}
			progress, err := updater.FrigateStageBegin(t.Context(), instance, manifest)
			require.NoError(t, err)
			targets = append(targets, interfaces.FrigateStageTarget{InstanceID: instance, Manifest: manifest, ManifestSHA256: progress.ManifestSHA256})
		}
		outcomes, err := interfaces.FrigatePackageStager(container.miner).FrigateStagePackage(t.Context(), ref, targets)
		require.NoError(t, err)
		require.Len(t, outcomes, len(on))
		for i, outcome := range outcomes {
			require.NoError(t, outcome.Err, on[i])
			require.Equal(t, sdk.FrigateStageCommit{JobID: job, BytesStaged: int64(len(pkg)), SHA256: digest}, outcome.Commit, on[i])
		}
	}
	// finished waits until the controllers in on report job in a final state.
	finished := func(job string, on []string) []sdk.FrigateInstanceSupport {
		t.Helper()
		var done []sdk.FrigateInstanceSupport
		require.Eventually(t, func() bool {
			supports, err = updater.FrigateUpdateSupport(t.Context())
			if err != nil {
				return false
			}
			done = done[:0]
			for _, s := range supports {
				state := s.ProtoOS.Job.State
				if slices.Contains(on, s.InstanceID) && s.ProtoOS.Job.JobID == job && (state == sdk.FrigateProtoOSJobSucceeded || state == sdk.FrigateProtoOSJobFailed) {
					done = append(done, s)
				}
			}
			return len(done) == len(on)
		}, 40*time.Second, 50*time.Millisecond, "the controllers did not finish job %s: %+v", job, supports)
		return done
	}

	// Each controller installs its copy when the copy is committed. No Start.
	const job = "d1e2f3a4-0000-4000-8000-000000000001"
	stage(job, tag, protoOSKey, instances)
	require.Equal(t, 1, container.artifacts.downloadsOf(pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE), "one download serves both controllers")
	for _, s := range finished(job, instances) {
		require.Equal(t, sdk.FrigateProtoOSJobSucceeded, s.ProtoOS.Job.State, "%s: %s", s.InstanceID, s.ProtoOS.Job.Message)
		require.Equal(t, version, s.ProtoOS.Job.TargetVersion, s.InstanceID)
		require.Equal(t, version, s.ProtoOS.InstalledVersion, s.InstanceID)
		require.Equal(t, sdk.FrigateProtoOSBundle{Source: sdk.FrigateProtoOSBundlePackage, ReleaseTag: tag}, s.ProtoOS.Served, s.InstanceID)
	}

	// A ProtoOS package that the Frigate release key signs is refused, and the
	// controller keeps the installed release.
	const refusedJob = "d1e2f3a4-0000-4000-8000-000000000002"
	stage(refusedJob, "v0.6.3", frigateKey, instances[:1])
	refused := finished(refusedJob, instances[:1])[0]
	require.Equal(t, sdk.FrigateProtoOSJobFailed, refused.ProtoOS.Job.State)
	require.Contains(t, refused.ProtoOS.Job.Message, "UPDATE_SIGNATURE_INVALID")
	require.Equal(t, version, refused.ProtoOS.InstalledVersion)
	require.Equal(t, tag, refused.ProtoOS.Served.ReleaseTag)

	// Each change went through the write gate as a controller software update.
	writes := container.link.gatedWrites()
	for _, kind := range writes {
		require.Equal(t, commandtype.FrigateServerUpdate, kind)
	}
	require.Len(t, writes, 5, "two begins and one copy, then one begin and one copy")
}

// The node refuses a package above its download limit before it downloads
// anything. Fleet's handle returns that refusal as an invalid argument, which
// tells the update runner that the same stage cannot succeed later.
func TestCT1SimulatorControllerSoftwarePackageAboveTheNodeLimitThroughNode(t *testing.T) {
	const job, size = "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be1", maxFleetNodeFirmwareArtifactSizeBytes + 1<<20
	container := startCT1BehindNode(t, false)
	digest := hex.EncodeToString(make([]byte, sha256.Size))
	ref := interfaces.FrigatePackageRef{ID: "5a0c7a50-7a2b-4f0e-9d44-2f6f3f3a1b10", Filename: "frigate_9.9.9_linux_arm64.deb", SizeBytes: size, SHA256: digest}
	manifest := sdk.FrigateManifest{
		JobID: job, InstanceID: "pair-a", ExpectedInstanceIDs: []string{"pair-a"}, TargetVersion: "9.9.9", SHA256: digest, SizeBytes: size,
		Actor: "fleet:tester@example.com", Coordination: sdk.FrigateCoordinationSelf,
	}

	_, err := interfaces.FrigatePackageStager(container.miner).FrigateStagePackage(t.Context(), ref,
		[]interfaces.FrigateStageTarget{{InstanceID: "pair-a", Manifest: manifest, ManifestSHA256: digest}})

	require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
	require.ErrorContains(t, err, "exceeds fleet node limit")
	require.Zero(t, container.artifacts.downloadsOf(pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE))
}

func TestCT1SimulatorHashboardFirmwareUpdateThroughNode(t *testing.T) {
	const version = "7.0.0"
	container := startCT1BehindNode(t, false)
	archive, err := os.ReadFile(firmwaretest.Archive(t, map[string][]byte{"prod.bin": firmwaretest.Image(t, "prod", "f1", 7)}))
	require.NoError(t, err)
	sum := sha256.Sum256(archive)
	file := sdk.FirmwareFile{ID: "firmware-7", Filename: "hashboard-firmware-7.0.0.zip", Size: int64(len(archive)), SHA256: hex.EncodeToString(sum[:]), RequestID: "batch-1:ct1-behind-node"}
	container.artifacts.files[file.ID] = archive

	// boardVersions reads the firmware version of each hashboard from the plugin.
	boardVersions := func() map[string]int {
		t.Helper()
		handle, err := container.driver.NewDevice(t.Context(), "ct1-firmware-check", container.info, sdk.SecretBundle{})
		require.NoError(t, err)
		defer handle.Device.Close(context.Background())
		metrics, err := handle.Device.Status(t.Context())
		require.NoError(t, err)
		versions := map[string]int{}
		for _, tank := range metrics.Container.Tanks {
			for _, module := range tank.Modules {
				for _, board := range module.Hashboards {
					var metadata struct {
						FirmwareVersion string `json:"firmwareVersion"`
					}
					require.NoError(t, json.Unmarshal(board.MetadataJSON, &metadata))
					versions[metadata.FirmwareVersion]++
				}
			}
		}
		return versions
	}
	const boards = 6 * 16 * sdk.CT1HashboardsPerModule
	before := boardVersions()
	require.Zero(t, before[version], "no board runs the new firmware before the update")

	// No update ran: the container has no firmware task to report.
	_, err = container.miner.GetFirmwareUpdateStatus(t.Context())
	require.ErrorContains(t, err, "no CT1 firmware job")

	require.NoError(t, container.miner.FirmwareUpdate(t.Context(), file))
	require.Equal(t, 1, container.artifacts.downloadsOf(pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_FIRMWARE_PAYLOAD), "the node got the archive from Fleet")
	// A retry of the same Fleet command resumes its task and does not flash again.
	require.NoError(t, container.miner.FirmwareUpdate(t.Context(), file))
	require.Equal(t, []commandtype.Type{commandtype.FirmwareUpdate, commandtype.FirmwareUpdate}, container.link.gatedWrites())

	// Each status command uses a new device handle on the node.
	var status *sdk.FirmwareUpdateStatus
	require.Eventually(t, func() bool {
		status, err = container.miner.GetFirmwareUpdateStatus(t.Context())
		return err == nil && status.State != "installing"
	}, 30*time.Second, 100*time.Millisecond)
	require.Equal(t, "completed", status.State, "%+v", status)
	require.Equal(t, map[string]int{version: boards}, boardVersions(), "each hashboard runs the new firmware")
}
