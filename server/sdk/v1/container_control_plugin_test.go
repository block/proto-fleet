package sdk

import (
	"context"
	"testing"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type controlDevice struct {
	Device
	received ContainerControlRequest
}

func (d *controlDevice) SetContainerControl(_ context.Context, r ContainerControlRequest) error {
	d.received = r
	return nil
}
func TestEquipmentRPCValidatesAndPreservesDesiredOff(t *testing.T) {
	device := &controlDevice{}
	server := &DriverGRPCServer{devices: map[string]Device{"ct1": device, "rig": fakeDevice{}}}
	req := &pb.SetContainerControlRequest{Ref: &pb.DeviceRef{DeviceId: "ct1"}, Name: "dry_cooler", Enabled: new(false)}
	_, err := server.SetContainerControl(t.Context(), req)
	require.NoError(t, err)
	require.False(t, *device.received.Enabled)
	req.Enabled = nil
	_, err = server.SetContainerControl(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	req.Enabled = new(true)
	req.Ref.DeviceId = "rig"
	_, err = server.SetContainerControl(t.Context(), req)
	require.Equal(t, codes.Unimplemented, status.Code(err))
}
