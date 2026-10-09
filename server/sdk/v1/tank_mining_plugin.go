package sdk

import (
	"context"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *DriverGRPCServer) SetTankMining(ctx context.Context, req *pb.SetTankMiningRequest) (*emptypb.Empty, error) {
	if req == nil || req.Ref == nil {
		return nil, grpcStatusError("missing device", codes.InvalidArgument, "missing device")
	}
	payload := TankMiningRequest{Positions: req.Positions, Mining: req.Mining}
	if err := payload.Validate(); err != nil {
		return nil, grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())
	}
	s.mu.RLock()
	device, ok := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()
	if !ok {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}
	controller, ok := device.(DeviceTankMiner)
	if !ok {
		return nil, sdkErrorToGRPCStatus(NewErrUnsupportedCapability(CapabilityTankMining))
	}
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(controller.SetTankMining(ctx, payload))
}
func (d *DeviceGRPCClient) SetTankMining(ctx context.Context, req TankMiningRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	_, err := d.client.SetTankMining(ctx, &pb.SetTankMiningRequest{Ref: &pb.DeviceRef{DeviceId: d.deviceID}, Positions: req.Positions, Mining: req.Mining})
	return err
}
