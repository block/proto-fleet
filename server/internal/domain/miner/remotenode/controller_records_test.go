package remotenode

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestControllerRecordsCommandRequiresV11AndDecodesBoundedResult(t *testing.T) {
	req := sdk.ControllerRecordsRequest{Kind: sdk.ControllerRecordsKindLogs, ControllerID: "ct1-a", Logs: &sdk.ControllerLogQuery{Limit: 10, Component: "plant"}}
	want := sdk.ControllerRecordsResult{ControllerID: "ct1-a", ControllerIDs: []string{"ct1-a"}, Logs: &sdk.ControllerLogSnapshot{Coverage: sdk.ControllerLogNoResponse}}
	payload, err := sdk.EncodeControllerRecords(want)
	require.NoError(t, err)
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload}}
	got, err := newTestMiner(t, sender).ControllerRecords(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V11, sender.minimumCommandProtocolVersion)

	var wire gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &wire))
	action := wire.GetMinerCommand().GetControllerRecords()
	require.NotNil(t, action)
	decoded, err := sdk.DecodeControllerRecordsRequest(action.Request)
	require.NoError(t, err)
	require.Equal(t, req, decoded)

	_, physical := physicalWriteKind(wire.GetMinerCommand())
	require.False(t, physical, "reading controller records is not a physical write")
}

func TestControllerRecordsCommandRejectsInvalidRequestsAndResults(t *testing.T) {
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: []byte(`{"audit":{"events":[],"limit_reached":false}}`)}}
	miner := newTestMiner(t, sender)
	_, err := miner.ControllerRecords(t.Context(), sdk.ControllerRecordsRequest{Kind: sdk.ControllerRecordsKindLogs})
	require.True(t, devicewrite.IsNotDispatched(err))
	require.Nil(t, sender.cmd)

	_, err = miner.ControllerRecords(t.Context(), sdk.ControllerRecordsRequest{Kind: sdk.ControllerRecordsKindLogs, Logs: &sdk.ControllerLogQuery{Limit: 1}})
	require.Error(t, err, "an audit page cannot answer a log read")
}
