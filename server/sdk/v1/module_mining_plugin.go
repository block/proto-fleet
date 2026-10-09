package sdk

import (
	"context"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *DriverGRPCServer) SetModuleMining(ctx context.Context, req *pb.SetModuleMiningRequest) (*emptypb.Empty, error) {
	if req == nil || req.Ref == nil {
		return nil, grpcStatusError("missing device", codes.InvalidArgument, "missing device")
	}
	payload := ModuleMiningRequest{Mining: req.Mining}
	for _, m := range req.Modules {
		if m == nil {
			return nil, grpcStatusError("missing module", codes.InvalidArgument, "missing module")
		}
		payload.Modules = append(payload.Modules, ModuleMiningTarget{Tank: m.Tank, Module: m.Module, ReportedID: m.ReportedId})
	}
	if err := payload.Validate(); err != nil {
		return nil, grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())
	}
	s.mu.RLock()
	device, ok := s.devices[req.Ref.DeviceId]
	s.mu.RUnlock()
	if !ok {
		return nil, sdkErrorToGRPCStatus(NewErrorDeviceNotFound(req.Ref.DeviceId))
	}
	controller, ok := device.(DeviceModuleMiner)
	if !ok {
		return nil, sdkErrorToGRPCStatus(NewErrUnsupportedCapability(CapabilityModuleMining))
	}
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(controller.SetModuleMining(ctx, payload))
}
func (d *DeviceGRPCClient) SetModuleMining(ctx context.Context, req ModuleMiningRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	wire := &pb.SetModuleMiningRequest{Ref: &pb.DeviceRef{DeviceId: d.deviceID}, Mining: req.Mining}
	for _, m := range req.Modules {
		wire.Modules = append(wire.Modules, &pb.ModuleMiningTarget{Tank: m.Tank, Module: m.Module, ReportedId: m.ReportedID})
	}
	_, err := d.client.SetModuleMining(ctx, wire)
	return err
}
