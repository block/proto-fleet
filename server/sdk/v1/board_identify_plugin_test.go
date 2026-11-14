package sdk

import (
	"context"
	"net"
	"testing"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type boardIdentifyDevice struct {
	Device
	received BoardIdentifyRequest
	writes   int
}

func (d *boardIdentifyDevice) SetBoardIdentify(_ context.Context, r BoardIdentifyRequest) error {
	d.received = r
	d.writes++
	return nil
}

func TestBoardIdentifyRPCRoundTripAndValidation(t *testing.T) {
	d := &boardIdentifyDevice{}
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
	client := &DeviceGRPCClient{client: pb.NewDriverClient(conn), deviceID: "ct1", backendGeneration: generation}

	board := BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, ModuleReportedID: "module-a", MACAddress: "aa:bb:cc:dd:ee:01"}
	for _, mode := range []BoardIdentifyMode{BoardIdentifyBlink, BoardIdentifyOff} {
		req := BoardIdentifyRequest{Board: board, Mode: mode}
		require.NoError(t, client.SetBoardIdentify(t.Context(), req))
		require.Equal(t, req, d.received)
	}
	client.backendGeneration = []byte("replaced-backend")
	require.True(t, IsBackendGenerationRejected(client.SetBoardIdentify(t.Context(), BoardIdentifyRequest{Board: board, Mode: BoardIdentifyBlink})))
	require.Equal(t, 2, d.writes, "stale backend references must not reach the controller")
	client.deviceID = "rig"
	client.backendGeneration = nil
	require.Equal(t, codes.Unimplemented, status.Code(client.SetBoardIdentify(t.Context(), BoardIdentifyRequest{Board: board, Mode: BoardIdentifyBlink})))

	for _, invalid := range []BoardIdentifyRequest{
		{Board: board},
		{Board: board, Mode: "toggle"},
		{Board: BoardIdentifyTarget{Tank: 1, Module: 2, Slot: CT1HashboardsPerModule + 1, ModuleReportedID: "module-a", MACAddress: "aa:bb:cc:dd:ee:01"}, Mode: BoardIdentifyBlink},
		{Board: BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, MACAddress: "aa:bb:cc:dd:ee:01"}, Mode: BoardIdentifyBlink},
		{Board: BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, ModuleReportedID: "module-a"}, Mode: BoardIdentifyBlink},
	} {
		require.Error(t, invalid.Validate())
	}

	// The server validates the wire request itself; a nil board or unknown mode
	// is rejected before any device is selected.
	raw := pb.NewDriverClient(conn)
	ref := &pb.DeviceRef{DeviceId: "ct1", BackendGeneration: generation}
	wire := &pb.BoardIdentifyTarget{Tank: 1, Module: 2, Slot: 3, ModuleReportedId: "module-a", MacAddress: "aa:bb:cc:dd:ee:01"}
	for _, req := range []*pb.SetBoardIdentifyRequest{
		{Ref: ref, Mode: string(BoardIdentifyBlink)},
		{Ref: ref, Board: wire, Mode: "toggle"},
		{Board: wire, Mode: string(BoardIdentifyBlink)},
	} {
		_, err = raw.SetBoardIdentify(t.Context(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err), "%v", req)
	}
	require.Equal(t, 2, d.writes)
}
