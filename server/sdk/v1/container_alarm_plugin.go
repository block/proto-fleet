package sdk

import (
	"context"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *DriverGRPCServer) RunContainerAlarmAction(ctx context.Context, req *pb.RunContainerAlarmActionRequest) (*emptypb.Empty, error) {
	if req == nil || req.Ref == nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError("missing device", codes.InvalidArgument, "missing device")))
	}
	payload := ContainerAlarmRequest{Action: req.Action}
	if err := payload.Validate(); err != nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())))
	}
	device, err := s.selectDevice(req.Ref)
	if err != nil {
		return nil, err
	}
	controller, ok := device.(DeviceContainerAlarmController)
	if !ok {
		return nil, sdkErrorToGRPCStatus(UnsupportedWrite(CapabilityContainerAlarm))
	}
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(controller.RunContainerAlarmAction(ctx, payload))
}

func (d *DeviceGRPCClient) RunContainerAlarmAction(ctx context.Context, req ContainerAlarmRequest) error {
	if err := req.Validate(); err != nil {
		return InvalidWriteRequest(err)
	}
	_, err := d.client.RunContainerAlarmAction(ctx, &pb.RunContainerAlarmActionRequest{Ref: d.deviceRef(), Action: req.Action})
	return err
}
