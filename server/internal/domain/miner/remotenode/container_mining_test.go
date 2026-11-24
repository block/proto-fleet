package remotenode

import (
	"encoding/json"
	"errors"
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestContainerMiningRequiresV8AndClassifiesReadsAndWrites(t *testing.T) {
	for _, req := range []sdk.ContainerMiningRequest{{Action: "status"}, {Action: "power_target"}, {Action: "set_mining", Mining: new(false)}, {Action: "set_power_target", Power: &sdk.ContainerPowerTarget{Watts: 2500, BalanceBays: new(false)}}} {
		t.Run(req.Action, func(t *testing.T) {
			result := sdk.ContainerMiningResult{}
			if req.Action == "status" {
				result = sdk.ContainerMiningResult{Status: &sdk.ContainerMiningStatus{Mining: new(true)}}
			}
			if req.Action == "power_target" {
				result = sdk.ContainerMiningResult{Power: &sdk.ContainerPowerState{Watts: new(2500.0)}}
			}
			payload, err := json.Marshal(result)
			require.NoError(t, err)
			sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: payload}}
			miner := newTestMiner(t, sender)
			got, err := miner.ContainerMining(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, result, got)
			require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V8, sender.minimumCommandProtocolVersion)
			var wire gatewaypb.AgentCommand
			require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &wire))
			decoded, err := sdk.DecodeContainerMiningRequest(wire.GetMinerCommand().GetContainerMining().Request)
			require.NoError(t, err)
			require.Equal(t, req, decoded)
			kind, write := physicalWriteKind(wire.GetMinerCommand())
			require.Equal(t, commandtype.ContainerMining, kind)
			require.Equal(t, !req.ReadOnly(), write)
		})
	}
}

func TestContainerMiningWriteRequiresSuccessfulAck(t *testing.T) {
	for _, request := range []sdk.ContainerMiningRequest{
		{Action: "set_mining", Mining: new(false)},
		{Action: "set_power_target", Power: &sdk.ContainerPowerTarget{Watts: 2500}},
	} {
		t.Run(request.Action, func(t *testing.T) {
			for _, sender := range []*fakeSender{
				{err: errors.New("node disconnected")},
				{ack: &gatewaypb.ControlAck{Succeeded: false, Payload: []byte(`{}`)}},
			} {
				_, err := newTestMiner(t, sender).ContainerMining(t.Context(), request)
				require.Error(t, err)
			}
		})
	}
}
