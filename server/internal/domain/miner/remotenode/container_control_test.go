package remotenode

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestEquipmentCommandRequiresV2AndPreservesFalse(t *testing.T) {
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}}
	miner := newTestMiner(t, sender)
	require.NoError(t, miner.SetContainerControl(t.Context(), sdk.ContainerControlRequest{Name: "dry_cooler", Enabled: new(false)}))
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V2, sender.minimumCommandProtocolVersion)
	var wire gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &wire))
	action := wire.GetMinerCommand().GetSetContainerControl()
	require.NotNil(t, action.Enabled)
	require.False(t, *action.Enabled)
	require.Equal(t, "dry_cooler", action.Name)
	require.NoError(t, miner.Reboot(t.Context()))
	require.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, sender.minimumCommandProtocolVersion)
}
