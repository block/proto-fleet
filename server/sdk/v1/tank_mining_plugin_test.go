package sdk

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type tankDevice struct {
	Device
	received TankMiningRequest
}

func (d *tankDevice) SetTankMining(_ context.Context, req TankMiningRequest) error {
	d.received = req
	return nil
}
func TestTankMiningRPCPreservesScopeAndExplicitStop(t *testing.T) {
	device := &tankDevice{}
	server := &DriverGRPCServer{devices: map[string]Device{"ct1": device, "rig": fakeDevice{}}}
	req := &pb.SetTankMiningRequest{Ref: &pb.DeviceRef{DeviceId: "ct1"}, Positions: []int32{1, 3}, Mining: new(false)}
	_, err := server.SetTankMining(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int32{1, 3}, device.received.Positions)
	require.False(t, *device.received.Mining)
	req.Positions = nil
	_, err = server.SetTankMining(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	req.Positions = []int32{1}
	req.Mining = nil
	_, err = server.SetTankMining(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	req.Mining = new(true)
	req.Ref.DeviceId = "rig"
	_, err = server.SetTankMining(t.Context(), req)
	require.Equal(t, codes.Unimplemented, status.Code(err))
}
