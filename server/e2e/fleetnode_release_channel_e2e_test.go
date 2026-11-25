//go:build e2e

package e2e

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/block/proto-fleet/sdk/mdk/firmware/firmwaretest"
	"github.com/block/proto-fleet/sdk/mdk/psucap/psucaptest"
	containersv1 "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/generated/grpc/containers/v1/containersv1connect"
	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	fleetnodeadminv1 "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	frigateupdatev1 "github.com/block/proto-fleet/server/generated/grpc/frigateupdate/v1"
	"github.com/block/proto-fleet/server/generated/grpc/frigateupdate/v1/frigateupdatev1connect"
	pairingv1 "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	rolloutv1 "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
	"github.com/block/proto-fleet/server/generated/grpc/rollout/v1/rolloutv1connect"
	"github.com/block/proto-fleet/server/internal/domain/frigaterelease"
)

// Addresses and services of docker-compose.fleetnode-ui-test.yaml. The miner
// network is internal and only the node joins it, so fleet-api can reach
// these devices only through the node.
const (
	fleetNodeRigIP          = "10.90.0.10"
	fleetNodeCT1IP          = "10.90.0.30"
	fleetNodeScanPort       = "8080"
	fleetNodeCT1PeerPort    = "8082"
	fleetNodeRigService     = "ui-test-proto-rig"
	fleetNodeCT1Service     = "ui-test-ct1"
	fleetNodeFleetAPI       = "fleet-api"
	fleetNodeMinerNetwork   = "fleetnode-miner-network"
	fleetNodeCT1HTTPPort    = "8081"
	fleetNodeChannelPrefix  = "fleetnode-e2e-"
	fleetNodeDefaultTimeout = 10 * time.Minute
)

type nodeDevice struct {
	identifier   string
	manufacturer string
	model        string
}

type fleetNodeRun struct {
	project    string
	runID      int64
	session    *http.Client
	nodeID     int64
	rig        nodeDevice
	ct1        nodeDevice
	channelID  int64
	rigVersion string
	admin      fleetnodeadminv1connect.FleetNodeAdminServiceClient
	fleet      fleetmanagementv1connect.FleetManagementServiceClient
	containers containersv1connect.ContainerServiceClient
	rollouts   rolloutv1connect.RolloutServiceClient
	frigate    frigateupdatev1connect.FrigateUpdateServiceClient
	timeout    time.Duration
}

// TestFleetNodeReleaseChannelFirmware drives release-channel firmware updates
// for devices owned by a Fleet Node, through the public API only. It needs the
// stack from `just fleetnode-channel-e2e`, which sets
// FLEET_E2E_FLEETNODE_COMPOSE_PROJECT.
func TestFleetNodeReleaseChannelFirmware(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}
	project := os.Getenv("FLEET_E2E_FLEETNODE_COMPOSE_PROJECT")
	if project == "" {
		t.Skip("FLEET_E2E_FLEETNODE_COMPOSE_PROJECT is not set; run `just fleetnode-channel-e2e`")
	}
	timeout := fleetNodeDefaultTimeout
	if value := os.Getenv("FLEET_E2E_ROLLOUT_TIMEOUT"); value != "" {
		parsed, err := time.ParseDuration(value)
		require.NoError(t, err, "FLEET_E2E_ROLLOUT_TIMEOUT must be a Go duration")
		timeout = parsed
	}

	ctx := context.Background()
	waitForFleetAPIHealth(t, ctx, 90*time.Second)
	// Fleet node administration accepts only a user session, so every call
	// uses the session, as the UI does.
	session := sessionHTTPClient(t, ctx, testUsername, testPassword)
	run := &fleetNodeRun{
		project:    project,
		runID:      time.Now().Unix(),
		session:    session,
		admin:      fleetnodeadminv1connect.NewFleetNodeAdminServiceClient(session, fleetAPIURL),
		fleet:      fleetmanagementv1connect.NewFleetManagementServiceClient(session, fleetAPIURL),
		containers: containersv1connect.NewContainerServiceClient(session, fleetAPIURL),
		rollouts:   rolloutv1connect.NewRolloutServiceClient(session, fleetAPIURL),
		frigate:    frigateupdatev1connect.NewFrigateUpdateServiceClient(session, fleetAPIURL),
		timeout:    timeout,
	}
	t.Logf("run id %d, rollout timeout %s", run.runID, timeout)

	if !t.Run("SetupNodeOwnsBothDevices", run.testSetup) {
		t.Fatal("the node does not own both devices; later steps would not prove the node path")
	}
	t.Run("NodeTelemetryReachesFleet", run.testTelemetry)
	t.Run("RigFirmware", run.testRigFirmware)
	t.Run("ContainerHashboardFirmware", run.testContainerHashboardFirmware)
	t.Run("ContainerPSUFirmware", run.testContainerPSUFirmware)
	t.Run("ContainerControllerSoftware", run.testContainerControllerSoftware)
	t.Run("RigStaysOnTargetAfterNodeRescan", run.testRescanKeepsRigOnTarget)
}

func (r *fleetNodeRun) testSetup(t *testing.T) {
	ctx := t.Context()
	nodeName := envOrDefault("FLEET_E2E_FLEETNODE_NAME", "ui-test-fleetnode")

	require.False(t, r.containerJoins(t, fleetNodeFleetAPI, fleetNodeMinerNetwork),
		"fleet-api must not join the miner network, or the test cannot prove the node path")

	var node *fleetnodeadminv1.FleetNodeSummary
	require.Eventually(t, func() bool {
		resp, err := r.admin.ListFleetNodes(ctx, connect.NewRequest(&fleetnodeadminv1.ListFleetNodesRequest{}))
		if err != nil {
			t.Logf("list fleet nodes: %v", err)
			return false
		}
		for _, n := range resp.Msg.GetFleetNodes() {
			if n.GetName() == nodeName && n.GetEnrollmentStatus() == fleetnodeadminv1.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_CONFIRMED {
				node = n
				return n.GetControlStreamConnected()
			}
		}
		return false
	}, 2*time.Minute, 2*time.Second, "fleet node %q must be confirmed and connected", nodeName)
	r.nodeID = node.GetFleetNodeId()
	t.Logf("node %d %q connected, version %s", r.nodeID, node.GetName(), node.GetVersion())

	ips := r.discoverThroughNode(t)
	owned, err := r.nodeDevices(ctx, ips)
	require.NoError(t, err)
	if owned[fleetNodeRigIP] == nil || owned[fleetNodeCT1IP] == nil {
		r.pairThroughNode(t, ips)
	}

	var rig, ct1 *fleetnodeadminv1.FleetNodeDeviceSummary
	require.Eventually(t, func() bool {
		owned, err := r.nodeDevices(ctx, ips)
		if err != nil {
			t.Logf("list fleet node devices: %v", err)
			return false
		}
		rig, ct1 = owned[fleetNodeRigIP], owned[fleetNodeCT1IP]
		return rig != nil && ct1 != nil
	}, time.Minute, 2*time.Second, "the node must own the rig at %s and the container at %s", fleetNodeRigIP, fleetNodeCT1IP)

	for _, d := range []*fleetnodeadminv1.FleetNodeDeviceSummary{rig, ct1} {
		require.Equal(t, r.nodeID, d.GetFleetNodeId(), "device %s must be owned by node %d", d.GetDeviceIdentifier(), r.nodeID)
	}
	r.rig = nodeDevice{identifier: rig.GetDeviceIdentifier(), manufacturer: rig.GetManufacturer(), model: rig.GetModel()}
	r.ct1 = nodeDevice{identifier: ct1.GetDeviceIdentifier(), manufacturer: ct1.GetManufacturer(), model: ct1.GetModel()}
	t.Logf("rig %+v", r.rig)
	t.Logf("container %+v", r.ct1)
	require.Equal(t, "CT1", r.ct1.model, "the container must report the CT1 model")
}

// testTelemetry waits until Fleet shows live data for both devices: the miner
// list for the rig and the container list for the container. A rollout
// snapshots each target's health when it starts, so it must not start before
// the first telemetry arrives.
func (r *fleetNodeRun) testTelemetry(t *testing.T) {
	r.requireDevices(t)
	ctx := t.Context()
	require.Eventually(t, func() bool {
		resp, err := r.fleet.ListMinerStateSnapshots(ctx, connect.NewRequest(&fleetmanagementv1.ListMinerStateSnapshotsRequest{PageSize: 1000}))
		if err != nil {
			t.Logf("list miners: %v", err)
			return false
		}
		for _, m := range resp.Msg.GetMiners() {
			if m.GetDeviceIdentifier() == r.rig.identifier && len(m.GetHashrate()) > 0 {
				t.Logf("rig telemetry: status %s, firmware %q", m.GetDeviceStatus(), m.GetFirmwareVersion())
				return true
			}
		}
		return false
	}, 3*time.Minute, 5*time.Second, "the miner list must show telemetry for the node-owned rig")
	require.Eventually(t, func() bool {
		resp, err := r.containers.ListContainers(ctx, connect.NewRequest(&containersv1.ListContainersRequest{IncludeUnassigned: true}))
		if err != nil {
			t.Logf("list containers: %v", err)
			return false
		}
		for _, c := range resp.Msg.GetContainers() {
			if c.GetDeviceIdentifier() == r.ct1.identifier && c.GetMetricsAt() != nil {
				t.Logf("container telemetry: metrics at %s, hashrate %.0f H/s", c.GetMetricsAt().AsTime().Format(time.RFC3339), c.GetHashrateHs())
				return true
			}
		}
		return false
	}, 3*time.Minute, 5*time.Second, "the container list must show telemetry for the node-owned container")
}

// nodeDevices maps the IP of each discovered device to the node's pairing for
// it, so a device counts only when Fleet records this node as its owner.
func (r *fleetNodeRun) nodeDevices(ctx context.Context, ips map[string]string) (map[string]*fleetnodeadminv1.FleetNodeDeviceSummary, error) {
	pairs, err := r.admin.ListFleetNodeDevices(ctx, connect.NewRequest(&fleetnodeadminv1.ListFleetNodeDevicesRequest{FleetNodeId: r.nodeID}))
	if err != nil {
		return nil, fmt.Errorf("list fleet node devices: %w", err)
	}
	out := map[string]*fleetnodeadminv1.FleetNodeDeviceSummary{}
	for _, p := range pairs.Msg.GetPairs() {
		if ip, ok := ips[p.GetDeviceIdentifier()]; ok {
			out[ip] = p
		}
	}
	return out, nil
}

// discoverThroughNode maps each device identifier the node finds to its IP.
// The fake CT1 serves its second controller on another port of the same IP.
// Fleet must find both controllers before it pairs the container, or a
// controller software update cannot start.
func (r *fleetNodeRun) discoverThroughNode(t *testing.T) map[string]string {
	t.Helper()
	stream, err := r.admin.DiscoverOnFleetNode(t.Context(), connect.NewRequest(&fleetnodeadminv1.DiscoverOnFleetNodeRequest{
		FleetNodeId: r.nodeID,
		Request: &pairingv1.DiscoverRequest{Mode: &pairingv1.DiscoverRequest_IpList{IpList: &pairingv1.IPListModeRequest{
			IpAddresses: []string{fleetNodeRigIP, fleetNodeCT1IP},
			Ports:       []string{fleetNodeScanPort, fleetNodeCT1PeerPort},
		}}},
	}))
	require.NoError(t, err, "discover on fleet node")
	ips := map[string]string{}
	for stream.Receive() {
		resp := stream.Msg().GetResponse()
		if w := resp.GetWarning(); w != "" {
			t.Logf("discovery warning: %s", w)
		}
		for _, d := range resp.GetDevices() {
			ips[d.GetDeviceIdentifier()] = d.GetIpAddress()
		}
	}
	require.NoError(t, stream.Err(), "discover on fleet node stream")
	return ips
}

func (r *fleetNodeRun) pairThroughNode(t *testing.T, ips map[string]string) {
	t.Helper()
	var identifiers []string
	for id, ip := range ips {
		if ip == fleetNodeRigIP || ip == fleetNodeCT1IP {
			identifiers = append(identifiers, id)
		}
	}
	require.Len(t, identifiers, 2, "the node must discover the rig and the container")
	stream, err := r.admin.PairDiscoveredDevicesOnFleetNode(t.Context(), connect.NewRequest(&fleetnodeadminv1.PairDiscoveredDevicesOnFleetNodeRequest{
		FleetNodeId:       r.nodeID,
		DeviceIdentifiers: identifiers,
	}))
	require.NoError(t, err, "pair through fleet node")
	for stream.Receive() {
		for _, result := range stream.Msg().GetResults() {
			t.Logf("pair %s: %s %s", result.GetDeviceIdentifier(), result.GetPairingStatus(), result.GetError())
			assert.Contains(t, []fleetmanagementv1.PairingStatus{
				fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED,
				fleetmanagementv1.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
			}, result.GetPairingStatus(), "device %s must pair through the node", result.GetDeviceIdentifier())
		}
	}
	require.NoError(t, stream.Err(), "pair through fleet node stream")
}

func (r *fleetNodeRun) testRigFirmware(t *testing.T) {
	r.requireDevices(t)
	version := fmt.Sprintf("4.%d.0", r.runID)
	payload := append([]byte("firmware_version="+version+"\n"), randomBytes(t, 64<<10)...)
	fileID := r.uploadFirmware(t, fmt.Sprintf("fleetnode-e2e-rig-%s.swu", version), payload, map[string]string{
		"target_manufacturer": r.rig.manufacturer,
		"target_model":        r.rig.model,
		"firmware_version":    version,
	})

	rollout := r.applyFirmware(t, &rolloutv1.FirmwareAssignment{Manufacturer: r.rig.manufacturer, Model: r.rig.model, FirmwareFileId: fileID})
	r.assertRolloutVerifies(t, rollout, r.rig)
	assert.Equal(t, version, r.rigReportedVersion(t), "the rig itself must run the assigned firmware")
	if !t.Failed() {
		r.rigVersion = version
	}
}

// testRescanKeepsRigOnTarget scans the node's network again, as an operator
// does to find new miners. The rig still runs the assigned firmware, so the
// channel must keep it on target and must not send the firmware again.
func (r *fleetNodeRun) testRescanKeepsRigOnTarget(t *testing.T) {
	require.NotEmpty(t, r.rigVersion, "the rig must first converge on the channel's firmware")
	ctx := t.Context()
	r.discoverThroughNode(t)
	// Enforcement runs every 15 s; three ticks give it time to react.
	assert.Never(t, func() bool {
		miners, err := r.rollouts.ListReleaseChannelMiners(ctx, connect.NewRequest(&rolloutv1.ListReleaseChannelMinersRequest{
			ChannelId: r.channelID, Manufacturer: r.rig.manufacturer, Model: r.rig.model,
		}))
		if err != nil {
			t.Logf("list channel miners: %v", err)
			return false
		}
		for _, m := range miners.Msg.GetMiners() {
			if m.GetDeviceIdentifier() == r.rig.identifier && m.GetFirmwareVersion() != r.rigVersion {
				t.Logf("after the rescan Fleet reports rig firmware %q: %s", m.GetFirmwareVersion(), compactJSON(m))
				return true
			}
		}
		active, err := r.rollouts.ListRollouts(ctx, connect.NewRequest(&rolloutv1.ListRolloutsRequest{
			ChannelId: r.channelID, Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_ACTIVE,
		}))
		if err != nil {
			t.Logf("list rollouts: %v", err)
			return false
		}
		for _, ro := range active.Msg.GetRollouts() {
			if ro.GetModel() == r.rig.model {
				t.Logf("after the rescan a rollout sends the rig firmware again: %s", compactJSON(ro))
				return true
			}
		}
		return false
	}, 45*time.Second, 5*time.Second, "a node rescan must keep the rig on %s without a new rollout", r.rigVersion)
}

func (r *fleetNodeRun) testContainerHashboardFirmware(t *testing.T) {
	r.requireDevices(t)
	major := uint32(r.runID%1_000_000) + 1000
	version := fmt.Sprintf("%d.0.0", major)
	archive, err := os.ReadFile(firmwaretest.Archive(t, map[string][]byte{"prod.bin": firmwaretest.Image(t, "prod", "f1", major)}))
	require.NoError(t, err)
	fileID := r.uploadFirmware(t, fmt.Sprintf("hashboard-firmware-%s.zip", version), archive, map[string]string{
		"target_manufacturer": r.ct1.manufacturer,
		"target_model":        r.ct1.model,
		"firmware_version":    version,
		"hashboard_build":     "prod",
	})

	rollout := r.applyFirmware(t, &rolloutv1.FirmwareAssignment{Manufacturer: r.ct1.manufacturer, Model: r.ct1.model, FirmwareFileId: fileID})
	r.assertRolloutVerifies(t, rollout, r.ct1)
	counts := r.ct1BoardVersions(t, func(b ct1Board) string { return b.FirmwareVersion })
	assert.Equal(t, []string{version}, slices.Sorted(maps.Keys(counts)), "every hashboard must run the assigned firmware: %v", counts)
}

func (r *fleetNodeRun) testContainerPSUFirmware(t *testing.T) {
	r.requireDevices(t)
	version := fmt.Sprintf("app=%d;bl=1110", r.runID)
	capsule, err := psucaptest.Build(psucaptest.Options{Version: version})
	require.NoError(t, err)
	fileID := r.uploadFirmware(t, fmt.Sprintf("fleetnode-e2e-%d.psucap", r.runID), capsule, map[string]string{
		"target_manufacturer": r.ct1.manufacturer,
		"target_model":        r.ct1.model,
		"firmware_version":    version,
		"component":           "psu",
	})

	rollout := r.applyFirmware(t, &rolloutv1.FirmwareAssignment{Manufacturer: r.ct1.manufacturer, Model: r.ct1.model, FirmwareFileId: fileID, Component: "psu"})
	r.assertRolloutVerifies(t, rollout, r.ct1)
	counts := r.ct1BoardVersions(t, func(b ct1Board) string { return b.PSUIdentity.FirmwareVersion })
	assert.Equal(t, []string{version}, slices.Sorted(maps.Keys(counts)), "the PSU of every hashboard must run the assigned firmware: %v", counts)
}

func (r *fleetNodeRun) testContainerControllerSoftware(t *testing.T) {
	r.requireDevices(t)
	before := r.controllerSupport(t)
	require.Len(t, before.GetInstances(), 2, "Fleet must know both controllers of the HA pair")
	keyPath := os.Getenv("FLEET_E2E_CT1_RELEASE_KEY")
	require.NotEmpty(t, keyPath, "FLEET_E2E_CT1_RELEASE_KEY is not set; run `just fleetnode-channel-e2e`")
	version := fmt.Sprintf("2.%d.0", r.runID)
	versions := runningVersions(before)
	t.Logf("controller versions before the update: %v", versions)
	require.NotContains(t, slices.Collect(maps.Values(versions)), version, "the controllers must run another version before the update")

	packageID := r.uploadControllerPackage(t, keyPath, version, before.GetInstances()[0].GetArchitecture())
	started, err := r.frigate.StartUpdate(t.Context(), connect.NewRequest(&frigateupdatev1.StartUpdateRequest{
		DeviceIdentifier: r.ct1.identifier, PackageId: packageID, IdempotencyKey: fmt.Sprintf("%s%d", fleetNodeChannelPrefix, r.runID),
	}))
	require.NoError(t, err, "start the controller software update")
	job := r.waitForControllerUpdate(t, started.Msg.GetJob())
	require.Equal(t, frigateupdatev1.FrigateUpdateState_FRIGATE_UPDATE_STATE_SUCCEEDED, job.GetState(),
		"the controller software update must succeed: %s %s", job.GetErrorCode(), job.GetErrorMessage())

	after := runningVersions(r.controllerSupport(t))
	t.Logf("controller versions after the update: %v", after)
	want := map[string]string{}
	for id := range versions {
		want[id] = version
	}
	assert.Equal(t, want, after, "each controller must run the new version")
}

// controllerSupport reads the update readiness of the container's controllers
// and requires that an update can start.
func (r *fleetNodeRun) controllerSupport(t *testing.T) *frigateupdatev1.GetSupportResponse {
	t.Helper()
	resp, err := r.frigate.GetSupport(t.Context(), connect.NewRequest(&frigateupdatev1.GetSupportRequest{DeviceIdentifier: r.ct1.identifier}))
	require.NoError(t, err, "controller software update support must be readable for a node-owned container")
	t.Logf("support: %s", compactJSON(resp.Msg))
	require.NotEmpty(t, resp.Msg.GetInstances(), "the controllers must report their update readiness")
	for _, in := range resp.Msg.GetInstances() {
		require.Empty(t, in.GetError(), "controller %s must be readable", in.GetInstanceId())
	}
	require.True(t, resp.Msg.GetEligible(), "the container must be eligible for a controller software update: %s %s",
		resp.Msg.GetIneligibleCode(), resp.Msg.GetIneligibleReason())
	return resp.Msg
}

func runningVersions(support *frigateupdatev1.GetSupportResponse) map[string]string {
	out := map[string]string{}
	for _, in := range support.GetInstances() {
		out[in.GetInstanceId()] = in.GetRunningVersion()
	}
	return out
}

// uploadControllerPackage signs a release of version with the key at keyPath
// and uploads it with its public key, as an operator does in the Fleet UI.
func (r *fleetNodeRun) uploadControllerPackage(t *testing.T, keyPath, version, architecture string) string {
	t.Helper()
	keyPEM, err := os.ReadFile(keyPath)
	require.NoError(t, err, "read the release key that `just fleetnode-channel-e2e` makes")
	block, _ := pem.Decode(keyPEM)
	require.NotNil(t, block, "%s must hold a PEM key", keyPath)
	key, err := x509.ParseECPrivateKey(block.Bytes)
	require.NoError(t, err, "%s must hold a P-256 key", keyPath)
	publicKey, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)

	deb := debPackage(t, fmt.Sprintf("Package: %s\nVersion: %s\nArchitecture: %s\n", frigaterelease.PackageName, version, architecture))
	sum := sha256.Sum256(deb)
	statement, err := frigaterelease.MarshalCanonical(frigaterelease.Statement{
		Schema: frigaterelease.SchemaV1, Package: frigaterelease.PackageName, Version: version, Architecture: architecture,
		Repository: "btc-mining/frigate", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(deb)), IssuedAt: time.Now(),
	})
	require.NoError(t, err)
	digest := sha256.Sum256(statement)
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	require.NoError(t, err)

	call := func(method, path, contentRange string, body []byte, out any) {
		req, err := http.NewRequestWithContext(t.Context(), method, fleetAPIURL+"/api/v1/frigate-packages/upload/chunked"+path, bytes.NewReader(body))
		require.NoError(t, err)
		if contentRange != "" {
			req.Header.Set("Content-Range", contentRange)
		}
		resp, err := r.session.Do(req)
		require.NoError(t, err, "%s %s", method, req.URL.Path)
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s %s: %s", method, req.URL.Path, raw)
		if out != nil {
			require.NoError(t, json.Unmarshal(raw, out), "%s %s: %s", method, req.URL.Path, raw)
		}
	}
	filename := fmt.Sprintf("frigate_%s_%s.deb", version, architecture)
	initiate, err := json.Marshal(map[string]any{
		"filename": filename, "size": len(deb),
		"release_statement_b64":  base64.StdEncoding.EncodeToString(statement),
		"release_signature_b64":  base64.StdEncoding.EncodeToString(signature),
		"release_public_key_b64": base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKey})),
	})
	require.NoError(t, err)
	var upload struct {
		UploadID string `json:"upload_id"`
	}
	call(http.MethodPost, "", "", initiate, &upload)
	call(http.MethodPut, "/"+upload.UploadID, fmt.Sprintf("bytes 0-%d/%d", len(deb)-1, len(deb)), deb, nil)
	var completed struct {
		Package struct {
			ID           string `json:"id"`
			ReleaseKeyID string `json:"release_key_id"`
		} `json:"package"`
		Warning string `json:"warning"`
	}
	call(http.MethodPost, "/"+upload.UploadID+"/complete", "", nil, &completed)
	require.NotEmpty(t, completed.Package.ReleaseKeyID, "Fleet must verify the release signature with the uploaded key: %s", completed.Warning)
	t.Logf("uploaded %s (%d bytes) as package %s, release key %s", filename, len(deb), completed.Package.ID, completed.Package.ReleaseKeyID)
	return completed.Package.ID
}

// debPackage builds a Debian package that Fleet accepts: an ar archive whose
// gzip control archive holds control. The fake controllers do not unpack it.
func debPackage(t *testing.T, control string) []byte {
	t.Helper()
	var controlArchive bytes.Buffer
	gz := gzip.NewWriter(&controlArchive)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "./control", Mode: 0o644, Size: int64(len(control)), Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte(control))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	var deb bytes.Buffer
	deb.WriteString("!<arch>\n")
	for _, member := range []struct {
		name string
		data []byte
	}{
		{"debian-binary", []byte("2.0\n")},
		{"control.tar.gz", controlArchive.Bytes()},
		// More than two 1 MiB chunks, so the node copies in several chunks.
		{"data.tar.gz", randomBytes(t, 2<<20+4321)},
	} {
		fmt.Fprintf(&deb, "%-16s%-12d%-6d%-6d%-8s%-10d`\n", member.name+"/", 0, 0, 0, "100644", len(member.data))
		deb.Write(member.data)
		if len(member.data)%2 == 1 {
			deb.WriteByte('\n')
		}
	}
	return deb.Bytes()
}

// waitForControllerUpdate polls the job until it ends or the rollout timeout
// passes. It logs each change of the job state or the rollout phase.
func (r *fleetNodeRun) waitForControllerUpdate(t *testing.T, job *frigateupdatev1.UpdateJob) *frigateupdatev1.UpdateJob {
	t.Helper()
	pending := []frigateupdatev1.FrigateUpdateState{
		frigateupdatev1.FrigateUpdateState_FRIGATE_UPDATE_STATE_STAGING,
		frigateupdatev1.FrigateUpdateState_FRIGATE_UPDATE_STATE_STAGED,
		frigateupdatev1.FrigateUpdateState_FRIGATE_UPDATE_STATE_STARTING,
		frigateupdatev1.FrigateUpdateState_FRIGATE_UPDATE_STATE_RUNNING,
	}
	start := time.Now()
	logged := ""
	for {
		step := fmt.Sprintf("state %s, rollout phase %s", job.GetState(), job.GetRolloutPhase())
		if step != logged {
			t.Logf("%s (+%s) job %s: %s", time.Now().Format(time.TimeOnly), time.Since(start).Round(time.Second), job.GetJobId(), step)
			logged = step
		}
		if !slices.Contains(pending, job.GetState()) || time.Since(start) > r.timeout {
			t.Logf("job: %s", compactJSON(job))
			return job
		}
		time.Sleep(time.Second)
		resp, err := r.frigate.GetUpdate(t.Context(), connect.NewRequest(&frigateupdatev1.GetUpdateRequest{JobId: job.GetJobId()}))
		require.NoError(t, err, "get controller software update %s", job.GetJobId())
		job = resp.Msg.GetJob()
	}
}

func (r *fleetNodeRun) requireDevices(t *testing.T) {
	t.Helper()
	require.NotEmpty(t, r.rig.identifier, "setup must find the node-owned rig")
	require.NotEmpty(t, r.ct1.identifier, "setup must find the node-owned container")
}

// ensureChannel creates the run's channel over exactly the two node-owned
// devices. Channels from earlier runs claim the same devices, so it deletes
// them first.
func (r *fleetNodeRun) ensureChannel(t *testing.T) int64 {
	t.Helper()
	if r.channelID != 0 {
		return r.channelID
	}
	ctx := t.Context()
	list, err := r.rollouts.ListReleaseChannels(ctx, connect.NewRequest(&rolloutv1.ListReleaseChannelsRequest{PageSize: 1000}))
	require.NoError(t, err, "list release channels")
	for _, c := range list.Msg.GetChannels() {
		if strings.HasPrefix(c.GetName(), fleetNodeChannelPrefix) {
			_, err := r.rollouts.DeleteReleaseChannel(ctx, connect.NewRequest(&rolloutv1.DeleteReleaseChannelRequest{ChannelId: c.GetId()}))
			require.NoError(t, err, "delete channel %q from an earlier run", c.GetName())
		}
	}
	created, err := r.rollouts.CreateReleaseChannel(ctx, connect.NewRequest(&rolloutv1.CreateReleaseChannelRequest{
		Name:     fmt.Sprintf("%s%d", fleetNodeChannelPrefix, r.runID),
		Scope:    &rolloutv1.ReleaseChannelScope{DeviceIdentifiers: []string{r.rig.identifier, r.ct1.identifier}},
		Behavior: &rolloutv1.RolloutBehavior{Method: rolloutv1.RolloutMethod_ROLLOUT_METHOD_ALL_AT_ONCE},
	}))
	require.NoError(t, err, "create release channel")
	require.EqualValues(t, 2, created.Msg.GetChannel().GetMinerCount(), "the channel must cover exactly the two node-owned devices")
	r.channelID = created.Msg.GetChannel().GetId()
	return r.channelID
}

func (r *fleetNodeRun) applyFirmware(t *testing.T, assignment *rolloutv1.FirmwareAssignment) *rolloutv1.Rollout {
	t.Helper()
	channelID := r.ensureChannel(t)
	resp, err := r.rollouts.ApplyReleaseChannelFirmware(t.Context(), connect.NewRequest(&rolloutv1.ApplyReleaseChannelFirmwareRequest{
		ChannelId:   channelID,
		Assignments: []*rolloutv1.FirmwareAssignment{assignment},
	}))
	require.NoError(t, err, "apply %s %s %q firmware", assignment.GetManufacturer(), assignment.GetModel(), assignment.GetComponent())
	started := resp.Msg.GetStartedRollouts()
	require.Len(t, started, 1, "applying %s %s %q firmware must start one rollout", assignment.GetManufacturer(), assignment.GetModel(), assignment.GetComponent())
	t.Logf("rollout %d started for %s %s %q version %s", started[0].GetId(), started[0].GetManufacturer(), started[0].GetModel(), started[0].GetComponent(), started[0].GetFirmwareVersion())
	return started[0]
}

// assertRolloutVerifies waits for the rollout to finish and asserts the device
// verified the target. It does not stop the subtest, so the caller can still
// read the version from the device and tell a failed update from a failed
// verification.
func (r *fleetNodeRun) assertRolloutVerifies(t *testing.T, rollout *rolloutv1.Rollout, device nodeDevice) {
	t.Helper()
	ctx := t.Context()
	deadline := time.Now().Add(r.timeout)
	current := rollout
	for current.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_ACTIVE && time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		resp, err := r.rollouts.GetRollout(ctx, connect.NewRequest(&rolloutv1.GetRolloutRequest{RolloutId: rollout.GetId()}))
		require.NoError(t, err, "get rollout %d", rollout.GetId())
		current = resp.Msg.GetRollout()
	}
	r.dumpRollout(t, current, device)
	assert.Equal(t, rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED, current.GetStatus(),
		"rollout %d must complete within %s", rollout.GetId(), r.timeout)

	devices, err := r.rollouts.ListRolloutDevices(ctx, connect.NewRequest(&rolloutv1.ListRolloutDevicesRequest{RolloutId: rollout.GetId()}))
	require.NoError(t, err, "list rollout devices")
	var target *rolloutv1.RolloutDevice
	for _, d := range devices.Msg.GetDevices() {
		if d.GetDeviceIdentifier() == device.identifier {
			target = d
		}
	}
	if assert.NotNil(t, target, "rollout %d must target %s", rollout.GetId(), device.identifier) {
		assert.Equal(t, rolloutv1.RolloutDevicePhase_ROLLOUT_DEVICE_PHASE_DONE, target.GetPhase(), "%s must verify the target firmware", device.identifier)
	}
}

// dumpRollout logs the user-visible rollout, target and channel member state.
func (r *fleetNodeRun) dumpRollout(t *testing.T, rollout *rolloutv1.Rollout, device nodeDevice) {
	t.Helper()
	ctx := t.Context()
	t.Logf("rollout: %s", compactJSON(rollout))
	if devices, err := r.rollouts.ListRolloutDevices(ctx, connect.NewRequest(&rolloutv1.ListRolloutDevicesRequest{RolloutId: rollout.GetId()})); err == nil {
		for _, d := range devices.Msg.GetDevices() {
			t.Logf("rollout device: %s", compactJSON(d))
		}
	} else {
		t.Logf("list rollout devices: %v", err)
	}
	if miners, err := r.rollouts.ListReleaseChannelMiners(ctx, connect.NewRequest(&rolloutv1.ListReleaseChannelMinersRequest{
		ChannelId: rollout.GetChannelId(), Manufacturer: device.manufacturer, Model: device.model, Component: rollout.GetComponent(),
	})); err == nil {
		for _, m := range miners.Msg.GetMiners() {
			t.Logf("channel miner: %s", compactJSON(m))
		}
	} else {
		t.Logf("list channel miners: %v", err)
	}
}

func (r *fleetNodeRun) uploadFirmware(t *testing.T, filename string, payload []byte, fields map[string]string) string {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, name := range []string{"target_manufacturer", "target_model", "firmware_version", "hashboard_build", "component"} {
		if value, ok := fields[name]; ok {
			require.NoError(t, form.WriteField(name, value))
		}
	}
	part, err := form.CreateFormFile("file", filepath.Base(filename))
	require.NoError(t, err)
	_, err = part.Write(payload)
	require.NoError(t, err)
	require.NoError(t, form.Close())

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, fleetAPIURL+"/api/v1/firmware/upload", &body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := r.session.Do(req)
	require.NoError(t, err, "upload %s", filename)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "upload %s: %s", filename, raw)
	var out struct {
		FirmwareFileID string `json:"firmware_file_id"`
	}
	require.NoError(t, json.Unmarshal(raw, &out), "upload response: %s", raw)
	require.NotEmpty(t, out.FirmwareFileID, "upload response: %s", raw)
	t.Logf("uploaded %s as %s", filename, out.FirmwareFileID)
	return out.FirmwareFileID
}

func (r *fleetNodeRun) rigReportedVersion(t *testing.T) string {
	t.Helper()
	var info struct {
		SystemInfo struct {
			OS struct {
				Version string `json:"version"`
			} `json:"os"`
		} `json:"system-info"`
	}
	require.NoError(t, json.Unmarshal(r.deviceGET(t, fleetNodeRigService, "http://localhost:8080/api/v1/system"), &info))
	return info.SystemInfo.OS.Version
}

type ct1Board struct {
	Serial          string `json:"serial"`
	FirmwareVersion string `json:"firmwareVersion"`
	PSUIdentity     struct {
		FirmwareVersion string `json:"firmwareVersion"`
	} `json:"psuIdentity"`
}

func (r *fleetNodeRun) ct1Boards(t *testing.T) []ct1Board {
	t.Helper()
	var view struct {
		ContainerMap struct {
			Tanks []struct {
				Modules []struct {
					Slots []struct {
						Hashboard *ct1Board `json:"hashboard"`
					} `json:"slots"`
				} `json:"modules"`
			} `json:"tanks"`
		} `json:"containerMap"`
	}
	require.NoError(t, json.Unmarshal(r.deviceGET(t, fleetNodeCT1Service, "http://localhost:"+fleetNodeCT1HTTPPort+"/api/v2/container/map"), &view))
	var boards []ct1Board
	for _, tank := range view.ContainerMap.Tanks {
		for _, module := range tank.Modules {
			for _, slot := range module.Slots {
				if slot.Hashboard != nil {
					boards = append(boards, *slot.Hashboard)
				}
			}
		}
	}
	return boards
}

// ct1BoardVersions counts the container's hashboards by the version that
// version reads from each one.
func (r *fleetNodeRun) ct1BoardVersions(t *testing.T, version func(ct1Board) string) map[string]int {
	t.Helper()
	boards := r.ct1Boards(t)
	require.NotEmpty(t, boards, "the container must report its hashboards")
	counts := map[string]int{}
	for _, b := range boards {
		counts[version(b)]++
	}
	return counts
}

// deviceGET reads a device's own API from inside its container, because only
// the node can reach the miner network.
func (r *fleetNodeRun) deviceGET(t *testing.T, service, url string) []byte {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "docker", "exec", r.containerName(service), "wget", "-qO-", url).Output()
	require.NoError(t, err, "read %s from %s", url, service)
	return out
}

func (r *fleetNodeRun) containerJoins(t *testing.T, service, network string) bool {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "docker", "inspect", "--format", "{{json .NetworkSettings.Networks}}", r.containerName(service)).Output()
	require.NoError(t, err, "inspect %s", service)
	var networks map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out, &networks))
	_, ok := networks[r.project+"_"+network]
	return ok
}

func (r *fleetNodeRun) containerName(service string) string {
	return r.project + "-" + service + "-1"
}

func compactJSON(m proto.Message) string {
	out, err := protojson.Marshal(m)
	if err != nil {
		return err.Error()
	}
	return string(out)
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return b
}
