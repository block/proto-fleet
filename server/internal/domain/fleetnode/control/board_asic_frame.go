package control

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// BoardASICFrameToProto encodes a plugin's board ASIC frame as the payload a
// Fleet Node relays. BoardASICFrameFromProto reverses it. The node and fleetd
// share this pair so a reading field cannot reach one side and be dropped by
// the other.
func BoardASICFrameToProto(frame sdk.BoardASICFrame) *gatewaypb.BoardASICStreamFrame {
	out := &gatewaypb.BoardASICStreamFrame{MacAddress: frame.MACAddress, Asics: make([]*gatewaypb.BoardASICStreamReading, 0, len(frame.ASICs))}
	if !frame.ObservedAt.IsZero() {
		out.ObservedAt = timestamppb.New(frame.ObservedAt)
	}
	if layout := frame.Layout; layout != nil {
		out.Layout = &gatewaypb.BoardASICStreamLayout{StackCount: layout.StackCount, AsicsPerStack: layout.ASICsPerStack, TotalAsics: layout.TotalASICs, Positions: make([]*gatewaypb.BoardASICStreamPosition, 0, len(layout.Positions))}
		for _, p := range layout.Positions {
			out.Layout.Positions = append(out.Layout.Positions, &gatewaypb.BoardASICStreamPosition{Index: p.Index, StackId: p.StackID, StackIndex: p.StackIndex, X: p.X, Y: p.Y})
		}
	}
	for _, a := range frame.ASICs {
		out.Asics = append(out.Asics, &gatewaypb.BoardASICStreamReading{
			Index: a.Index, TemperatureC: a.TemperatureC, HashrateGhs: a.HashrateGHs, VoltageV: a.VoltageV,
			FrequencyMhz: a.FrequencyMHz, PassRatePercent: a.PassRatePercent, ThermalQuarantined: a.ThermalQuarantined,
		})
	}
	return out
}

// BoardASICFrameFromProto decodes a relayed frame payload into the plugin's
// board ASIC frame.
func BoardASICFrameFromProto(frame *gatewaypb.BoardASICStreamFrame) sdk.BoardASICFrame {
	out := sdk.BoardASICFrame{MACAddress: frame.GetMacAddress(), ASICs: make([]sdk.BoardASICReading, 0, len(frame.GetAsics()))}
	if frame.GetObservedAt() != nil {
		out.ObservedAt = frame.GetObservedAt().AsTime()
	}
	if layout := frame.GetLayout(); layout != nil {
		out.Layout = &sdk.BoardASICLayout{
			StackCount: layout.GetStackCount(), ASICsPerStack: layout.GetAsicsPerStack(), TotalASICs: layout.GetTotalAsics(),
			Positions: make([]sdk.BoardASICPosition, 0, len(layout.GetPositions())),
		}
		for _, p := range layout.GetPositions() {
			out.Layout.Positions = append(out.Layout.Positions, sdk.BoardASICPosition{Index: p.GetIndex(), StackID: p.GetStackId(), StackIndex: p.GetStackIndex(), X: p.GetX(), Y: p.GetY()})
		}
	}
	for _, a := range frame.GetAsics() {
		out.ASICs = append(out.ASICs, sdk.BoardASICReading{
			Index: a.GetIndex(), TemperatureC: a.GetTemperatureC(), HashrateGHs: a.GetHashrateGhs(), VoltageV: a.GetVoltageV(),
			FrequencyMHz: a.GetFrequencyMhz(), PassRatePercent: a.GetPassRatePercent(), ThermalQuarantined: a.ThermalQuarantined,
		})
	}
	return out
}
