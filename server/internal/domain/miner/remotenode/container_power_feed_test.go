package remotenode

import (
	"encoding/json"
	"testing"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestContainerPowerFeedIsReadOnlyAndRequiresV13(t *testing.T) {
	req := sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedInspect, Module: sdk.ContainerPowerFeedModule{Tank: 2, Module: 5, ReportedID: "module-x"}}
	result := sdk.ContainerPowerFeedResult{Inspection: &sdk.ModulePowerFeedInspection{ObservedAt: time.Now().UTC(), Mapping: sdk.PowerFeedMapping{Confidence: sdk.PowerFeedMappingUnavailable}}}
	payload, err := json.Marshal(result)
	require.NoError(t, err)
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload}}
	miner := newTestMiner(t, sender)
	got, err := miner.ContainerPowerFeed(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, sdk.PowerFeedMappingUnavailable, got.Inspection.Mapping.Confidence)
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V13, sender.minimumCommandProtocolVersion)
	var wire gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &wire))
	action := wire.GetMinerCommand().GetContainerPowerFeed()
	require.NotNil(t, action)
	decoded, err := sdk.DecodeContainerPowerFeedRequest(action.Request)
	require.NoError(t, err)
	require.Equal(t, req, decoded)
	_, physical := physicalWriteKind(wire.GetMinerCommand())
	require.False(t, physical, "inspection must not take physical-write admission")
	_, physical = physicalWriteKind(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_ContainerPowerFeed{ContainerPowerFeed: &gatewaypb.ContainerPowerFeedAction{Request: []byte(`{"action":"cycle"}`)}}})
	require.True(t, physical, "an undecodable feed operation stays fenced")
}

func TestContainerPowerFeedWriteTakesPhysicalWriteAdmissionAndRequiresV16(t *testing.T) {
	req := sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedTurnOff, FeedID: "pdu-2-outlet-05", Module: sdk.ContainerPowerFeedModule{Tank: 2, Module: 5, ReportedID: "module-x"}}
	payload, err := json.Marshal(sdk.ContainerPowerFeedResult{Applied: &sdk.PowerFeedReading{ID: req.FeedID, State: sdk.PowerFeedStateDisabled, Overridden: true}})
	require.NoError(t, err)
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload}}
	miner := newTestMiner(t, sender)
	got, err := miner.ContainerPowerFeed(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, sdk.PowerFeedStateDisabled, got.Applied.State)
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V16, sender.minimumCommandProtocolVersion, "an older node only inspects")
	data, err := json.Marshal(req)
	require.NoError(t, err)
	kind, physical := physicalWriteKind(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_ContainerPowerFeed{ContainerPowerFeed: &gatewaypb.ContainerPowerFeedAction{Request: data}}})
	require.True(t, physical)
	require.Equal(t, commandtype.ControlPowerFeed, kind)
}

func TestContainerPowerFeedRejectsInvalidRequestsBeforeSending(t *testing.T) {
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}}
	miner := newTestMiner(t, sender)
	_, err := miner.ContainerPowerFeed(t.Context(), sdk.ContainerPowerFeedRequest{Action: "cycle", Module: sdk.ContainerPowerFeedModule{Tank: 1, Module: 1}})
	require.True(t, devicewrite.IsNotDispatched(err))
	require.Nil(t, sender.cmd)
}

// The controller's refusal reaches the operator with its reason, as it does
// from a directly connected controller, and stays proven unsent.
func TestContainerPowerFeedKeepsControllerRefusalReason(t *testing.T) {
	req := sdk.ContainerPowerFeedRequest{Action: sdk.ContainerPowerFeedTurnOff, FeedID: "pdu-2-outlet-05", Module: sdk.ContainerPowerFeedModule{Tank: 2, Module: 5, ReportedID: "module-x"}}
	reason := "container power feed: tank 2 module 5 is now supplied by pdu-9, not pdu-2-outlet-05"
	sender := &fakeSender{ack: &gatewaypb.ControlAck{
		Code: gatewaypb.AckCode_ACK_CODE_FAILED_PRECONDITION, ErrorMessage: reason,
		WriteOutcome: &gatewaypb.DeviceWriteOutcome{Outcome: sdk.WriteNotDispatched, Stage: string(sdk.WriteStageConfigurationRead), Reason: string(sdk.WriteReasonStaleBinding)},
	}}
	_, err := newTestMiner(t, sender).ContainerPowerFeed(t.Context(), req)
	require.True(t, fleeterror.IsFailedPreconditionError(err), err)
	require.True(t, devicewrite.IsNotDispatched(err))
	require.ErrorContains(t, err, reason)
	require.NotContains(t, err.Error(), "fleet node reported command failure")
}
