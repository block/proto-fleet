package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	fleetnodecontrol "github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
)

// boardStreamAcker records the frames and acks a node sends, and can fail
// sends to stand in for a lost control stream.
type boardStreamAcker struct {
	mu      sync.Mutex
	frames  []*pb.BoardASICStreamFrame
	ids     []string
	acks    []*pb.ControlAck
	sendErr error
}

func (a *boardStreamAcker) Send(req *pb.ControlStreamRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if f := req.GetCommandFrame(); f != nil {
		if a.sendErr != nil {
			return a.sendErr
		}
		var frame pb.BoardASICStreamFrame
		if err := proto.Unmarshal(f.GetPayload(), &frame); err != nil {
			return err //nolint:wrapcheck // test double
		}
		a.frames = append(a.frames, &frame)
		a.ids = append(a.ids, f.GetCommandId())
	}
	if ack := req.GetAck(); ack != nil {
		a.acks = append(a.acks, ack)
	}
	return nil
}

func (a *boardStreamAcker) only(t *testing.T) (*pb.ControlAck, pb.BoardASICStreamEnd) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	require.Len(t, a.acks, 1, "expected exactly one ack")
	var result pb.StreamBoardASICReadingsResult
	require.NoError(t, proto.Unmarshal(a.acks[0].GetPayload(), &result))
	return a.acks[0], result.GetEnd()
}

// boardStreamDevice sends frames, then returns err or waits for cancellation.
type boardStreamDevice struct {
	sdk.Device
	frames  []sdk.BoardASICFrame
	err     error
	block   bool
	started chan struct{}
	board   sdk.BoardIdentifyTarget
}

func (d *boardStreamDevice) StreamBoardASICReadings(ctx context.Context, board sdk.BoardIdentifyTarget, send func(sdk.BoardASICFrame) error) error {
	d.board = board
	for _, f := range d.frames {
		if err := send(f); err != nil {
			return err
		}
	}
	if d.started != nil {
		close(d.started)
	}
	if d.block {
		<-ctx.Done()
		return grpcstatus.FromContextError(ctx.Err()).Err() //nolint:wrapcheck // stands in for the plugin's gRPC status
	}
	return d.err
}

var boardStreamAction = &pb.StreamBoardASICReadingsAction{Tank: 1, Module: 2, Slot: 3, ModuleReportedId: "mod-2", MacAddress: "aa:bb:cc:dd:ee:ff"}

func streamCommand() *pb.MinerCommand {
	return withTarget(&pb.MinerCommand{Action: &pb.MinerCommand_StreamBoardAsicReadings{StreamBoardAsicReadings: boardStreamAction}})
}

func boardStreamRun(t *testing.T, device sdk.Device) *RunCmd {
	t.Helper()
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)
	base := mocks.NewMockDevice(ctrl)
	base.EXPECT().Close(gomock.Any()).Return(nil).AnyTimes()
	if d, ok := device.(*boardStreamDevice); ok {
		d.Device = base
	} else {
		device = base
	}
	driver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(sdk.NewDeviceResult{Device: device}, nil).AnyTimes()
	return &RunCmd{driverGetter: fakeDriverGetter{d: driver}, minerSecrets: nodeSecretProvider{}}
}

func TestBoardASICStreamNodeRelaysFramesBoundToTheCommand(t *testing.T) {
	observed := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	device := &boardStreamDevice{frames: []sdk.BoardASICFrame{
		{MACAddress: "aa:bb:cc:dd:ee:ff", ObservedAt: observed, Layout: &sdk.BoardASICLayout{StackCount: 1, ASICsPerStack: 1, TotalASICs: 1, Positions: []sdk.BoardASICPosition{{Index: 0, StackID: 1}}}, ASICs: []sdk.BoardASICReading{{Index: 0, TemperatureC: 60}}},
		{MACAddress: "aa:bb:cc:dd:ee:ff", ASICs: []sdk.BoardASICReading{{Index: 0, TemperatureC: 61}}},
	}}
	run := boardStreamRun(t, device)
	capture := &boardStreamAcker{}

	run.handleMinerCommand(t.Context(), nil, capture, "stream-1", streamCommand(), slog.Default())

	require.Equal(t, sdk.BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, ModuleReportedID: "mod-2", MACAddress: "aa:bb:cc:dd:ee:ff"}, device.board)
	require.Len(t, capture.frames, 2)
	require.Equal(t, []string{"stream-1", "stream-1"}, capture.ids)
	require.Equal(t, observed, capture.frames[0].GetObservedAt().AsTime())
	require.Equal(t, int32(1), capture.frames[0].GetLayout().GetPositions()[0].GetStackId())
	require.Nil(t, capture.frames[1].GetLayout())
	require.Nil(t, capture.frames[1].GetObservedAt())
	ack, end := capture.only(t)
	require.True(t, ack.GetSucceeded(), ack.GetErrorMessage())
	require.Equal(t, pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CONTROLLER_ENDED, end)
	require.Zero(t, run.boardASICStreams.running, "a finished stream frees its slot")
}

func TestBoardASICStreamNodeReportsHowTheStreamEnded(t *testing.T) {
	for name, tc := range map[string]struct {
		device  sdk.Device
		sendErr error
		code    pb.AckCode
		end     pb.BoardASICStreamEnd
	}{
		"board moved":            {device: &boardStreamDevice{err: grpcstatus.Error(codes.FailedPrecondition, "slot 3 holds another board")}, code: pb.AckCode_ACK_CODE_INTERNAL, end: pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_BOARD_CHANGED},
		"controller unsupported": {device: &boardStreamDevice{err: grpcstatus.Error(codes.Unimplemented, "no stream")}, code: pb.AckCode_ACK_CODE_UNIMPLEMENTED, end: pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNSUPPORTED},
		"driver unsupported":     {device: nil, code: pb.AckCode_ACK_CODE_UNIMPLEMENTED, end: pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNSUPPORTED},
		"controller failed":      {device: &boardStreamDevice{err: errors.New("dial 10.0.0.5: refused")}, code: pb.AckCode_ACK_CODE_INTERNAL, end: pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNAVAILABLE},
		"control stream lost":    {device: &boardStreamDevice{frames: []sdk.BoardASICFrame{{}}}, sendErr: errors.New("stream closed"), code: pb.AckCode_ACK_CODE_INTERNAL, end: pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNAVAILABLE},
	} {
		t.Run(name, func(t *testing.T) {
			run := boardStreamRun(t, tc.device)
			capture := &boardStreamAcker{sendErr: tc.sendErr}

			run.handleMinerCommand(t.Context(), nil, capture, "stream-1", streamCommand(), slog.Default())

			ack, end := capture.only(t)
			require.Equal(t, tc.code, ack.GetCode())
			require.Equal(t, tc.end, end)
			require.NotContains(t, ack.GetErrorMessage(), "10.0.0.5", "controller error text stays in the node log")
		})
	}
}

func TestBoardASICStreamNodeStopsOnCancelAndBoundsConcurrentStreams(t *testing.T) {
	device := &boardStreamDevice{block: true, started: make(chan struct{})}
	run := boardStreamRun(t, device)
	capture := &boardStreamAcker{}
	// The control loop runs each stream under a context a cancel ends.
	ctx, forget := run.controlCancels.track(t.Context(), "stream-1", true)
	defer forget()
	done := make(chan struct{})
	go func() {
		defer close(done)
		run.handleMinerCommand(ctx, nil, capture, "stream-1", streamCommand(), slog.Default())
	}()
	<-device.started

	run.controlCancels.cancel("unknown")
	run.controlCancels.cancel("stream-1")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not stop on cancel")
	}
	ack, end := capture.only(t)
	require.True(t, ack.GetSucceeded())
	require.Equal(t, pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CANCELED, end)

	for range fleetnodecontrol.MaxConcurrentCommandStreamsPerNode {
		_, ok := run.boardASICStreams.start()
		require.True(t, ok)
	}
	refused := &boardStreamAcker{}
	run.handleMinerCommand(t.Context(), nil, refused, "extra", streamCommand(), slog.Default())
	ack, end = refused.only(t)
	require.Equal(t, pb.AckCode_ACK_CODE_BUSY, ack.GetCode(), "the node relays at most the server's per-node cap")
	require.Equal(t, pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_AT_CAPACITY, end)
}

// The node lifetime ends a stream the server never cancelled, such as after a
// lost ControlCancel, and frees its slot.
func TestBoardASICStreamNodeLifetimeEndsAnUncancelledStream(t *testing.T) {
	prev := boardASICStreamNodeLifetime
	boardASICStreamNodeLifetime = 20 * time.Millisecond
	t.Cleanup(func() { boardASICStreamNodeLifetime = prev })
	run := boardStreamRun(t, &boardStreamDevice{block: true})
	capture := &boardStreamAcker{}

	run.handleMinerCommand(t.Context(), nil, capture, "stream-1", streamCommand(), slog.Default())

	ack, end := capture.only(t)
	require.True(t, ack.GetSucceeded(), ack.GetErrorMessage())
	require.Equal(t, pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CANCELED, end)
	require.Zero(t, run.boardASICStreams.running, "a stream ended by its lifetime frees its slot")
}

// A stream that ends because its control session dropped is a cancel, not a
// failure, even though the frame send failed too.
func TestBoardASICStreamEndTreatsACancelledContextAsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	sendErr := errors.New("stream closed")
	require.Equal(t, pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_UNAVAILABLE, boardASICStreamEnd(ctx, sendErr, sendErr), "a send error on a live session is a failure")
	cancel()
	require.Equal(t, pb.BoardASICStreamEnd_BOARD_ASIC_STREAM_END_CANCELED, boardASICStreamEnd(ctx, sendErr, sendErr))
}
