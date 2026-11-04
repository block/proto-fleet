package sdk

import (
	"context"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *DriverGRPCServer) SetContainerControl(ctx context.Context, req *pb.SetContainerControlRequest) (*emptypb.Empty, error) {
	if req == nil || req.Ref == nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError("missing device", codes.InvalidArgument, "missing device")))
	}
	payload := ContainerControlRequest{Name: req.Name, Enabled: req.Enabled}
	if err := payload.Validate(); err != nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())))
	}
	device, err := s.selectDevice(req.Ref)
	if err != nil {
		return nil, err
	}
	controller, ok := device.(DeviceContainerController)
	if !ok {
		return nil, sdkErrorToGRPCStatus(UnsupportedWrite(CapabilityContainerControl))
	}
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(controller.SetContainerControl(ctx, payload))
}
func (d *DeviceGRPCClient) SetContainerControl(ctx context.Context, req ContainerControlRequest) error {
	if err := req.Validate(); err != nil {
		return InvalidWriteRequest(err)
	}
	_, err := d.client.SetContainerControl(ctx, &pb.SetContainerControlRequest{Ref: d.deviceRef(), Name: req.Name, Enabled: req.Enabled})
	return err
}
