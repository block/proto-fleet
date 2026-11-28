package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
)

const (
	frigateNodeJob         = "0b9f0f4e-1c2d-4e5f-8a6b-7c8d9e0f1a2b"
	frigateNodeFingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// frigateControllers is the CT1 controller pair behind the node's plugin.
type frigateControllers struct {
	sdk.Device
	mu        sync.Mutex
	copies    map[string][]byte
	begun     []string
	statusErr error
	// unreachable makes one controller fail the copy.
	unreachable string
}

func (c *frigateControllers) FrigateUpdateSupport(context.Context) ([]sdk.FrigateInstanceSupport, error) {
	return []sdk.FrigateInstanceSupport{{InstanceID: "a", RunningVersion: "1.0.0"}, {InstanceID: "b", RunningVersion: "1.0.0"}}, nil
}

func (c *frigateControllers) FrigateStageBegin(_ context.Context, instanceID string, m sdk.FrigateManifest) (sdk.FrigateStageProgress, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.begun = append(c.begun, instanceID)
	if instanceID == c.unreachable {
		return sdk.FrigateStageProgress{}, errors.New("controller " + instanceID + " did not answer")
	}
	return sdk.FrigateStageProgress{JobID: m.JobID, ManifestSHA256: frigateNodeFingerprint, NextOffset: int64(len(c.copies[instanceID])), SizeBytes: m.SizeBytes}, nil
}

func (c *frigateControllers) FrigateStageAppend(_ context.Context, instanceID string, a sdk.FrigateStageAppend) (sdk.FrigateStageProgress, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.copies[instanceID] = append(c.copies[instanceID], a.Chunk...)
	return sdk.FrigateStageProgress{JobID: a.JobID, ManifestSHA256: a.ManifestSHA256, NextOffset: int64(len(c.copies[instanceID]))}, nil
}

func (c *frigateControllers) FrigateStageCommit(_ context.Context, instanceID string, r sdk.FrigateStageRef) (sdk.FrigateStageCommit, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sum := sha256.Sum256(c.copies[instanceID])
	return sdk.FrigateStageCommit{JobID: r.JobID, BytesStaged: int64(len(c.copies[instanceID])), SHA256: hex.EncodeToString(sum[:])}, nil
}

func (c *frigateControllers) FrigateStageAbort(context.Context, string, sdk.FrigateStageRef) error {
	return nil
}

func (c *frigateControllers) FrigateUpdateStart(_ context.Context, r sdk.FrigateStartRequest) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{JobID: r.JobID, Phase: sdk.FrigatePhaseVerifying}, nil
}

func (c *frigateControllers) FrigateUpdateStatus(_ context.Context, jobID string) (sdk.FrigateStatus, error) {
	if c.statusErr != nil {
		return sdk.FrigateStatus{}, c.statusErr
	}
	return sdk.FrigateStatus{Rollout: sdk.FrigateRollout{JobID: jobID, Phase: sdk.FrigatePhaseInstalling}, ServedByMaintenance: true}, nil
}

func (c *frigateControllers) FrigateUpdateRetry(_ context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{JobID: a.JobID}, nil
}

func (c *frigateControllers) FrigateUpdateCancel(_ context.Context, a sdk.FrigateAction) (sdk.FrigateRollout, error) {
	return sdk.FrigateRollout{JobID: a.JobID}, nil
}

// runFrigateUpdateCommand runs one update command on a node with these controllers.
func runFrigateUpdateCommand(t *testing.T, client gatewayClient, device sdk.Device, tempRoot string, action *pb.FrigateUpdateAction) *pb.ControlAck {
	t.Helper()
	ctrl := gomock.NewController(t)
	base := mocks.NewMockDevice(ctrl)
	base.EXPECT().Close(gomock.Any()).Return(nil)
	if controllers, ok := device.(*frigateControllers); ok {
		controllers.Device = base
	} else {
		device = base
	}
	driver := mocks.NewMockDriver(ctrl)
	driver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, info sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.NewDeviceResult, error) {
			// The update addresses the controllers, whatever class the target has.
			assert.Equal(t, sdk.DeviceClassContainer, info.DeviceClass)
			return sdk.NewDeviceResult{Device: device}, nil
		})
	run := &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}, firmwareTempRoot: tempRoot}
	capture := &captureAcker{}
	run.handleMinerCommand(t.Context(), client, capture, "cmd-1", withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_FrigateUpdate{FrigateUpdate: action}}), discardLogger(t))
	return capture.only(t)
}

func frigateOutcomes(t *testing.T, ack *pb.ControlAck) []*pb.FrigateUpdateOutcome {
	t.Helper()
	require.True(t, ack.GetSucceeded(), ack.GetErrorMessage())
	var result pb.FrigateUpdateResult
	require.NoError(t, proto.Unmarshal(ack.GetPayload(), &result))
	return result.GetOutcomes()
}

// frigateRequest encodes one call as Fleet's relay does.
func frigateRequest(t *testing.T, call func(relay sdk.FrigateRelay)) *pb.FrigateUpdateAction {
	t.Helper()
	var request []byte
	call(sdk.NewFrigateRelay(func(_ context.Context, encoded []byte) ([]byte, error) {
		request = encoded
		return nil, context.Canceled
	}))
	require.NotEmpty(t, request)
	return &pb.FrigateUpdateAction{Call: &pb.FrigateUpdateAction_Request{Request: request}}
}

func TestFrigateUpdateNodeRunsACallOnTheControllers(t *testing.T) {
	controllers := &frigateControllers{}
	ack := runFrigateUpdateCommand(t, nil, controllers, "", frigateRequest(t, func(relay sdk.FrigateRelay) {
		_, _ = relay.FrigateUpdateStatus(t.Context(), frigateNodeJob)
	}))

	outcomes := frigateOutcomes(t, ack)
	require.Len(t, outcomes, 1)
	// Fleet's relay decodes the response as it decodes one from a plugin.
	status, err := sdk.NewFrigateRelay(func(context.Context, []byte) ([]byte, error) { return outcomes[0].GetResponse(), nil }).FrigateUpdateStatus(t.Context(), frigateNodeJob)
	require.NoError(t, err)
	assert.Equal(t, sdk.FrigatePhaseInstalling, status.Rollout.Phase)
	assert.True(t, status.ServedByMaintenance)
}

func TestFrigateUpdateNodeReportsARefusalAsAnOutcome(t *testing.T) {
	controllers := &frigateControllers{statusErr: &sdk.FrigateRefusal{Reason: "UPDATE_NOT_FOUND", Message: "unknown rollout", Code: codes.NotFound}}
	ack := runFrigateUpdateCommand(t, nil, controllers, "", frigateRequest(t, func(relay sdk.FrigateRelay) {
		_, _ = relay.FrigateUpdateStatus(t.Context(), frigateNodeJob)
	}))

	outcomes := frigateOutcomes(t, ack)
	require.Len(t, outcomes, 1)
	refusal := outcomes[0].GetRefusal()
	require.NotNil(t, refusal, "the command succeeded; the controller refused")
	assert.Equal(t, "UPDATE_NOT_FOUND", refusal.GetReason())
	assert.Equal(t, "unknown rollout", refusal.GetMessage())
	assert.Equal(t, uint32(codes.NotFound), refusal.GetCode())
}

func TestFrigateUpdateNodeFailsTheCommandForOtherErrors(t *testing.T) {
	t.Run("the controllers did not answer", func(t *testing.T) {
		ack := runFrigateUpdateCommand(t, nil, &frigateControllers{statusErr: errors.New("controllers offline")}, "", frigateRequest(t, func(relay sdk.FrigateRelay) {
			_, _ = relay.FrigateUpdateStatus(t.Context(), frigateNodeJob)
		}))
		assert.False(t, ack.GetSucceeded())
		assert.Contains(t, ack.GetErrorMessage(), "controllers offline")
	})
	t.Run("the device has no update calls", func(t *testing.T) {
		ack := runFrigateUpdateCommand(t, nil, nil, "", frigateRequest(t, func(relay sdk.FrigateRelay) {
			_, _ = relay.FrigateUpdateSupport(t.Context())
		}))
		assert.False(t, ack.GetSucceeded())
	})
	t.Run("the request does not decode", func(t *testing.T) {
		ack := runFrigateUpdateCommand(t, nil, &frigateControllers{}, "", &pb.FrigateUpdateAction{Call: &pb.FrigateUpdateAction_Request{Request: []byte("\xff\xff\xff")}})
		assert.False(t, ack.GetSucceeded())
		assert.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, ack.GetCode())
	})
}

func frigateStageAction(t *testing.T, ref *pb.CommandArtifactRef, size int64, digest string, instances ...string) *pb.FrigateUpdateAction {
	t.Helper()
	stage := &pb.FrigateUpdateStage{Package: ref}
	for _, instanceID := range instances {
		begin, err := sdk.EncodeFrigateRelayStageBegin(instanceID, sdk.FrigateManifest{
			JobID: frigateNodeJob, InstanceID: instanceID, ExpectedInstanceIDs: []string{"a", "b"}, TargetVersion: "2.0.0",
			SHA256: digest, SizeBytes: size, Actor: "alice", Coordination: sdk.FrigateCoordinationSelf,
			ReleaseStatement: []byte("statement"), ReleaseSignature: []byte("signature"),
		})
		require.NoError(t, err)
		stage.Targets = append(stage.Targets, &pb.FrigateUpdateStageTarget{BeginRequest: begin, ManifestSha256: frigateNodeFingerprint})
	}
	return &pb.FrigateUpdateAction{Call: &pb.FrigateUpdateAction_Stage{Stage: stage}}
}

func packageRef(payload []byte) *pb.CommandArtifactRef {
	ref := firmwareRef("5a0c7a50-7a2b-4f0e-9d44-2f6f3f3a1b10", "frigate_2.0.0_linux_arm64.deb", payload)
	ref.Purpose = pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE
	return ref
}

func TestFrigateUpdateNodeDownloadsThePackageOnceAndCopiesItToEveryController(t *testing.T) {
	payload := bytes.Repeat([]byte("controller software "), 80_000) // 1.6 MB: two chunks.
	ref := packageRef(payload)
	gateway := &firmwareDownloadGateway{ref: ref, payload: payload}
	tempRoot := t.TempDir()
	// Controller a already holds the first part, as after an attempt that stopped.
	controllers := &frigateControllers{copies: map[string][]byte{"a": bytes.Clone(payload[:300_000])}}

	ack := runFrigateUpdateCommand(t, newFirmwareDownloadClient(t, gateway), controllers, tempRoot, frigateStageAction(t, ref, int64(len(payload)), ref.GetSha256(), "a", "b"))

	outcomes := frigateOutcomes(t, ack)
	require.Len(t, outcomes, 2)
	for i, instanceID := range []string{"a", "b"} {
		commit, err := sdk.DecodeFrigateRelayStageCommit(outcomes[i].GetResponse())
		require.NoError(t, err, instanceID)
		assert.Equal(t, sdk.FrigateStageCommit{JobID: frigateNodeJob, BytesStaged: int64(len(payload)), SHA256: ref.GetSha256()}, commit)
		assert.Equal(t, payload, controllers.copies[instanceID])
	}
	// One download serves both controllers, and it is bound to this command.
	require.NotNil(t, gateway.request)
	assert.Equal(t, "cmd-1", gateway.request.GetCommandId())
	assert.Equal(t, "dev-1", gateway.request.GetDeviceIdentifier())
	assert.Equal(t, pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE, gateway.request.GetArtifact().GetPurpose())
	entries, err := os.ReadDir(tempRoot)
	require.NoError(t, err)
	assert.Empty(t, entries, "the downloaded package is removed")
}

func TestFrigateUpdateNodeReportsEachControllerOfAStage(t *testing.T) {
	payload := []byte("controller software package")
	ref := packageRef(payload)
	controllers := &frigateControllers{copies: map[string][]byte{}, unreachable: "a"}

	ack := runFrigateUpdateCommand(t, newFirmwareDownloadClient(t, &firmwareDownloadGateway{ref: ref, payload: payload}), controllers, t.TempDir(),
		frigateStageAction(t, ref, int64(len(payload)), ref.GetSha256(), "a", "b"))

	outcomes := frigateOutcomes(t, ack)
	require.Len(t, outcomes, 2)
	assert.Contains(t, outcomes[0].GetError(), "controller a did not answer")
	_, err := sdk.DecodeFrigateRelayStageCommit(outcomes[1].GetResponse())
	require.NoError(t, err, "a controller that fails does not stop the copy to the other")
	assert.ElementsMatch(t, []string{"a", "b"}, controllers.begun)
}

func TestFrigateUpdateNodeRejectsAStageItCannotRun(t *testing.T) {
	payload := []byte("controller software package")
	good := packageRef(payload)
	firmware := firmwareRef(good.GetArtifactId(), good.GetFilename(), payload)
	oversized := packageRef(payload)
	oversized.SizeBytes = maxFleetNodeFirmwareArtifactSizeBytes + 1

	for name, tc := range map[string]struct {
		action   *pb.FrigateUpdateAction
		client   gatewayClient
		wantCode pb.AckCode
		wantText string
	}{
		"wrong artifact purpose": {frigateStageAction(t, firmware, int64(len(payload)), good.GetSha256(), "a"), nil, pb.AckCode_ACK_CODE_BAD_REQUEST, "wrong purpose"},
		"package too large":      {frigateStageAction(t, oversized, int64(len(payload)), good.GetSha256(), "a"), nil, pb.AckCode_ACK_CODE_BAD_REQUEST, "exceeds fleet node limit"},
		"no gateway client":      {frigateStageAction(t, good, int64(len(payload)), good.GetSha256(), "a"), nil, pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "gateway client"},
		"download is corrupt": {frigateStageAction(t, good, int64(len(payload)), good.GetSha256(), "a"),
			newFirmwareDownloadClient(t, &firmwareDownloadGateway{ref: good, payload: payload, sendData: []byte(strings.Repeat("x", len(payload)))}), pb.AckCode_ACK_CODE_BAD_REQUEST, ""},
	} {
		t.Run(name, func(t *testing.T) {
			controllers := &frigateControllers{copies: map[string][]byte{}}
			ack := runFrigateUpdateCommand(t, tc.client, controllers, t.TempDir(), tc.action)
			assert.False(t, ack.GetSucceeded())
			assert.Equal(t, tc.wantCode, ack.GetCode())
			assert.Contains(t, ack.GetErrorMessage(), tc.wantText)
			assert.Empty(t, controllers.begun, "no controller is touched")
		})
	}
}

func TestFrigateUpdateNodeRejectsAnInvalidCommand(t *testing.T) {
	ref := packageRef([]byte("package"))
	target := &pb.FrigateUpdateStageTarget{BeginRequest: []byte{1}, ManifestSha256: frigateNodeFingerprint}
	badFingerprint := &pb.FrigateUpdateStageTarget{BeginRequest: []byte{1}, ManifestSha256: "nope"}
	for name, action := range map[string]*pb.FrigateUpdateAction{
		"no call":         {},
		"no package":      {Call: &pb.FrigateUpdateAction_Stage{Stage: &pb.FrigateUpdateStage{Targets: []*pb.FrigateUpdateStageTarget{target}}}},
		"no target":       {Call: &pb.FrigateUpdateAction_Stage{Stage: &pb.FrigateUpdateStage{Package: ref}}},
		"bad fingerprint": {Call: &pb.FrigateUpdateAction_Stage{Stage: &pb.FrigateUpdateStage{Package: ref, Targets: []*pb.FrigateUpdateStageTarget{badFingerprint}}}},
	} {
		t.Run(name, func(t *testing.T) {
			run := &RunCmd{driverGetter: fakeDriverGetter{d: mocks.NewMockDriver(gomock.NewController(t))}, minerSecrets: nodeSecretProvider{}}
			capture := &captureAcker{}
			run.handleMinerCommand(t.Context(), nil, capture, "cmd-1", withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_FrigateUpdate{FrigateUpdate: action}}), discardLogger(t))
			ack := capture.only(t)
			assert.False(t, ack.GetSucceeded())
			assert.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, ack.GetCode())
			assert.Contains(t, ack.GetErrorMessage(), "invalid miner command")
		})
	}
}

func TestFrigateUpdateStageUsesTheFirmwareCommandBudget(t *testing.T) {
	stage := &pb.MinerCommand{Action: &pb.MinerCommand_FrigateUpdate{FrigateUpdate: &pb.FrigateUpdateAction{Call: &pb.FrigateUpdateAction_Stage{Stage: &pb.FrigateUpdateStage{}}}}}
	call := &pb.MinerCommand{Action: &pb.MinerCommand_FrigateUpdate{FrigateUpdate: &pb.FrigateUpdateAction{Call: &pb.FrigateUpdateAction_Request{Request: []byte{1}}}}}
	assert.Equal(t, firmwareMinerCommandTimeout, minerCommandActionTimeout(stage))
	assert.Equal(t, minerCommandTimeout, minerCommandActionTimeout(call))
	assert.Equal(t, pb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V21, latestCommandProtocolVersion, "the node tells Fleet that it stages ProtoOS packages")
}

func TestFrigateUpdateOutcomeKeepsItsTextInTheContract(t *testing.T) {
	long := strings.Repeat("é", maxFrigateUpdateTextBytes)
	for name, err := range map[string]error{
		"error":   errors.New(long),
		"refusal": &sdk.FrigateRefusal{Reason: strings.Repeat("UPDATE_", 30), Message: long, Code: codes.FailedPrecondition},
	} {
		outcome := frigateUpdateOutcome(nil, err)
		require.NoError(t, protovalidate.Validate(outcome), name)
		for _, text := range []string{outcome.GetError(), outcome.GetRefusal().GetReason(), outcome.GetRefusal().GetMessage()} {
			assert.True(t, utf8.ValidString(text), name)
			assert.LessOrEqual(t, len(text), maxFrigateUpdateTextBytes, name)
		}
	}
	assert.Equal(t, "short", frigateUpdateOutcome(nil, errors.New("short")).GetError())
}
