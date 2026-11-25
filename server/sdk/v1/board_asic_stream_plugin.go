package sdk

import (
	"context"
	"errors"
	"io"

	pb "github.com/block/proto-fleet/server/sdk/v1/pb/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *DriverGRPCServer) StreamBoardASICReadings(req *pb.StreamBoardASICReadingsRequest, stream pb.Driver_StreamBoardASICReadingsServer) error {
	if req == nil || req.Ref == nil {
		return grpcStatusError("missing device", codes.InvalidArgument, "missing device")
	}
	if req.Board == nil {
		return grpcStatusError("missing board", codes.InvalidArgument, "missing board")
	}
	board := boardIdentifyTargetFromProto(req.Board)
	if err := board.Validate(); err != nil {
		return grpcStatusError("invalid board", codes.InvalidArgument, err.Error())
	}
	device, err := s.selectDevice(req.Ref)
	if err != nil {
		return err
	}
	streamer, ok := device.(DeviceBoardASICStreamer)
	if !ok {
		return sdkErrorToGRPCStatus(NewErrUnsupportedCapability(CapabilityBoardASICStream))
	}
	return sdkErrorToGRPCStatus(streamer.StreamBoardASICReadings(stream.Context(), board, func(frame BoardASICFrame) error {
		return stream.Send(boardASICFrameToProto(frame))
	}))
}

// StreamBoardASICReadings relays the plugin's frames to send. It returns nil when the
// plugin ends the stream, and the stream or send error otherwise.
func (d *DeviceGRPCClient) StreamBoardASICReadings(ctx context.Context, board BoardIdentifyTarget, send func(BoardASICFrame) error) error {
	if err := board.Validate(); err != nil {
		return grpcStatusError("invalid board", codes.InvalidArgument, err.Error())
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := d.client.StreamBoardASICReadings(ctx, &pb.StreamBoardASICReadingsRequest{Ref: d.deviceRef(), Board: boardIdentifyTargetToProto(board)})
	if err != nil {
		return err //nolint:wrapcheck // callers inspect the gRPC status
	}
	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err //nolint:wrapcheck // callers inspect the gRPC status
		}
		if err := send(boardASICFrameFromProto(frame)); err != nil {
			return err
		}
	}
}

func boardIdentifyTargetFromProto(b *pb.BoardIdentifyTarget) BoardIdentifyTarget {
	return BoardIdentifyTarget{Tank: b.GetTank(), Module: b.GetModule(), Slot: b.GetSlot(), ModuleReportedID: b.GetModuleReportedId(), MACAddress: b.GetMacAddress()}
}

func boardIdentifyTargetToProto(b BoardIdentifyTarget) *pb.BoardIdentifyTarget {
	return &pb.BoardIdentifyTarget{Tank: b.Tank, Module: b.Module, Slot: b.Slot, ModuleReportedId: b.ModuleReportedID, MacAddress: b.MACAddress}
}

func boardASICFrameToProto(f BoardASICFrame) *pb.BoardASICFrame {
	out := &pb.BoardASICFrame{MacAddress: f.MACAddress, Asics: make([]*pb.BoardASICReading, 0, len(f.ASICs))}
	if !f.ObservedAt.IsZero() {
		out.ObservedAt = timestamppb.New(f.ObservedAt)
	}
	if f.Layout != nil {
		out.Layout = &pb.BoardASICLayout{StackCount: f.Layout.StackCount, AsicsPerStack: f.Layout.ASICsPerStack, TotalAsics: f.Layout.TotalASICs, Positions: make([]*pb.BoardASICPosition, 0, len(f.Layout.Positions))}
		for _, p := range f.Layout.Positions {
			out.Layout.Positions = append(out.Layout.Positions, &pb.BoardASICPosition{Index: p.Index, StackId: p.StackID, StackIndex: p.StackIndex, X: p.X, Y: p.Y})
		}
	}
	for _, a := range f.ASICs {
		out.Asics = append(out.Asics, &pb.BoardASICReading{Index: a.Index, TemperatureC: a.TemperatureC, HashrateGhs: a.HashrateGHs, VoltageV: a.VoltageV, FrequencyMhz: a.FrequencyMHz, PassRatePercent: a.PassRatePercent, ThermalQuarantined: a.ThermalQuarantined})
	}
	return out
}

func boardASICFrameFromProto(f *pb.BoardASICFrame) BoardASICFrame {
	out := BoardASICFrame{MACAddress: f.GetMacAddress(), ASICs: make([]BoardASICReading, 0, len(f.GetAsics()))}
	if f.ObservedAt != nil {
		out.ObservedAt = f.ObservedAt.AsTime()
	}
	if l := f.GetLayout(); l != nil {
		out.Layout = &BoardASICLayout{StackCount: l.GetStackCount(), ASICsPerStack: l.GetAsicsPerStack(), TotalASICs: l.GetTotalAsics(), Positions: make([]BoardASICPosition, 0, len(l.GetPositions()))}
		for _, p := range l.GetPositions() {
			out.Layout.Positions = append(out.Layout.Positions, BoardASICPosition{Index: p.GetIndex(), StackID: p.GetStackId(), StackIndex: p.GetStackIndex(), X: p.GetX(), Y: p.GetY()})
		}
	}
	for _, a := range f.GetAsics() {
		out.ASICs = append(out.ASICs, BoardASICReading{Index: a.GetIndex(), TemperatureC: a.GetTemperatureC(), HashrateGHs: a.GetHashrateGhs(), VoltageV: a.GetVoltageV(), FrequencyMHz: a.GetFrequencyMhz(), PassRatePercent: a.GetPassRatePercent(), ThermalQuarantined: a.ThermalQuarantined})
	}
	return out
}
