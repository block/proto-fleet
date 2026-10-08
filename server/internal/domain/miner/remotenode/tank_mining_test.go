package remotenode

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestTankMiningCommandPreservesExactScopeAndRequiresV3(t *testing.T) {
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}}
	miner := newTestMiner(t, sender)
	require.NoError(t, miner.SetTankMining(t.Context(), sdk.TankMiningRequest{Positions: []int32{1, 3}, Mining: new(false)}))
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V3, sender.minimumCommandProtocolVersion)
	var wire gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &wire))
	action := wire.GetMinerCommand().GetSetTankMining()
	require.NotNil(t, action)
	require.Equal(t, []int32{1, 3}, action.Positions)
	require.False(t, *action.Mining)
}
