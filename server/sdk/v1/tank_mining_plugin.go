package sdk

import (
	"context"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *DriverGRPCServer) SetTankMining(ctx context.Context, req *pb.SetTankMiningRequest) (*emptypb.Empty, error) {
	if req == nil || req.Ref == nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError("missing device", codes.InvalidArgument, "missing device")))
	}
	payload := TankMiningRequest{Positions: req.Positions, Mining: req.Mining}
	if err := payload.Validate(); err != nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())))
	}
	device, err := s.selectDevice(req.Ref)
	if err != nil {
		return nil, err
	}
	controller, ok := device.(DeviceTankMiner)
	if !ok {
		return nil, sdkErrorToGRPCStatus(UnsupportedWrite(CapabilityTankMining))
	}
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(controller.SetTankMining(ctx, payload))
}
func (d *DeviceGRPCClient) SetTankMining(ctx context.Context, req TankMiningRequest) error {
	if err := req.Validate(); err != nil {
		return InvalidWriteRequest(err)
	}
	_, err := d.client.SetTankMining(ctx, &pb.SetTankMiningRequest{Ref: d.deviceRef(), Positions: req.Positions, Mining: req.Mining})
	return err
}
