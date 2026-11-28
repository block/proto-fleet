package remotenode

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const (
	frigateTestJob         = "0b9f0f4e-1c2d-4e5f-8a6b-7c8d9e0f1a2b"
	frigateTestPackage     = "5a0c7a50-7a2b-4f0e-9d44-2f6f3f3a1b10"
	frigateTestFingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

var frigateTestDigest = strings.Repeat("3", 64)

// nodeUpdater is the controller pair behind the node. Its answers go through
// the same SDK code that a Fleet Node runs.
type nodeUpdater struct {
	sdk.DeviceFrigateUpdater
	jobID   string
	started sdk.FrigateStartRequest
	refusal error
}

func (u *nodeUpdater) FrigateUpdateSupport(context.Context) ([]sdk.FrigateInstanceSupport, error) {
	return []sdk.FrigateInstanceSupport{{InstanceID: "a", RunningVersion: "1.0.0"}, {InstanceID: "b", RunningVersion: "1.0.0"}}, nil
}

func (u *nodeUpdater) FrigateUpdateStatus(_ context.Context, jobID string) (sdk.FrigateStatus, error) {
	u.jobID = jobID
	if u.refusal != nil {
		return sdk.FrigateStatus{}, u.refusal
	}
	return sdk.FrigateStatus{Rollout: sdk.FrigateRollout{JobID: jobID, Phase: sdk.FrigatePhaseVerifying}, AnsweredBy: "a"}, nil
}

func (u *nodeUpdater) FrigateUpdateStart(_ context.Context, r sdk.FrigateStartRequest) (sdk.FrigateRollout, error) {
	u.started = r
	return sdk.FrigateRollout{JobID: r.JobID, TargetVersion: r.TargetVersion, Phase: sdk.FrigatePhaseVerifying}, nil
}

// nodeSender answers each command as a Fleet Node does for a plain update call.
type nodeSender struct {
	fakeSender
	updater *nodeUpdater
}

func (s *nodeSender) SendCommand(ctx context.Context, _ int64, version gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	s.minimumCommandProtocolVersion, s.cmd = version, cmd
	var wire gatewaypb.AgentCommand
	if err := proto.Unmarshal(cmd.GetPayload(), &wire); err != nil {
		return nil, fmt.Errorf("decode agent command: %w", err)
	}
	response, err := sdk.ServeFrigateRelay(ctx, s.updater, wire.GetMinerCommand().GetFrigateUpdate().GetRequest())
	outcome := &gatewaypb.FrigateUpdateOutcome{Outcome: &gatewaypb.FrigateUpdateOutcome_Response{Response: response}}
	if refusal := sdk.AsFrigateRefusal(err); refusal != nil {
		outcome = &gatewaypb.FrigateUpdateOutcome{Outcome: &gatewaypb.FrigateUpdateOutcome_Refusal{Refusal: &gatewaypb.FrigateUpdateRefusal{
			Reason: refusal.Reason, Message: refusal.Message, Code: uint32(refusal.Code),
		}}}
	} else if err != nil {
		return nil, err
	}
	return frigateAck(outcome), nil
}

func frigateAck(outcomes ...*gatewaypb.FrigateUpdateOutcome) *gatewaypb.ControlAck {
	payload, err := proto.Marshal(&gatewaypb.FrigateUpdateResult{Outcomes: outcomes})
	if err != nil {
		panic(err)
	}
	return &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload}
}

func sentFrigateUpdate(t *testing.T, cmd *gatewaypb.ControlCommand) (*gatewaypb.MinerCommand, *gatewaypb.FrigateUpdateAction) {
	t.Helper()
	var wire gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(cmd.GetPayload(), &wire))
	action := wire.GetMinerCommand().GetFrigateUpdate()
	require.NotNil(t, action)
	return wire.GetMinerCommand(), action
}

func frigateTestManifest(instanceID string) sdk.FrigateManifest {
	return sdk.FrigateManifest{
		JobID: frigateTestJob, InstanceID: instanceID, ExpectedInstanceIDs: []string{"a", "b"}, TargetVersion: "2.0.0",
		SHA256: frigateTestDigest, SizeBytes: 4096, Actor: "alice", Coordination: sdk.FrigateCoordinationSelf,
		ReleaseStatement: []byte("statement"), ReleaseSignature: []byte("signature"),
	}
}

func TestFrigateUpdateCallsRunOnTheNodeAndRequireV20(t *testing.T) {
	sender := &nodeSender{updater: &nodeUpdater{}}
	miner := newTestMiner(t, sender)
	var gated []commandtype.Type
	miner.writeGate = func(ctx context.Context, kind commandtype.Type, call func(context.Context) error) error {
		gated = append(gated, kind)
		return call(ctx)
	}

	support, err := miner.FrigateRelay().FrigateUpdateSupport(t.Context())
	require.NoError(t, err)
	require.Len(t, support, 2)
	require.Equal(t, FrigateUpdateCommandProtocolVersion, sender.minimumCommandProtocolVersion)
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V20, FrigateUpdateCommandProtocolVersion)
	command, _ := sentFrigateUpdate(t, sender.cmd)
	_, physical := physicalWriteKind(command)
	require.False(t, physical, "a support read skips the write gate, as on a direct controller")

	status, err := miner.FrigateRelay().FrigateUpdateStatus(t.Context(), frigateTestJob)
	require.NoError(t, err)
	require.Equal(t, frigateTestJob, status.Rollout.JobID)
	require.Equal(t, frigateTestJob, sender.updater.jobID)
	require.Empty(t, gated, "reads are not gated")

	start := sdk.FrigateStartRequest{JobID: frigateTestJob, TargetVersion: "2.0.0", ExpectedInstanceIDs: []string{"a", "b"}, IdempotencyKey: "key-1"}
	rollout, err := miner.FrigateRelay().FrigateUpdateStart(t.Context(), start)
	require.NoError(t, err)
	require.Equal(t, sdk.FrigatePhaseVerifying, rollout.Phase)
	require.Equal(t, start, sender.updater.started)
	require.Equal(t, []commandtype.Type{commandtype.FrigateServerUpdate}, gated, "a mutation takes the same gate kind as a direct controller")
	command, _ = sentFrigateUpdate(t, sender.cmd)
	kind, physical := physicalWriteKind(command)
	require.True(t, physical)
	require.Equal(t, commandtype.FrigateServerUpdate, kind)
}

func TestFrigateUpdateRefusalFromTheControllerStaysARefusal(t *testing.T) {
	refusal := &sdk.FrigateRefusal{Reason: "UPDATE_NOT_FOUND", Message: "unknown rollout", Code: codes.NotFound}
	miner := newTestMiner(t, &nodeSender{updater: &nodeUpdater{refusal: refusal}})

	_, err := miner.FrigateRelay().FrigateUpdateStatus(t.Context(), frigateTestJob)

	got := sdk.AsFrigateRefusal(err)
	require.NotNil(t, got, "the runner tells a refusal from a transport failure by this type")
	require.Equal(t, *refusal, *got)
}

func TestFrigateUpdateFailuresThatAreNotRefusals(t *testing.T) {
	t.Run("an older node is refused with an upgrade message", func(t *testing.T) {
		miner := newTestMiner(t, &fakeSender{err: control.ErrCommandProtocolUpgradeRequired})
		_, err := miner.FrigateRelay().FrigateUpdateSupport(t.Context())
		require.True(t, fleeterror.IsFailedPreconditionError(err), err)
		require.ErrorContains(t, err, "Upgrade the Fleet Node")
		require.Nil(t, sdk.AsFrigateRefusal(err))
	})
	t.Run("a failed ack is a plain error", func(t *testing.T) {
		miner := newTestMiner(t, &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_INTERNAL, ErrorMessage: "controllers offline"}})
		_, err := miner.FrigateRelay().FrigateUpdateSupport(t.Context())
		require.Error(t, err)
		require.Nil(t, sdk.AsFrigateRefusal(err), "the runner retries it")
	})
	t.Run("a result with the wrong shape is an error", func(t *testing.T) {
		for name, ack := range map[string]*gatewaypb.ControlAck{
			"no outcome":    frigateAck(),
			"two outcomes":  frigateAck(&gatewaypb.FrigateUpdateOutcome{}, &gatewaypb.FrigateUpdateOutcome{}),
			"empty outcome": frigateAck(&gatewaypb.FrigateUpdateOutcome{}),
			"not a result":  {Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: []byte("\xff\xff\xff")},
		} {
			_, err := newTestMiner(t, &fakeSender{ack: ack}).FrigateRelay().FrigateUpdateSupport(t.Context())
			require.Error(t, err, name)
		}
	})
	t.Run("package bytes are never sent as a call", func(t *testing.T) {
		sender := &fakeSender{}
		_, err := newTestMiner(t, sender).FrigateRelay().FrigateStageAppend(t.Context(), "a", sdk.FrigateStageAppend{JobID: frigateTestJob, ManifestSHA256: frigateTestFingerprint, Chunk: []byte{1}})
		require.ErrorIs(t, err, sdk.ErrFrigateRelayAppend)
		require.Nil(t, sender.cmd)
	})
}

func TestFrigateStagePackageSendsOneCommandWithThePackageForAllTargets(t *testing.T) {
	commit := func(staged int64) *gatewaypb.FrigateUpdateOutcome {
		// A commit response, as the node's SDK encodes it.
		response, err := sdk.ServeFrigateRelay(t.Context(), committed{staged: staged}, mustCommitRequest(t))
		require.NoError(t, err)
		return &gatewaypb.FrigateUpdateOutcome{Outcome: &gatewaypb.FrigateUpdateOutcome_Response{Response: response}}
	}
	sender := &fakeSender{ack: frigateAck(
		commit(4096),
		&gatewaypb.FrigateUpdateOutcome{Outcome: &gatewaypb.FrigateUpdateOutcome_Refusal{Refusal: &gatewaypb.FrigateUpdateRefusal{Reason: "UPDATE_INSUFFICIENT_SPACE", Code: uint32(codes.ResourceExhausted)}}},
		&gatewaypb.FrigateUpdateOutcome{Outcome: &gatewaypb.FrigateUpdateOutcome_Error{Error: "controller c did not answer"}},
	)}
	miner := newTestMiner(t, sender)
	var gated []commandtype.Type
	miner.writeGate = func(ctx context.Context, kind commandtype.Type, call func(context.Context) error) error {
		gated = append(gated, kind)
		_, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline, "the copy is bounded")
		return call(ctx)
	}
	pkg := interfaces.FrigatePackageRef{ID: frigateTestPackage, Filename: "frigate_2.0.0_linux_arm64.deb", SizeBytes: 4096, SHA256: frigateTestDigest}
	targets := []interfaces.FrigateStageTarget{
		{InstanceID: "a", Manifest: frigateTestManifest("a"), ManifestSHA256: frigateTestFingerprint},
		{InstanceID: "b", Manifest: frigateTestManifest("b"), ManifestSHA256: frigateTestFingerprint},
		{InstanceID: "c", Manifest: frigateTestManifest("c"), ManifestSHA256: frigateTestFingerprint},
	}
	for i := range targets {
		targets[i].Manifest.ExpectedInstanceIDs = []string{"a", "b", "c"}
	}

	outcomes, err := miner.FrigateStagePackage(t.Context(), pkg, targets)

	require.NoError(t, err)
	require.Len(t, outcomes, 3)
	require.NoError(t, outcomes[0].Err)
	require.Equal(t, sdk.FrigateStageCommit{JobID: frigateTestJob, BytesStaged: 4096, SHA256: frigateTestDigest}, outcomes[0].Commit)
	refusal := sdk.AsFrigateRefusal(outcomes[1].Err)
	require.NotNil(t, refusal)
	require.Equal(t, "UPDATE_INSUFFICIENT_SPACE", refusal.Reason)
	require.ErrorContains(t, outcomes[2].Err, "controller c did not answer")
	require.Nil(t, sdk.AsFrigateRefusal(outcomes[2].Err))

	require.Equal(t, FrigateUpdateCommandProtocolVersion, sender.minimumCommandProtocolVersion)
	require.Equal(t, []commandtype.Type{commandtype.FrigateServerUpdate}, gated)
	_, action := sentFrigateUpdate(t, sender.cmd)
	stage := action.GetStage()
	require.NotNil(t, stage)
	require.Empty(t, action.GetRequest())
	require.True(t, proto.Equal(&gatewaypb.CommandArtifactRef{
		ArtifactId: frigateTestPackage, Purpose: gatewaypb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE,
		Filename: "frigate_2.0.0_linux_arm64.deb", SizeBytes: 4096, Sha256: frigateTestDigest,
	}, stage.GetPackage()))
	require.Len(t, stage.GetTargets(), 3)
	for _, target := range stage.GetTargets() {
		require.Equal(t, frigateTestFingerprint, target.GetManifestSha256())
		require.False(t, sdk.FrigateRelayReadOnly(target.GetBeginRequest()))
	}
	// The node may download exactly this package, for this command only.
	require.Equal(t, []control.ArtifactExpectation{{
		Direction: control.ArtifactDirectionDownload, Purpose: gatewaypb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE,
		ArtifactID: frigateTestPackage, DeviceIdentifier: "dev-1",
	}}, sender.artifacts)
}

func TestFrigateStagePackageFailures(t *testing.T) {
	pkg := interfaces.FrigatePackageRef{ID: frigateTestPackage, Filename: "frigate.deb", SizeBytes: 4096, SHA256: frigateTestDigest}
	target := interfaces.FrigateStageTarget{InstanceID: "a", Manifest: frigateTestManifest("a"), ManifestSHA256: frigateTestFingerprint}

	sender := &fakeSender{}
	outcomes, err := newTestMiner(t, sender).FrigateStagePackage(t.Context(), pkg, nil)
	require.NoError(t, err)
	require.Empty(t, outcomes)
	require.Nil(t, sender.cmd, "nothing to copy sends nothing")

	oversized := target
	oversized.Manifest.ReleaseStatement = make([]byte, sdk.FrigateRelayRequestMaxBytes)
	_, err = newTestMiner(t, sender).FrigateStagePackage(t.Context(), pkg, []interfaces.FrigateStageTarget{oversized})
	require.True(t, devicewrite.IsNotDispatched(err), err)
	require.Nil(t, sender.cmd)

	_, err = newTestMiner(t, &fakeSender{err: control.ErrCommandProtocolUpgradeRequired}).FrigateStagePackage(t.Context(), pkg, []interfaces.FrigateStageTarget{target})
	require.ErrorContains(t, err, "Upgrade the Fleet Node")

	_, err = newTestMiner(t, &fakeSender{ack: frigateAck()}).FrigateStagePackage(t.Context(), pkg, []interfaces.FrigateStageTarget{target})
	require.ErrorContains(t, err, "expected 1", "one outcome for each target")

	// A node that is still copying must not get a second copy of the same stage.
	require.Greater(t, remoteFrigateStageCommandTimeout, 10*time.Minute)
}

func protoOSTestManifest(instanceID string) sdk.FrigateManifest {
	m := frigateTestManifest(instanceID)
	m.Package, m.ReleaseTag, m.AssetName, m.TargetVersion = sdk.FrigatePackageProtoOS, "v0.6.2", sdk.ProtoOSAssetName("v0.6.2"), "0.6.2"
	return m
}

func TestProtoOSStagingRequiresV21(t *testing.T) {
	pkg := interfaces.FrigatePackageRef{ID: frigateTestPackage, Filename: sdk.ProtoOSAssetName("v0.6.2"), SizeBytes: 4096, SHA256: frigateTestDigest}
	stage := func(m sdk.FrigateManifest) []interfaces.FrigateStageTarget {
		return []interfaces.FrigateStageTarget{{InstanceID: "a", Manifest: m, ManifestSHA256: frigateTestFingerprint}}
	}

	t.Run("each call names the protocol that its package needs", func(t *testing.T) {
		for name, tc := range map[string]struct {
			call func(*Miner) error
			want gatewaypb.CommandProtocolVersion
		}{
			"ProtoOS begin": {want: ProtoOSUpdateCommandProtocolVersion, call: func(m *Miner) error {
				_, err := m.FrigateRelay().FrigateStageBegin(t.Context(), "a", protoOSTestManifest("a"))
				return err
			}},
			"ProtoOS stage": {want: ProtoOSUpdateCommandProtocolVersion, call: func(m *Miner) error {
				_, err := m.FrigateStagePackage(t.Context(), pkg, stage(protoOSTestManifest("a")))
				return err
			}},
			"controller software begin": {want: FrigateUpdateCommandProtocolVersion, call: func(m *Miner) error {
				_, err := m.FrigateRelay().FrigateStageBegin(t.Context(), "a", frigateTestManifest("a"))
				return err
			}},
			"controller software stage": {want: FrigateUpdateCommandProtocolVersion, call: func(m *Miner) error {
				_, err := m.FrigateStagePackage(t.Context(), pkg, stage(frigateTestManifest("a")))
				return err
			}},
			"support": {want: FrigateUpdateCommandProtocolVersion, call: func(m *Miner) error {
				_, err := m.FrigateRelay().FrigateUpdateSupport(t.Context())
				return err
			}},
		} {
			sender := &fakeSender{ack: frigateAck(&gatewaypb.FrigateUpdateOutcome{Outcome: &gatewaypb.FrigateUpdateOutcome_Error{Error: "controller a did not answer"}})}
			_ = tc.call(newTestMiner(t, sender))
			require.NotNil(t, sender.cmd, name)
			require.Equal(t, tc.want, sender.minimumCommandProtocolVersion, name)
		}
		require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V21, ProtoOSUpdateCommandProtocolVersion)
	})

	t.Run("a V20 node is refused for ProtoOS before anything is sent", func(t *testing.T) {
		registry := control.NewRegistry()
		stream, err := registry.RegisterAuthenticated(7, "v20", gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V20)
		require.NoError(t, err)
		defer stream.Unregister()
		miner := newTestMiner(t, registry)

		for name, call := range map[string]func() error{
			"check": func() error { return miner.CheckProtoOSUpdates(t.Context()) },
			"begin": func() error {
				_, err := miner.FrigateRelay().FrigateStageBegin(t.Context(), "a", protoOSTestManifest("a"))
				return err
			},
			"stage": func() error {
				_, err := miner.FrigateStagePackage(t.Context(), pkg, stage(protoOSTestManifest("a")))
				return err
			},
		} {
			err := call()
			require.True(t, fleeterror.IsFailedPreconditionError(err), "%s: %v", name, err)
			require.ErrorContains(t, err, ProtoOSUpdateUpgradeReason, name)
		}
		select {
		case <-stream.Outgoing:
			t.Fatal("a ProtoOS call was sent to a node that cannot run it")
		default:
		}

		// A controller software call still goes to the same node.
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		go func() { _, _ = miner.FrigateRelay().FrigateStageBegin(ctx, "a", frigateTestManifest("a")) }()
		select {
		case <-stream.Outgoing:
		case <-time.After(time.Second):
			t.Fatal("a controller software call was not sent to a V20 node")
		}
	})

	t.Run("a V21 node runs the ProtoOS stage", func(t *testing.T) {
		registry := control.NewRegistry()
		stream, err := registry.RegisterAuthenticated(7, "v21", ProtoOSUpdateCommandProtocolVersion)
		require.NoError(t, err)
		defer stream.Unregister()
		require.NoError(t, newTestMiner(t, registry).CheckProtoOSUpdates(t.Context()))

		response, err := sdk.ServeFrigateRelay(t.Context(), committed{staged: 4096}, mustCommitRequest(t))
		require.NoError(t, err)
		sender := &fakeSender{ack: frigateAck(&gatewaypb.FrigateUpdateOutcome{Outcome: &gatewaypb.FrigateUpdateOutcome_Response{Response: response}})}
		outcomes, err := newTestMiner(t, sender).FrigateStagePackage(t.Context(), pkg, stage(protoOSTestManifest("a")))
		require.NoError(t, err)
		require.Len(t, outcomes, 1)
		require.NoError(t, outcomes[0].Err)
		require.Equal(t, sdk.FrigateStageCommit{JobID: frigateTestJob, BytesStaged: 4096, SHA256: frigateTestDigest}, outcomes[0].Commit)
		_, action := sentFrigateUpdate(t, sender.cmd)
		require.True(t, sdk.FrigateRelayStagesProtoOS(action.GetStage().GetTargets()[0].GetBeginRequest()))
	})
}

// committed answers a stage commit with a fixed size.
type committed struct {
	sdk.DeviceFrigateUpdater
	staged int64
}

func (c committed) FrigateStageCommit(_ context.Context, _ string, r sdk.FrigateStageRef) (sdk.FrigateStageCommit, error) {
	return sdk.FrigateStageCommit{JobID: r.JobID, BytesStaged: c.staged, SHA256: frigateTestDigest}, nil
}

// mustCommitRequest encodes a stage-commit call as the relay sends it.
func mustCommitRequest(t *testing.T) []byte {
	t.Helper()
	var request []byte
	relay := sdk.NewFrigateRelay(func(_ context.Context, encoded []byte) ([]byte, error) {
		request = encoded
		return nil, context.Canceled
	})
	_, _ = relay.FrigateStageCommit(t.Context(), "a", sdk.FrigateStageRef{JobID: frigateTestJob, ManifestSHA256: frigateTestFingerprint})
	require.NotEmpty(t, request)
	return request
}
