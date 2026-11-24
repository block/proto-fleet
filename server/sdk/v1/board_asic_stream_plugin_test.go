package sdk

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type boardASICStreamDevice struct {
	Device
	frames  []BoardASICFrame
	err     error
	follow  bool
	opened  int
	stopped chan struct{}
	board   BoardIdentifyTarget
}

func (d *boardASICStreamDevice) StreamBoardASICReadings(ctx context.Context, board BoardIdentifyTarget, send func(BoardASICFrame) error) error {
	d.opened++
	d.board = board
	for _, frame := range d.frames {
		if err := send(frame); err != nil {
			return err
		}
	}
	if d.follow {
		<-ctx.Done()
		close(d.stopped)
		return ctx.Err() //nolint:wrapcheck // The fake device reports cancellation as-is.
	}
	return d.err
}

func newBoardASICStreamClient(t *testing.T, d *boardASICStreamDevice) *DeviceGRPCClient {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	generation := []byte("current-backend")
	pb.RegisterDriverServer(server, &DriverGRPCServer{devices: map[string]Device{"ct1": d, "rig": fakeDevice{}}, generations: map[string][]byte{"ct1": generation}})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	return &DeviceGRPCClient{client: pb.NewDriverClient(conn), deviceID: "ct1", backendGeneration: generation}
}

func TestBoardASICStreamRelaysFramesAndErrors(t *testing.T) {
	quarantined := true
	observed := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	layout := &BoardASICLayout{StackCount: 1, ASICsPerStack: 2, TotalASICs: 2, Positions: []BoardASICPosition{{Index: 0}, {Index: 1, StackIndex: 1, X: 1}}}
	frames := []BoardASICFrame{
		{MACAddress: "aa:bb:cc:dd:ee:01", ObservedAt: observed, Layout: layout, ASICs: []BoardASICReading{{Index: 0, TemperatureC: 71.5, HashrateGHs: 12.5, VoltageV: 0.32, FrequencyMHz: 500, PassRatePercent: 99.5, ThermalQuarantined: &quarantined}, {Index: 1}}},
		{MACAddress: "aa:bb:cc:dd:ee:01", ObservedAt: observed.Add(time.Second), ASICs: []BoardASICReading{{Index: 0, TemperatureC: 72}}},
	}
	d := &boardASICStreamDevice{frames: frames, err: status.Error(codes.NotFound, "board not found")}
	client := newBoardASICStreamClient(t, d)
	board := BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, ModuleReportedID: "module-a", MACAddress: "aa:bb:cc:dd:ee:01"}

	var got []BoardASICFrame
	err := client.StreamBoardASICReadings(t.Context(), board, func(f BoardASICFrame) error {
		got = append(got, f)
		return nil
	})
	require.Equal(t, codes.NotFound, status.Code(err), "the plugin's terminal error reaches the caller")
	require.Equal(t, frames, got)
	require.Equal(t, board, d.board)

	d.err = nil
	got = nil
	require.NoError(t, client.StreamBoardASICReadings(t.Context(), board, func(f BoardASICFrame) error {
		got = append(got, f)
		return nil
	}), "a controller that ends the stream is a clean end")
	require.Len(t, got, 2)

	stop := errors.New("view closed")
	require.ErrorIs(t, client.StreamBoardASICReadings(t.Context(), board, func(BoardASICFrame) error { return stop }), stop)
}

func TestBoardASICStreamStopsThePluginWhenTheCallerCancels(t *testing.T) {
	d := &boardASICStreamDevice{frames: []BoardASICFrame{{MACAddress: "aa:bb:cc:dd:ee:01"}}, follow: true, stopped: make(chan struct{})}
	client := newBoardASICStreamClient(t, d)
	ctx, cancel := context.WithCancel(t.Context())
	board := BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, ModuleReportedID: "module-a", MACAddress: "aa:bb:cc:dd:ee:01"}
	done := make(chan error, 1)
	go func() {
		done <- client.StreamBoardASICReadings(ctx, board, func(BoardASICFrame) error {
			cancel()
			return nil
		})
	}()
	select {
	case <-d.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("plugin stream kept running after the caller cancelled")
	}
	require.Equal(t, codes.Canceled, status.Code(<-done))
}

func TestBoardASICStreamRejectsStaleUnsupportedAndInvalidRequests(t *testing.T) {
	d := &boardASICStreamDevice{}
	client := newBoardASICStreamClient(t, d)
	board := BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, ModuleReportedID: "module-a", MACAddress: "aa:bb:cc:dd:ee:01"}
	ignore := func(BoardASICFrame) error { return nil }

	client.backendGeneration = []byte("replaced-backend")
	require.True(t, IsBackendGenerationRejected(client.StreamBoardASICReadings(t.Context(), board, ignore)))
	client.deviceID = "rig"
	client.backendGeneration = nil
	require.Equal(t, codes.Unimplemented, status.Code(client.StreamBoardASICReadings(t.Context(), board, ignore)))
	require.Equal(t, codes.InvalidArgument, status.Code(client.StreamBoardASICReadings(t.Context(), BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 4, ModuleReportedID: "m", MACAddress: "aa"}, ignore)))

	raw := client.client
	ref := &pb.DeviceRef{DeviceId: "ct1", BackendGeneration: []byte("current-backend")}
	for _, req := range []*pb.StreamBoardASICReadingsRequest{
		{Ref: ref},
		{Board: &pb.BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, ModuleReportedId: "module-a", MacAddress: "aa:bb:cc:dd:ee:01"}},
		{Ref: ref, Board: &pb.BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, MacAddress: "aa:bb:cc:dd:ee:01"}},
	} {
		stream, err := raw.StreamBoardASICReadings(t.Context(), req)
		require.NoError(t, err)
		_, err = stream.Recv()
		require.Equal(t, codes.InvalidArgument, status.Code(err), "%v", req)
	}
	require.Zero(t, d.opened, "invalid, stale and unsupported requests never reach the controller")
}
