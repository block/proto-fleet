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
		ack, err := m.send(ctx, frigateUpdateCommand(&gatewaypb.FrigateUpdateAction{Call: &gatewaypb.FrigateUpdateAction_Request{Request: request}}))
		outcomes, err := frigateUpdateOutcomes(ack, err, 1)
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
func frigateUpdateOutcomes(ack *gatewaypb.ControlAck, err error, want int) ([]frigateUpdateOutcome, error) {
	if err != nil {
		if errors.Is(err, control.ErrCommandProtocolUpgradeRequired) {
			return nil, devicewrite.NotDispatched(fleeterror.NewFailedPreconditionError(FrigateUpdateUpgradeReason))
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
	command := frigateUpdateCommand(&gatewaypb.FrigateUpdateAction{Call: &gatewaypb.FrigateUpdateAction_Stage{Stage: stage}})
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
	outcomes, err := frigateUpdateOutcomes(ack, err, len(targets))
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
