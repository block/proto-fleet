package main

import (
	"context"
	"errors"
	"sync"

	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// maxFrigateUpdateTextBytes is the limit the gateway contract sets for an
// outcome's message.
const maxFrigateUpdateTextBytes = 4096

// runFrigateUpdateAction runs one controller software update call on the
// plugin, next to the controllers. A refusal from a controller is an outcome,
// not a failed command, so Fleet gets its reason unchanged.
//
// A plugin device has the update calls whether or not its driver supports
// them. A driver that does not support them answers each call as unsupported.
func runFrigateUpdateAction(ctx context.Context, client gatewayClient, commandID, deviceIdentifier, tempRoot string, dev sdk.Device, action *pb.FrigateUpdateAction) ([]byte, error) {
	updater, ok := dev.(sdk.DeviceFrigateUpdater)
	if !ok {
		return nil, sdk.UnsupportedWrite(sdk.CapabilityFrigateServerUpdate)
	}
	var outcomes []*pb.FrigateUpdateOutcome
	switch call := action.GetCall().(type) {
	case *pb.FrigateUpdateAction_Request:
		response, err := sdk.ServeFrigateRelay(ctx, updater, call.Request)
		if errors.Is(err, sdk.ErrFrigateRelayRequest) {
			return nil, sdk.InvalidWriteRequest(cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "%s", err.Error()))
		}
		if err != nil && sdk.AsFrigateRefusal(err) == nil {
			return nil, err
		}
		outcomes = []*pb.FrigateUpdateOutcome{frigateUpdateOutcome(response, err)}
	case *pb.FrigateUpdateAction_Stage:
		var err error
		outcomes, err = runFrigateUpdateStage(ctx, client, commandID, deviceIdentifier, tempRoot, updater, call.Stage)
		if err != nil {
			return nil, err
		}
	}
	payload, err := proto.Marshal(&pb.FrigateUpdateResult{Outcomes: outcomes})
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_INTERNAL, "encode controller software update result: %v", err)
	}
	return payload, nil
}

// runFrigateUpdateStage downloads the package once and copies it to all the
// targets at the same time, as Fleet does for controllers that it reaches
// directly. A target that fails does not stop the others: each gets its own
// outcome, and Fleet sends the stage again for the ones that failed.
func runFrigateUpdateStage(ctx context.Context, client gatewayClient, commandID, deviceIdentifier, tempRoot string, updater sdk.DeviceFrigateUpdater, stage *pb.FrigateUpdateStage) ([]*pb.FrigateUpdateOutcome, error) {
	ref := stage.GetPackage()
	// The package shares the firmware download limits and temporary space.
	if err := checkCommandArtifact(ref, pb.CommandArtifactPurpose_COMMAND_ARTIFACT_PURPOSE_CONTROLLER_SOFTWARE_PACKAGE, "controller software package"); err != nil {
		return nil, sdk.InvalidWriteRequest(err)
	}
	file, done, err := openCommandArtifact(ctx, client, commandID, deviceIdentifier, tempRoot, ref)
	if err != nil {
		return nil, err
	}
	defer done()

	pkg := sdk.FrigateRelayPackage{Data: file, SizeBytes: ref.GetSizeBytes()}
	outcomes := make([]*pb.FrigateUpdateOutcome, len(stage.GetTargets()))
	var wg sync.WaitGroup
	for i, target := range stage.GetTargets() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := sdk.ServeFrigateRelayStage(ctx, updater, target.GetBeginRequest(), target.GetManifestSha256(), pkg)
			outcomes[i] = frigateUpdateOutcome(response, err)
		}()
	}
	wg.Wait()
	return outcomes, nil
}

// frigateUpdateOutcome reports one call: its response, the controller's
// refusal, or the reason a copy failed.
func frigateUpdateOutcome(response []byte, err error) *pb.FrigateUpdateOutcome {
	if err == nil {
		return &pb.FrigateUpdateOutcome{Outcome: &pb.FrigateUpdateOutcome_Response{Response: response}}
	}
	if refusal := sdk.AsFrigateRefusal(err); refusal != nil {
		return &pb.FrigateUpdateOutcome{Outcome: &pb.FrigateUpdateOutcome_Refusal{Refusal: &pb.FrigateUpdateRefusal{
			Reason:  truncateUTF8(refusal.Reason, 128),
			Message: truncateUTF8(refusal.Message, maxFrigateUpdateTextBytes),
			Code:    uint32(refusal.Code),
		}}}
	}
	return &pb.FrigateUpdateOutcome{Outcome: &pb.FrigateUpdateOutcome_Error{Error: truncateUTF8(err.Error(), maxFrigateUpdateTextBytes)}}
}
