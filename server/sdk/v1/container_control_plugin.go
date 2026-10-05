package sdk

import (
	"context"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *DriverGRPCServer) SetContainerControl(ctx context.Context, req *pb.SetContainerControlRequest) (*emptypb.Empty, error) {
	if req == nil || req.Ref == nil {
		return nil, grpcStatusError("missing device", codes.InvalidArgument, "missing device")
	}
	payload := ContainerControlRequest{Name: req.Name, Enabled: req.Enabled}
	if err := payload.Validate(); err != nil {
		return nil, grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())
	}
	s.mu.RLock()
	device, ok := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()
	if !ok {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}
	controller, ok := device.(DeviceContainerController)
	if !ok {
		return nil, sdkErrorToGRPCStatus(NewErrUnsupportedCapability(CapabilityContainerControl))
	}
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(controller.SetContainerControl(ctx, payload))
}
func (d *DeviceGRPCClient) SetContainerControl(ctx context.Context, req ContainerControlRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	_, err := d.client.SetContainerControl(ctx, &pb.SetContainerControlRequest{Ref: &pb.DeviceRef{DeviceId: d.deviceID}, Name: req.Name, Enabled: req.Enabled})
	return err
}
