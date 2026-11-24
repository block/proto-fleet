package remotenode

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestBoardIdentifyCommandPreservesPlacementAndIdentityAndRequiresV10(t *testing.T) {
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}}
	miner := newTestMiner(t, sender)
	req := sdk.BoardIdentifyRequest{Mode: sdk.BoardIdentifyBlink, Board: sdk.BoardIdentifyTarget{Tank: 2, Module: 5, Slot: 3, ModuleReportedID: "psu-5", MACAddress: "aa:bb:cc:dd:ee:ff"}}
	require.NoError(t, miner.SetBoardIdentify(t.Context(), req))
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V10, sender.minimumCommandProtocolVersion)
	var wire gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &wire))
	action := wire.GetMinerCommand().GetSetBoardIdentify()
	require.NotNil(t, action)
	require.EqualValues(t, 2, action.Tank)
	require.EqualValues(t, 5, action.Module)
	require.EqualValues(t, 3, action.Slot)
	require.Equal(t, "psu-5", action.ModuleReportedId)
	require.Equal(t, "aa:bb:cc:dd:ee:ff", action.MacAddress)
	require.Equal(t, "blink", action.Mode)
}

func TestBoardIdentifyIsIndicatorControlAndRejectsInvalidRequests(t *testing.T) {
	_, physical := physicalWriteKind(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_SetBoardIdentify{SetBoardIdentify: &gatewaypb.SetBoardIdentifyAction{}}})
	require.False(t, physical, "identify LEDs must not take physical-write admission")

	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}}
	miner := newTestMiner(t, sender)
	err := miner.SetBoardIdentify(t.Context(), sdk.BoardIdentifyRequest{Mode: "flash", Board: sdk.BoardIdentifyTarget{Tank: 1, Module: 1, Slot: 1, ModuleReportedID: "m", MACAddress: "a"}})
	require.True(t, devicewrite.IsNotDispatched(err))
	require.Nil(t, sender.cmd)
}
