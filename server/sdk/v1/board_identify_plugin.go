package sdk

import (
	"context"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *DriverGRPCServer) SetBoardIdentify(ctx context.Context, req *pb.SetBoardIdentifyRequest) (*emptypb.Empty, error) {
	if req == nil || req.Ref == nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError("missing device", codes.InvalidArgument, "missing device")))
	}
	if req.Board == nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError("missing board", codes.InvalidArgument, "missing board")))
	}
	payload := BoardIdentifyRequest{
		Board: BoardIdentifyTarget{Tank: req.Board.Tank, Module: req.Board.Module, Slot: req.Board.Slot, ModuleReportedID: req.Board.ModuleReportedId, MACAddress: req.Board.MacAddress},
		Mode:  BoardIdentifyMode(req.Mode),
	}
	if err := payload.Validate(); err != nil {
		return nil, sdkErrorToGRPCStatus(InvalidWriteRequest(grpcStatusError(err.Error(), codes.InvalidArgument, err.Error())))
	}
	device, err := s.selectDevice(req.Ref)
	if err != nil {
		return nil, err
	}
	controller, ok := device.(DeviceBoardIdentifier)
	if !ok {
		return nil, sdkErrorToGRPCStatus(UnsupportedWrite(CapabilityBoardIdentify))
	}
	return &emptypb.Empty{}, sdkErrorToGRPCStatus(controller.SetBoardIdentify(ctx, payload))
}

func (d *DeviceGRPCClient) SetBoardIdentify(ctx context.Context, req BoardIdentifyRequest) error {
	if err := req.Validate(); err != nil {
		return InvalidWriteRequest(err)
	}
	b := req.Board
	_, err := d.client.SetBoardIdentify(ctx, &pb.SetBoardIdentifyRequest{
		Ref:   d.deviceRef(),
		Board: &pb.BoardIdentifyTarget{Tank: b.Tank, Module: b.Module, Slot: b.Slot, ModuleReportedId: b.ModuleReportedID, MacAddress: b.MACAddress},
		Mode:  string(req.Mode),
	})
	return err
}
