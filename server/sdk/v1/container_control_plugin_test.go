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

type alarmDevice struct {
	Device
	received []ContainerAlarmRequest
	err      error
}

func (d *alarmDevice) RunContainerAlarmAction(_ context.Context, r ContainerAlarmRequest) error {
	d.received = append(d.received, r)
	return d.err
}

func TestContainerAlarmRPCValidatesAndDispatchesOnce(t *testing.T) {
	device := &alarmDevice{}
	server := &DriverGRPCServer{devices: map[string]Device{"ct1": device, "rig": fakeDevice{}}}
	ref := &pb.DeviceRef{DeviceId: "ct1"}
	for _, action := range []string{ContainerAlarmActionMute, ContainerAlarmActionReset} {
		_, err := server.RunContainerAlarmAction(t.Context(), &pb.RunContainerAlarmActionRequest{Ref: ref, Action: action})
		require.NoError(t, err)
	}
	require.Equal(t, []ContainerAlarmRequest{{Action: "mute"}, {Action: "reset"}}, device.received)

	_, err := server.RunContainerAlarmAction(t.Context(), &pb.RunContainerAlarmActionRequest{Ref: ref, Action: "silence"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = server.RunContainerAlarmAction(t.Context(), &pb.RunContainerAlarmActionRequest{Ref: ref})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = server.RunContainerAlarmAction(t.Context(), &pb.RunContainerAlarmActionRequest{Action: "mute"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Len(t, device.received, 2, "invalid requests must not reach the device")

	_, err = server.RunContainerAlarmAction(t.Context(), &pb.RunContainerAlarmActionRequest{Ref: &pb.DeviceRef{DeviceId: "rig"}, Action: "mute"})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestContainerAlarmClientRejectsInvalidActionBeforeDispatch(t *testing.T) {
	client := &DeviceGRPCClient{deviceID: "ct1"}
	require.Error(t, client.RunContainerAlarmAction(t.Context(), ContainerAlarmRequest{Action: "toggle"}))
}
