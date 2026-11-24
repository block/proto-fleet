package ct1

import (
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
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

const simulatorTargetVersion = "9.9.9"

var (
	fakeCT1Once   sync.Once
	fakeCT1Binary string
	fakeCT1Err    error
)

// fakeCT1 returns the CT1_SIMULATOR_BIN binary or builds server/fake-ct1 once.
// The fake is its own Go module outside go.work, so it is built with GOWORK=off.
func fakeCT1(t *testing.T) string {
	t.Helper()
	if binary := os.Getenv("CT1_SIMULATOR_BIN"); binary != "" {
		return binary
	}
	fakeCT1Once.Do(func() {
		dir, err := os.MkdirTemp("", "fake-ct1-*") //nolint:usetesting // Shared by every test in the package.
		if err != nil {
			fakeCT1Err = err
			return
		}
		fakeCT1Binary = filepath.Join(dir, "fake-ct1")
		build := exec.Command("go", "build", "-o", fakeCT1Binary, ".")
		build.Dir = filepath.Join("..", "..", "..", "..", "server", "fake-ct1")
		build.Env = append(os.Environ(), "GOWORK=off")
		if output, err := build.CombinedOutput(); err != nil {
			fakeCT1Err = &buildError{err: err, output: string(output)}
		}
	})
	if fakeCT1Err != nil {
		t.Skipf("cannot build server/fake-ct1 (set CT1_SIMULATOR_BIN to use a prebuilt binary): %v", fakeCT1Err)
	}
	return fakeCT1Binary
}

type buildError struct {
	err    error
	output string
}

func (e *buildError) Error() string { return e.err.Error() + ": " + e.output }

func freePorts(t *testing.T, n int) []string {
	t.Helper()
	listeners := make([]net.Listener, n)
	ports := make([]string, n)
	for i := range listeners {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners[i] = l
		_, ports[i], err = net.SplitHostPort(l.Addr().String())
		require.NoError(t, err)
	}
	for _, l := range listeners {
		require.NoError(t, l.Close())
	}
	return ports
}

// simulatedPair is a fake-ct1 HA pair, its release signing key and a Device.
type simulatedPair struct {
	key     *ecdsa.PrivateKey
	process *exec.Cmd
	info    sdk.DeviceInfo
	device  *Device
}

func startSimulatedPair(t *testing.T, env ...string) *simulatedPair {
	t.Helper()
	binary := fakeCT1(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	keyFile := filepath.Join(t.TempDir(), "release.pub")
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600))
	ports := freePorts(t, 4)
	process := exec.CommandContext(t.Context(), binary)
	process.Env = append(os.Environ(),
		"HTTP_PORT="+ports[0], "GRPC_PORT="+ports[1], "SERVER_ID=pair-a", "CT1_TANKS=1", "CT1_MODULES_PER_TANK=1", "CT1_DEMO_MODULE_STATES=false",
		"CT1_HA_PAIR=1", "CT1_HA_PEER_SERVER_ID=pair-b", "CT1_HA_PEER_HTTP_PORT="+ports[2], "CT1_HA_PEER_GRPC_PORT="+ports[3], "CT1_HA_CLUSTER_ID="+testClusterID,
		"CT1_UPDATE_TRUSTED_KEY_FILES="+keyFile, "CT1_UPDATE_STEP=150ms")
	process.Env = append(process.Env, env...)
	require.NoError(t, process.Start())
	t.Cleanup(func() { _ = process.Process.Kill(); _ = process.Wait() })
	atoi := func(s string) int32 { v, err := strconv.ParseInt(s, 10, 32); require.NoError(t, err); return int32(v) }
	endpoint := func(id, httpPort, grpcPort string) sdk.DeviceEndpoint {
		return sdk.DeviceEndpoint{ID: id, DeviceKey: "ct1:" + testClusterID, MemberIDs: []string{"pair-a", "pair-b"}, Host: "127.0.0.1", Port: atoi(httpPort), URLScheme: "http", RPCPort: atoi(grpcPort)}
	}
	info := sdk.DeviceInfo{Host: "127.0.0.1", Port: atoi(ports[0]), RPCPort: atoi(ports[1]), URLScheme: "http", SerialNumber: "pair-a",
		Endpoints: []sdk.DeviceEndpoint{endpoint("pair-a", ports[0], ports[1]), endpoint("pair-b", ports[2], ports[3])}}
	pair := &simulatedPair{key: key, process: process, info: info}
	pair.device = pair.connect(t)
	require.Eventually(t, func() bool {
		supports, err := pair.device.FrigateUpdateSupport(t.Context())
		return err == nil && len(supports) == 2 && supports[0].Err == "" && supports[1].Err == ""
	}, 10*time.Second, 50*time.Millisecond)
	return pair
}

func (p *simulatedPair) connect(t *testing.T) *Device {
	t.Helper()
	d, err := NewDevice(t.Context(), "pair", p.info, sdk.UsernamePassword{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close(t.Context()) })
	return d
}

type simulatedPackage struct {
	bytes     []byte
	sha       string
	statement []byte
	signature []byte
}

func (p *simulatedPair) release(t *testing.T, size int) simulatedPackage {
	t.Helper()
	pkg := make([]byte, size)
	for i := range pkg {
		pkg[i] = byte(i*13 + 5)
	}
	sum := sha256.Sum256(pkg)
	statement, err := json.Marshal(map[string]any{
		"schema": "frigate-release-statement/v1", "package": "frigate", "version": simulatorTargetVersion, "architecture": "arm64",
		"repository": "example/frigate", "sha256": hex.EncodeToString(sum[:]), "size": size, "min_installed_version": "", "issued_at": "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)
	digest := sha256.Sum256(statement)
	signature, err := ecdsa.SignASN1(rand.Reader, p.key, digest[:])
	require.NoError(t, err)
	return simulatedPackage{bytes: pkg, sha: hex.EncodeToString(sum[:]), statement: statement, signature: signature}
}

func (p *simulatedPair) manifest(job, instance string, pkg simulatedPackage) sdk.FrigateManifest {
	return sdk.FrigateManifest{
		JobID: job, InstanceID: instance, ExpectedInstanceIDs: []string{"pair-a", "pair-b"}, TargetVersion: simulatorTargetVersion,
		SHA256: pkg.sha, SizeBytes: int64(len(pkg.bytes)), Actor: "fleet:tester@example.com", Coordination: sdk.FrigateCoordinationSelf,
		ReleaseStatement: pkg.statement, ReleaseSignature: pkg.signature,
	}
}

// stage uploads pkg to one instance from the given offset in small chunks and
// commits it. It returns the instance's manifest fingerprint.
func stageInstance(t *testing.T, d *Device, m sdk.FrigateManifest, pkg simulatedPackage, stopAfter int) string {
	t.Helper()
	progress, err := d.FrigateStageBegin(t.Context(), m.InstanceID, m)
	require.NoError(t, err)
	offset := progress.NextOffset
	for offset < int64(len(pkg.bytes)) {
		end := min(offset+1000, int64(len(pkg.bytes)))
		next, err := d.FrigateStageAppend(t.Context(), m.InstanceID, sdk.FrigateStageAppend{JobID: m.JobID, ManifestSHA256: progress.ManifestSHA256, Offset: offset, Chunk: pkg.bytes[offset:end], Actor: m.Actor})
		require.NoError(t, err)
		require.Equal(t, end, next.NextOffset)
		offset = next.NextOffset
		if stopAfter > 0 && offset >= int64(stopAfter) {
			return progress.ManifestSHA256
		}
	}
	commit, err := d.FrigateStageCommit(t.Context(), m.InstanceID, sdk.FrigateStageRef{JobID: m.JobID, ManifestSHA256: progress.ManifestSHA256, Actor: m.Actor})
	require.NoError(t, err)
	require.Equal(t, pkg.sha, commit.SHA256)
	require.EqualValues(t, len(pkg.bytes), commit.BytesStaged)
	return progress.ManifestSHA256
}

func (p *simulatedPair) stageBoth(t *testing.T, job string, pkg simulatedPackage) map[string]string {
	t.Helper()
	fingerprints := map[string]string{}
	for _, instance := range []string{"pair-a", "pair-b"} {
		fingerprints[instance] = stageInstance(t, p.device, p.manifest(job, instance, pkg), pkg, 0)
	}
	return fingerprints
}

func startFor(job string, fingerprints map[string]string) sdk.FrigateStartRequest {
	return sdk.FrigateStartRequest{JobID: job, TargetVersion: simulatorTargetVersion, ExpectedInstanceIDs: []string{"pair-a", "pair-b"}, IdempotencyKey: "start-" + job, ExpectedManifestSHA256: fingerprints, Actor: "fleet:tester@example.com"}
}

func TestFrigateUpdateSimulatorHappyPath(t *testing.T) {
	pair := startSimulatedPair(t)
	supports, err := pair.device.FrigateUpdateSupport(t.Context())
	require.NoError(t, err)
	require.Equal(t, "pair-a", supports[0].InstanceID)
	require.Equal(t, "pair-b", supports[0].PeerInstanceID)
	// Update support reports the etcd cluster, not the HA cluster UUID in the
	// device key; Start must still accept the pair.
	require.NotEmpty(t, supports[0].ClusterID)
	require.NotEqual(t, testClusterID, supports[0].ClusterID)
	require.Equal(t, supports[0].ClusterID, supports[1].ClusterID)
	require.True(t, supports[0].SelfCoordinationSupported)
	require.True(t, supports[1].RemoteTriggerAllowed)
	require.Len(t, supports[0].TrustedReleaseKeyIDs, 1)

	pkg := pair.release(t, 3500)
	fingerprints := pair.stageBoth(t, "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be1", pkg)
	rollout, err := pair.device.FrigateUpdateStart(t.Context(), startFor("1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be1", fingerprints))
	require.NoError(t, err)
	require.Equal(t, sdk.FrigatePhaseVerifying, rollout.Phase)
	require.Equal(t, "proto-fleet:tester@example.com", rollout.Actor)
	require.Equal(t, pkg.sha, rollout.PackageSHA256)

	// Observe maintenance mode from the polling goroutine without asserting in it.
	var maintenance, supportUnavailable atomic.Bool
	require.Eventually(t, func() bool {
		got, err := pair.device.FrigateUpdateStatus(t.Context(), "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be1")
		if err != nil {
			return false
		}
		if got.ServedByMaintenance {
			maintenance.Store(true)
			// While stopped, update support is unavailable but status is not.
			if supports, err := pair.device.FrigateUpdateSupport(t.Context()); err == nil && supports[0].Err != "" {
				supportUnavailable.Store(true)
			}
		}
		return got.Rollout.Phase == sdk.FrigatePhaseSucceeded
	}, 20*time.Second, 25*time.Millisecond)
	require.True(t, maintenance.Load(), "maintenance-mode status was never observed")
	require.True(t, supportUnavailable.Load(), "update support stayed available while Frigate was stopped")

	final, err := pair.device.FrigateUpdateStatus(t.Context(), "")
	require.NoError(t, err)
	require.False(t, final.ServedByMaintenance)
	require.True(t, final.Rollout.Phase.Terminal())
	for _, instance := range final.Rollout.Instances {
		require.Equal(t, sdk.FrigateAgentStarted, instance.Phase)
		require.Equal(t, simulatorTargetVersion, instance.RunningVersion)
	}
	supports, err = pair.device.FrigateUpdateSupport(t.Context())
	require.NoError(t, err)
	require.Equal(t, simulatorTargetVersion, supports[0].RunningVersion)
	require.Empty(t, supports[0].ActiveJobID)
}

func TestFrigateUpdateSimulatorResumesStagingAfterRestart(t *testing.T) {
	pair := startSimulatedPair(t)
	pkg := pair.release(t, 3500)
	m := pair.manifest("2b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be2", "pair-b", pkg)
	// The first uploader dies after acknowledging 2000 bytes.
	stageInstance(t, pair.device, m, pkg, 2000)
	// A fresh Device (a restarted Fleet) resumes from the acknowledged offset.
	restarted := pair.connect(t)
	progress, err := restarted.FrigateStageBegin(t.Context(), "pair-b", m)
	require.NoError(t, err)
	require.EqualValues(t, 2000, progress.NextOffset)
	// Replaying an acknowledged chunk (lost reply) is accepted without moving the offset.
	again, err := restarted.FrigateStageAppend(t.Context(), "pair-b", sdk.FrigateStageAppend{JobID: "2b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be2", ManifestSHA256: progress.ManifestSHA256, Offset: 1000, Chunk: pkg.bytes[1000:2000]})
	require.NoError(t, err)
	require.EqualValues(t, 2000, again.NextOffset)
	// A gap is refused.
	_, err = restarted.FrigateStageAppend(t.Context(), "pair-b", sdk.FrigateStageAppend{JobID: "2b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be2", ManifestSHA256: progress.ManifestSHA256, Offset: 3000, Chunk: pkg.bytes[3000:3500]})
	require.Error(t, err)
	stageInstance(t, restarted, m, pkg, 0)
	// The other instance is independent of the first.
	stageInstance(t, restarted, pair.manifest("2b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be2", "pair-a", pkg), pkg, 0)
}

func TestFrigateUpdateSimulatorRefusals(t *testing.T) {
	t.Run("not staged", func(t *testing.T) {
		pair := startSimulatedPair(t)
		_, err := pair.device.FrigateUpdateStart(t.Context(), startFor("6b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be6", nil))
		refusal := sdk.AsFrigateRefusal(err)
		require.NotNil(t, refusal, "%v", err)
		require.Equal(t, "UPDATE_NOT_STAGED", refusal.Reason)
		require.Equal(t, codes.FailedPrecondition, refusal.Code)
	})
	t.Run("bad signature", func(t *testing.T) {
		pair := startSimulatedPair(t)
		pkg := pair.release(t, 800)
		pkg.signature = append([]byte(nil), pkg.signature...)
		pkg.signature[len(pkg.signature)-1] ^= 0xff
		fingerprints := pair.stageBoth(t, "3b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be3", pkg)
		_, err := pair.device.FrigateUpdateStart(t.Context(), startFor("3b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be3", fingerprints))
		refusal := sdk.AsFrigateRefusal(err)
		require.NotNil(t, refusal, "%v", err)
		require.Equal(t, "UPDATE_SIGNATURE_INVALID", refusal.Reason)
	})
	t.Run("remote trigger disabled", func(t *testing.T) {
		pair := startSimulatedPair(t, "CT1_UPDATE_FAULT_REMOTE_TRIGGER_DISABLED=true")
		_, err := pair.device.FrigateUpdateStart(t.Context(), startFor("7b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be7", nil))
		refusal := sdk.AsFrigateRefusal(err)
		require.NotNil(t, refusal, "%v", err)
		require.Equal(t, "UPDATE_REMOTE_TRIGGER_DISABLED", refusal.Reason)
	})
	t.Run("insufficient space", func(t *testing.T) {
		pair := startSimulatedPair(t, "CT1_UPDATE_SPOOL_FREE_BYTES=10")
		pkg := pair.release(t, 800)
		fingerprints := pair.stageBoth(t, "4b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be4", pkg)
		_, err := pair.device.FrigateUpdateStart(t.Context(), startFor("4b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be4", fingerprints))
		require.Equal(t, "UPDATE_INSUFFICIENT_SPACE", sdk.AsFrigateRefusal(err).Reason)
	})
	t.Run("unknown job status", func(t *testing.T) {
		pair := startSimulatedPair(t)
		_, err := pair.device.FrigateUpdateStatus(t.Context(), "8b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be8")
		refusal := sdk.AsFrigateRefusal(err)
		require.NotNil(t, refusal, "%v", err)
		require.Equal(t, "UPDATE_NOT_FOUND", refusal.Reason)
		require.Equal(t, codes.NotFound, refusal.Code)
	})
}

func TestFrigateUpdateSimulatorMigrateFailureRetryAndMaintenanceStatus(t *testing.T) {
	pair := startSimulatedPair(t, "CT1_UPDATE_FAULT_MIGRATE_FAILURES=1")
	pkg := pair.release(t, 800)
	fingerprints := pair.stageBoth(t, "5b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be5", pkg)
	_, err := pair.device.FrigateUpdateStart(t.Context(), startFor("5b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be5", fingerprints))
	require.NoError(t, err)
	var failed sdk.FrigateStatus
	require.Eventually(t, func() bool {
		failed, err = pair.device.FrigateUpdateStatus(t.Context(), "5b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be5")
		return err == nil && failed.Rollout.Phase == sdk.FrigatePhaseFailed
	}, 20*time.Second, 25*time.Millisecond)
	require.True(t, failed.ServedByMaintenance)
	require.True(t, failed.Rollout.Retryable)
	require.Equal(t, "UPDATE_MIGRATE_FAILED", failed.Rollout.ErrorCode)
	_, err = pair.device.FrigateUpdateCancel(t.Context(), sdk.FrigateAction{JobID: "5b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be5", IdempotencyKey: "cancel", Actor: "fleet:tester@example.com"})
	require.Equal(t, "UPDATE_NOT_CANCELLABLE", sdk.AsFrigateRefusal(err).Reason)
	retried, err := pair.device.FrigateUpdateRetry(t.Context(), sdk.FrigateAction{JobID: "5b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be5", IdempotencyKey: "retry", Actor: "fleet:tester@example.com"})
	require.NoError(t, err)
	require.NotEqual(t, sdk.FrigatePhaseFailed, retried.Phase)
	require.Eventually(t, func() bool {
		got, err := pair.device.FrigateUpdateStatus(t.Context(), "5b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4be5")
		return err == nil && got.Rollout.Phase == sdk.FrigatePhaseSucceeded
	}, 20*time.Second, 25*time.Millisecond)
}
