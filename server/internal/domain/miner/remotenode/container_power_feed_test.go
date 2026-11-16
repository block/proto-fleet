package remotenode

import (
	"encoding/json"
	"testing"
	"time"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
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
	require.True(t, physical, "an unclassified feed operation stays fenced")
}

func TestContainerPowerFeedRejectsInvalidRequestsBeforeSending(t *testing.T) {
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}}
	miner := newTestMiner(t, sender)
	_, err := miner.ContainerPowerFeed(t.Context(), sdk.ContainerPowerFeedRequest{Action: "cycle", Module: sdk.ContainerPowerFeedModule{Tank: 1, Module: 1}})
	require.True(t, devicewrite.IsNotDispatched(err))
	require.Nil(t, sender.cmd)
}
