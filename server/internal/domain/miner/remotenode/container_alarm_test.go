package remotenode

import (
	"testing"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/commandtype"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestContainerAlarmCommandCarriesActionAndRequiresAlarmProtocol(t *testing.T) {
	for _, action := range []string{sdk.ContainerAlarmActionMute, sdk.ContainerAlarmActionReset} {
		t.Run(action, func(t *testing.T) {
			sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}}
			miner := newTestMiner(t, sender)
			require.NoError(t, miner.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: action}))
			require.Equal(t, ContainerAlarmCommandProtocolVersion, sender.minimumCommandProtocolVersion)
			var wire gatewaypb.AgentCommand
			require.NoError(t, proto.Unmarshal(sender.cmd.Payload, &wire))
			got := wire.GetMinerCommand().GetRunContainerAlarmAction()
			require.NotNil(t, got)
			require.Equal(t, action, got.GetAction())
		})
	}
}

func TestContainerAlarmCommandIsPhysicalPlantWrite(t *testing.T) {
	kind, physical := physicalWriteKind(&gatewaypb.MinerCommand{Action: &gatewaypb.MinerCommand_RunContainerAlarmAction{RunContainerAlarmAction: &gatewaypb.RunContainerAlarmAction{Action: sdk.ContainerAlarmActionMute}}})
	require.True(t, physical)
	require.Equal(t, commandtype.ContainerAlarmAction, kind)
}

func TestContainerAlarmCommandRejectsInvalidActionBeforeDispatch(t *testing.T) {
	sender := &fakeSender{ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true}}
	miner := newTestMiner(t, sender)
	err := miner.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: "silence"})
	require.True(t, devicewrite.IsNotDispatched(err))
	require.Nil(t, sender.cmd)
}

func TestContainerAlarmCommandRefusesFleetNodeBelowAlarmProtocolBeforeDispatch(t *testing.T) {
	registry := control.NewRegistry()
	stream, err := registry.RegisterAuthenticated(7, "session", ContainerAlarmCommandProtocolVersion-1)
	require.NoError(t, err)
	defer stream.Unregister()
	miner := newTestMiner(t, registry)
	err = miner.RunContainerAlarmAction(t.Context(), sdk.ContainerAlarmRequest{Action: sdk.ContainerAlarmActionMute})
	require.ErrorIs(t, err, control.ErrCommandProtocolUpgradeRequired)
	require.ErrorContains(t, err, "upgrade Fleet Node")
	require.True(t, devicewrite.IsNotDispatched(err), "an old node must be refused before anything is sent")
	select {
	case <-stream.Outgoing:
		t.Fatal("a plant alarm command was enqueued for a node that cannot run it")
	default:
	}
}
