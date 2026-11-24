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

type moduleDevice struct {
	Device
	received ModuleMiningRequest
	writes   int
}

func (d *moduleDevice) SetModuleMining(_ context.Context, r ModuleMiningRequest) error {
	d.received = r
	d.writes++
	return nil
}

func TestModuleMiningRPCRoundTripAndValidation(t *testing.T) {
	d := &moduleDevice{}
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
	req := ModuleMiningRequest{Mining: new(false), Modules: []ModuleMiningTarget{{Tank: 1, Module: 2, ReportedID: "a"}, {Tank: 2, Module: 2, ReportedID: "b"}}}
	require.NoError(t, client.SetModuleMining(t.Context(), req))
	require.Equal(t, req, d.received)
	client.backendGeneration = []byte("replaced-backend")
	require.True(t, IsBackendGenerationRejected(client.SetModuleMining(t.Context(), req)))
	require.Equal(t, 1, d.writes, "stale backend references must not reach the controller")
	client.deviceID = "rig"
	client.backendGeneration = nil
	require.Equal(t, codes.Unimplemented, status.Code(client.SetModuleMining(t.Context(), req)))
	for _, invalid := range []ModuleMiningRequest{
		{Mining: new(false)},
		{Modules: req.Modules},
		{Mining: new(false), Modules: []ModuleMiningTarget{{Tank: 1, Module: 2}}},
		{Mining: new(false), Modules: []ModuleMiningTarget{req.Modules[0], req.Modules[0]}},
	} {
		require.Error(t, invalid.Validate())
	}
	wire := &pb.SetModuleMiningRequest{Ref: &pb.DeviceRef{DeviceId: "ct1"}, Mining: new(false), Modules: []*pb.ModuleMiningTarget{nil}}
	_, err = pb.NewDriverClient(conn).SetModuleMining(t.Context(), wire)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
