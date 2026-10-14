package remotenode

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestModuleMiningCommandPreservesScopeAndIdentityAndRequiresV5(t *testing.T) {
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}}
	miner := newTestMiner(t, sender)
	req := sdk.ModuleMiningRequest{Mining: new(false), Modules: []sdk.ModuleMiningTarget{{Tank: 1, Module: 3, ReportedID: "a"}, {Tank: 2, Module: 3, ReportedID: "b"}}}
	require.NoError(t, miner.SetModuleMining(t.Context(), req))
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V5, sender.minimumCommandProtocolVersion)
	var wire gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &wire))
	action := wire.GetMinerCommand().GetSetModuleMining()
	require.NotNil(t, action)
	require.False(t, *action.Mining)
	require.Len(t, action.Modules, 2)
	require.EqualValues(t, 1, action.Modules[0].Tank)
	require.EqualValues(t, 3, action.Modules[0].Module)
	require.Equal(t, "a", action.Modules[0].ReportedId)
	require.EqualValues(t, 2, action.Modules[1].Tank)
	require.Equal(t, "b", action.Modules[1].ReportedId)
}
