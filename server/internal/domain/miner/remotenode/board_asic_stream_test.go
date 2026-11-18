package remotenode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

var streamBoard = sdk.BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, ModuleReportedID: "mod-2", MACAddress: "aa:bb:cc:dd:ee:ff"}

// newRegistryMiner returns a miner whose sender is a real registry, plus the
// node's side of its control stream.
func newRegistryMiner(t *testing.T, version gatewaypb.CommandProtocolVersion) (*Miner, *control.Stream) {
	t.Helper()
	registry := control.NewRegistry()
	stream, err := registry.RegisterAuthenticated(1, "session", version)
	require.NoError(t, err)
	t.Cleanup(stream.Unregister)
	miner := newTestMiner(t, registry)
	miner.fleetNodeID = 1
	return miner, stream
}

// publishBoardFrame sends frame the way a node does: as a command frame's payload.
func publishBoardFrame(t *testing.T, node *control.Stream, commandID string, frame *gatewaypb.BoardASICStreamFrame) {
	t.Helper()
	payload, err := proto.Marshal(frame)
	require.NoError(t, err)
	node.PublishCommandFrame(&gatewaypb.CommandFrame{CommandId: commandID, Payload: payload})
}

func streamResult(t *testing.T, end gatewaypb.BoardASICStreamEnd) []byte {
	t.Helper()
	payload, err := proto.Marshal(&gatewaypb.StreamBoardASICReadingsResult{End: end})
	require.NoError(t, err)
	return payload
}

func TestBoardASICStreamThroughFleetNodeRelaysTheConfirmedBoard(t *testing.T) {
	miner, node := newRegistryMiner(t, control.CommandStreamProtocolVersion)
	observed := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	quarantined := true
	go func() {
		cmd := <-node.Outgoing
		publishBoardFrame(t, node, cmd.GetCommandId(), &gatewaypb.BoardASICStreamFrame{
			MacAddress: streamBoard.MACAddress, ObservedAt: timestamppb.New(observed),
			Layout: &gatewaypb.BoardASICStreamLayout{StackCount: 1, AsicsPerStack: 2, TotalAsics: 2, Positions: []*gatewaypb.BoardASICStreamPosition{{Index: 1, StackId: 1, StackIndex: 1, X: 1, Y: 0}}},
			Asics:  []*gatewaypb.BoardASICStreamReading{{Index: 1, TemperatureC: 61.5, HashrateGhs: 900, VoltageV: 0.3, FrequencyMhz: 500, PassRatePercent: 99, ThermalQuarantined: &quarantined}},
		})
		node.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: streamResult(t, gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CONTROLLER_ENDED)})
	}()
	var frames []sdk.BoardASICFrame
	err := miner.StreamBoardASICReadings(t.Context(), streamBoard, func(f sdk.BoardASICFrame) error {
		frames = append(frames, f)
		return nil
	})
	require.NoError(t, err, "a controller that ended the stream is reported the way a direct plugin reports it")
	require.Equal(t, []sdk.BoardASICFrame{{
		MACAddress: streamBoard.MACAddress, ObservedAt: observed,
		Layout: &sdk.BoardASICLayout{StackCount: 1, ASICsPerStack: 2, TotalASICs: 2, Positions: []sdk.BoardASICPosition{{Index: 1, StackID: 1, StackIndex: 1, X: 1}}},
		ASICs:  []sdk.BoardASICReading{{Index: 1, TemperatureC: 61.5, HashrateGHs: 900, VoltageV: 0.3, FrequencyMHz: 500, PassRatePercent: 99, ThermalQuarantined: &quarantined}},
	}}, frames)
}

func TestBoardASICStreamThroughFleetNodeAddressesTheBoardAndController(t *testing.T) {
	miner, node := newRegistryMiner(t, control.CommandStreamProtocolVersion)
	commands := make(chan *gatewaypb.ControlCommand, 1)
	go func() {
		cmd := <-node.Outgoing
		commands <- cmd
		node.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: streamResult(t, gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CONTROLLER_ENDED)})
	}()
	require.NoError(t, miner.StreamBoardASICReadings(t.Context(), streamBoard, func(sdk.BoardASICFrame) error { return nil }))
	var wire gatewaypb.AgentCommand
	require.NoError(t, proto.Unmarshal((<-commands).GetPayload(), &wire))
	mc := wire.GetMinerCommand()
	require.Equal(t, miner.desc.GetDeviceIdentifier(), mc.GetTarget().GetDeviceIdentifier())
	action := mc.GetStreamBoardAsicReadings()
	require.Equal(t, &gatewaypb.StreamBoardASICReadingsAction{Tank: 1, Module: 2, Slot: 3, ModuleReportedId: "mod-2", MacAddress: streamBoard.MACAddress}, action)
	_, physical := physicalWriteKind(mc)
	require.False(t, physical, "streaming readings is not a physical write")
}

func TestBoardASICStreamThroughFleetNodeMapsNodeOutcomes(t *testing.T) {
	for name, tc := range map[string]struct {
		ack  *gatewaypb.ControlAck
		code codes.Code
	}{
		"board changed":     {&gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_INTERNAL, Payload: streamResult(t, gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_BOARD_CHANGED)}, codes.FailedPrecondition},
		"unsupported":       {&gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNIMPLEMENTED, Payload: streamResult(t, gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNSUPPORTED)}, codes.Unimplemented},
		"no driver":         {&gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_AGENT_INCAPABLE, ErrorMessage: "no plugin"}, codes.Unimplemented},
		"node busy":         {&gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY, ErrorMessage: "agent at concurrency limit"}, codes.Unavailable},
		"node at capacity":  {&gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_BUSY, Payload: streamResult(t, gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_AT_CAPACITY)}, codes.ResourceExhausted},
		"controller failed": {&gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_INTERNAL, Payload: streamResult(t, gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNAVAILABLE)}, codes.Unavailable},
		"node lifetime":     {&gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: streamResult(t, gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CANCELED)}, codes.Unavailable},
	} {
		t.Run(name, func(t *testing.T) {
			miner, node := newRegistryMiner(t, control.CommandStreamProtocolVersion)
			go func() {
				cmd := <-node.Outgoing
				tc.ack.CommandId = cmd.GetCommandId()
				node.PublishAck(tc.ack)
			}()
			err := miner.StreamBoardASICReadings(t.Context(), streamBoard, func(sdk.BoardASICFrame) error { return nil })
			require.Equal(t, tc.code, status.Code(err), "got %v", err)
		})
	}
}

func TestBoardASICStreamThroughFleetNodeStopsForTheCaller(t *testing.T) {
	miner, node := newRegistryMiner(t, control.CommandStreamProtocolVersion)
	viewerGone := errors.New("viewer gone")
	go func() {
		cmd := <-node.Outgoing
		publishBoardFrame(t, node, cmd.GetCommandId(), &gatewaypb.BoardASICStreamFrame{})
	}()
	err := miner.StreamBoardASICReadings(t.Context(), streamBoard, func(sdk.BoardASICFrame) error { return viewerGone })
	require.ErrorIs(t, err, viewerGone, "the caller's send error is returned as is")
	require.NotEmpty(t, <-node.Cancels, "the node is asked to stop")

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-node.Outgoing
		cancel()
	}()
	err = miner.StreamBoardASICReadings(ctx, streamBoard, func(sdk.BoardASICFrame) error { return nil })
	require.Equal(t, codes.Canceled, status.Code(err))
	require.NotEmpty(t, <-node.Cancels)
}

func TestBoardASICStreamThroughFleetNodeNeedsANewEnoughNode(t *testing.T) {
	miner, _ := newRegistryMiner(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V14)
	err := miner.StreamBoardASICReadings(t.Context(), streamBoard, func(sdk.BoardASICFrame) error { return nil })
	require.True(t, fleeterror.IsFailedPreconditionError(err), "got %v", err)
	require.ErrorContains(t, err, BoardASICStreamUpgradeReason)

	disconnected := newTestMiner(t, control.NewRegistry())
	err = disconnected.StreamBoardASICReadings(t.Context(), streamBoard, func(sdk.BoardASICFrame) error { return nil })
	require.Equal(t, codes.Unavailable, status.Code(err), "a node that is reconnecting is retried")

	err = miner.StreamBoardASICReadings(t.Context(), sdk.BoardIdentifyTarget{}, func(sdk.BoardASICFrame) error { return nil })
	require.True(t, fleeterror.IsInvalidArgumentError(err))

	err = newTestMiner(t, &fakeSender{}).StreamBoardASICReadings(t.Context(), streamBoard, func(sdk.BoardASICFrame) error { return nil })
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestBoardASICStreamThroughFleetNodeKeepsTheLayoutOfASkippedFrame(t *testing.T) {
	miner, node := newRegistryMiner(t, control.CommandStreamProtocolVersion)
	frame := func(index int32, layout bool) *gatewaypb.BoardASICStreamFrame {
		f := &gatewaypb.BoardASICStreamFrame{Asics: []*gatewaypb.BoardASICStreamReading{{Index: index}}}
		if layout {
			f.Layout = &gatewaypb.BoardASICStreamLayout{StackCount: 1, AsicsPerStack: 1, TotalAsics: 1}
		}
		return f
	}
	got := make(chan sdk.BoardASICFrame, 4)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- miner.StreamBoardASICReadings(t.Context(), streamBoard, func(f sdk.BoardASICFrame) error {
			got <- f
			<-release
			return nil
		})
	}()
	cmd := <-node.Outgoing
	publishBoardFrame(t, node, cmd.GetCommandId(), frame(1, false))
	<-got // the viewer is now busy with frame 1
	publishBoardFrame(t, node, cmd.GetCommandId(), frame(2, true))
	publishBoardFrame(t, node, cmd.GetCommandId(), frame(3, false))
	close(release)
	latest := <-got
	require.Equal(t, int32(3), latest.ASICs[0].Index, "the viewer gets the newest frame")
	require.NotNil(t, latest.Layout, "the skipped frame's layout is carried onto it")
	node.PublishAck(&gatewaypb.ControlAck{CommandId: cmd.GetCommandId(), Code: gatewaypb.AckCode_ACK_CODE_OK, Succeeded: true, Payload: streamResult(t, gatewaypb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CONTROLLER_ENDED)})
	require.NoError(t, <-done)
}

func TestBoardASICStreamThroughFleetNodeEndsOnAnInvalidFrame(t *testing.T) {
	tooMany := &gatewaypb.BoardASICStreamFrame{}
	for i := range int32(4097) {
		tooMany.Asics = append(tooMany.Asics, &gatewaypb.BoardASICStreamReading{Index: i})
	}
	valid, err := proto.Marshal(tooMany)
	require.NoError(t, err)
	for name, payload := range map[string][]byte{"not a frame": {0xff, 0xff}, "too many readings": valid} {
		t.Run(name, func(t *testing.T) {
			miner, node := newRegistryMiner(t, control.CommandStreamProtocolVersion)
			go func() {
				cmd := <-node.Outgoing
				node.PublishCommandFrame(&gatewaypb.CommandFrame{CommandId: cmd.GetCommandId(), Payload: payload})
			}()
			sent := false
			err := miner.StreamBoardASICReadings(t.Context(), streamBoard, func(sdk.BoardASICFrame) error { sent = true; return nil })
			require.Equal(t, codes.Unavailable, status.Code(err), "got %v", err)
			require.False(t, sent, "an invalid frame never reaches the viewer")
			require.NotEmpty(t, <-node.Cancels, "the node is asked to stop")
		})
	}
}

func TestBoardASICStreamThroughFleetNodeReportsNodeCapacity(t *testing.T) {
	miner, node := newRegistryMiner(t, control.CommandStreamProtocolVersion)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for range control.MaxConcurrentCommandStreamsPerNode {
		go func() {
			_ = miner.StreamBoardASICReadings(ctx, streamBoard, func(sdk.BoardASICFrame) error { return nil })
		}()
		<-node.Outgoing
	}
	err := miner.StreamBoardASICReadings(t.Context(), streamBoard, func(sdk.BoardASICFrame) error { return nil })
	require.Equal(t, codes.ResourceExhausted, status.Code(err), "got %v", err)
}
