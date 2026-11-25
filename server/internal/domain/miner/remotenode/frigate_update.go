package remotenode

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// FrigateUpdateCommandProtocolVersion is the oldest command protocol whose
// nodes run controller software update calls.
const FrigateUpdateCommandProtocolVersion = gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V20

// FrigateUpdateUpgradeReason tells the operator why an older node is refused.
const FrigateUpdateUpgradeReason = "The Fleet Node that manages this container does not support controller software updates. Upgrade the Fleet Node, then try again."

// ProtoOSUpdateCommandProtocolVersion is the oldest command protocol whose
// nodes stage the ProtoOS package. An older node ignores the manifest's package
// selector and would stage the package as controller software.
const ProtoOSUpdateCommandProtocolVersion = gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V21

// ProtoOSUpdateUpgradeReason tells the operator why a node older than
// ProtoOSUpdateCommandProtocolVersion is refused.
const ProtoOSUpdateUpgradeReason = "The Fleet Node that manages this container does not support ProtoOS updates. Upgrade the Fleet Node, then try again."

// frigateUpdateCommandVersion returns the oldest command protocol whose nodes
// run action, and the operator text for an older node.
func frigateUpdateCommandVersion(action *gatewaypb.FrigateUpdateAction) (gatewaypb.CommandProtocolVersion, string) {
	requests := [][]byte{action.GetRequest()}
	if stage := action.GetStage(); stage != nil {
		requests = requests[:0]
		for _, target := range stage.GetTargets() {
			requests = append(requests, target.GetBeginRequest())
		}
	}
	for _, request := range requests {
		if sdk.FrigateRelayStagesProtoOS(request) {
			return ProtoOSUpdateCommandProtocolVersion, ProtoOSUpdateUpgradeReason
		}
	}
	return FrigateUpdateCommandProtocolVersion, FrigateUpdateUpgradeReason
}

// CheckProtoOSUpdates refuses a ProtoOS update before Fleet reads or stages
// anything, when the connected node cannot stage the ProtoOS package. A node
// that is not connected keeps the usual handling of each call.
func (m *Miner) CheckProtoOSUpdates(context.Context) error {
	checker, ok := m.sender.(interface {
		CommandProtocolUpgradeRequiredForDispatch(fleetNodeID int64, minimum gatewaypb.CommandProtocolVersion) bool
	})
	if ok && checker.CommandProtocolUpgradeRequiredForDispatch(m.fleetNodeID, ProtoOSUpdateCommandProtocolVersion) {
		return fleeterror.NewFailedPreconditionError(ProtoOSUpdateUpgradeReason)
	}
	return nil
}

// The node bounds a package copy with its firmware command budget of 10
// minutes. Fleet waits a little longer, so a new attempt never overlaps one
// that the node still runs.
var remoteFrigateStageCommandTimeout = 10*time.Minute + 30*time.Second

var _ interfaces.FrigatePackageStager = (*Miner)(nil)

// FrigateRelay runs each update call on the assigned Fleet Node, next to the
// controller. Only the encoded call and its response cross the gateway. The
// relay refuses FrigateStageAppend: package bytes do not cross the control
// stream. Use FrigateStagePackage.
func (m *Miner) FrigateRelay() sdk.FrigateRelay {
	return sdk.NewFrigateRelay(func(ctx context.Context, request []byte) ([]byte, error) {
		action := &gatewaypb.FrigateUpdateAction{Call: &gatewaypb.FrigateUpdateAction_Request{Request: request}}
		ack, err := m.send(ctx, frigateUpdateCommand(action))
		outcomes, err := frigateUpdateOutcomes(action, ack, err, 1)
		if err != nil {
			return nil, err
		}
		if outcomes[0].err != nil {
			return nil, outcomes[0].err
		}
		return outcomes[0].response, nil
	})
}

func frigateUpdateCommand(action *gatewaypb.FrigateUpdateAction) *gatewaypb.MinerCommand {
	return &gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_FrigateUpdate{FrigateUpdate: action}}
}

type frigateUpdateOutcome struct {
	response []byte
	err      error
}

// frigateUpdateOutcomes reads the outcomes of one update command. A refusal
// from the controller becomes a *sdk.FrigateRefusal, as on a direct connection.
func frigateUpdateOutcomes(action *gatewaypb.FrigateUpdateAction, ack *gatewaypb.ControlAck, err error, want int) ([]frigateUpdateOutcome, error) {
	if err != nil {
		if errors.Is(err, control.ErrCommandProtocolUpgradeRequired) {
			_, reason := frigateUpdateCommandVersion(action)
			return nil, devicewrite.NotDispatched(fleeterror.NewFailedPreconditionError(reason))
		}
		return nil, err
	}
	// A read skips the write gate, so its failed ack is still unchecked here.
	if err := ackToError(ack); err != nil {
		return nil, err
	}
	var result gatewaypb.FrigateUpdateResult
	if err := proto.Unmarshal(ack.GetPayload(), &result); err != nil {
		return nil, fleeterror.NewInternalErrorf("decode fleet node controller software update result: %v", err)
	}
	if len(result.GetOutcomes()) != want {
		return nil, fleeterror.NewInternalErrorf("fleet node returned %d controller software update outcomes, expected %d", len(result.GetOutcomes()), want)
	}
	outcomes := make([]frigateUpdateOutcome, 0, want)
	for _, outcome := range result.GetOutcomes() {
		switch o := outcome.GetOutcome().(type) {
		case *gatewaypb.FrigateUpdateOutcome_Response:
			outcomes = append(outcomes, frigateUpdateOutcome{response: o.Response})
		case *gatewaypb.FrigateUpdateOutcome_Refusal:
			outcomes = append(outcomes, frigateUpdateOutcome{err: &sdk.FrigateRefusal{
				Reason: o.Refusal.GetReason(), Message: clampAckReason(o.Refusal.GetMessage()), Code: codes.Code(o.Refusal.GetCode()),
			}})
		case *gatewaypb.FrigateUpdateOutcome_Error:
			outcomes = append(outcomes, frigateUpdateOutcome{err: errors.New(clampAckReason(o.Error))})
		default:
			return nil, fleeterror.NewInternalError("fleet node returned an empty controller software update outcome")
		}
	}
	return outcomes, nil
}

// FrigateStagePackage has the node download the package from Fleet once and
// copy it to each target. The node resumes each copy from the offset that the
// controller acknowledges.
func (m *Miner) FrigateStagePackage(ctx context.Context, pkg interfaces.FrigatePackageRef, targets []interfaces.FrigateStageTarget) ([]interfaces.FrigateStageOutcome, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	ref := &gatewaypb.CommandArtifactRef{
		ArtifactId: pkg.ID,
		Purpose:    gatewaypb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE,
		Filename:   pkg.Filename,
		SizeBytes:  pkg.SizeBytes,
		Sha256:     pkg.SHA256,
	}
	stage := &gatewaypb.FrigateUpdateStage{Package: ref}
	for _, target := range targets {
		begin, err := sdk.EncodeFrigateRelayStageBegin(target.InstanceID, target.Manifest)
		if err != nil {
			return nil, devicewrite.NotDispatched(fleeterror.NewInvalidArgumentErrorf("invalid controller software stage for %s: %v", target.InstanceID, err))
		}
		stage.Targets = append(stage.Targets, &gatewaypb.FrigateUpdateStageTarget{BeginRequest: begin, ManifestSha256: target.ManifestSHA256})
	}
	action := &gatewaypb.FrigateUpdateAction{Call: &gatewaypb.FrigateUpdateAction_Stage{Stage: stage}}
	command := frigateUpdateCommand(action)
	expectation := control.ArtifactExpectation{
		Direction:        control.ArtifactDirectionDownload,
		Purpose:          ref.GetPurpose(),
		ArtifactID:       ref.GetArtifactId(),
		DeviceIdentifier: m.desc.GetDeviceIdentifier(),
	}

	release, err := m.acquireGate(ctx, command)
	if err != nil {
		return nil, err
	}
	defer release()
	stageCtx, cancel := context.WithTimeout(ctx, remoteFrigateStageCommandTimeout)
	defer cancel()
	ack, _, err := m.sendInSlotWithArtifactResults(stageCtx, command, []control.ArtifactExpectation{expectation})
	outcomes, err := frigateUpdateOutcomes(action, ack, err, len(targets))
	if err != nil {
		return nil, err
	}

	results := make([]interfaces.FrigateStageOutcome, len(targets))
	for i, outcome := range outcomes {
		if outcome.err != nil {
			results[i].Err = outcome.err
			continue
		}
		commit, err := sdk.DecodeFrigateRelayStageCommit(outcome.response)
		if err != nil {
			results[i].Err = fmt.Errorf("decode the fleet node's stage result for %s: %w", targets[i].InstanceID, err)
			continue
		}
		results[i].Commit = commit
	}
	return results, nil
}
