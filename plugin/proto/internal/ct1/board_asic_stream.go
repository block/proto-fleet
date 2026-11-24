package ct1

import (
	"context"
	"errors"
	"io"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

// boardStreamResolveTimeout bounds controller selection and the topology
// check. The stream itself has no deadline; the caller's context ends it.
const boardStreamResolveTimeout = 10 * time.Second

// maxBoardASICs bounds one frame's readings and layout entries. A CT1 board
// has a few hundred ASICs; a frame beyond this is a controller fault.
const maxBoardASICs = 4096

// StreamBoardASICReadings follows Frigate's ASIC health stream for one board.
// The controller map must still place the confirmed board at the confirmed
// slot, as for board identify, before any reading is relayed. A frame that
// names a different board, is larger than a board, or repeats an ASIC index
// ends the stream.
func (d *Device) StreamBoardASICReadings(ctx context.Context, board sdk.BoardIdentifyTarget, send func(sdk.BoardASICFrame) error) error {
	if err := board.Validate(); err != nil {
		return status.Error(codes.InvalidArgument, err.Error()) //nolint:wrapcheck // The gRPC status is the plugin boundary error.
	}
	c, identifier, err := d.resolveStreamBoard(ctx, board)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := c.telemetry.StreamASICHealth(ctx, &mdk.StreamASICHealthRequest{Identifier: identifier})
	if err != nil {
		return err //nolint:wrapcheck // callers inspect the controller's gRPC status
	}
	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err //nolint:wrapcheck // callers inspect the controller's gRPC status
		}
		if normalizedBoardMAC(frame.GetMacAddress()) != normalizedBoardMAC(identifier) {
			return status.Errorf(codes.DataLoss, "controller streamed readings for %q instead of %q", frame.GetMacAddress(), identifier)
		}
		if err := validateBoardASICFrame(frame); err != nil {
			return err
		}
		if err := send(boardASICFrame(frame)); err != nil {
			return err
		}
	}
}

func (d *Device) resolveStreamBoard(ctx context.Context, board sdk.BoardIdentifyTarget) (*Client, string, error) {
	ctx, cancel := context.WithTimeout(ctx, boardStreamResolveTimeout)
	defer cancel()
	c, err := d.selectClient(ctx, false)
	if err != nil {
		return nil, "", err
	}
	mapWire, err := c.boards.GetContainerMap(ctx, &mdk.GetContainerMapRequest{})
	if err != nil {
		return nil, "", err //nolint:wrapcheck // callers inspect the controller's gRPC status
	}
	identifier, err := identifyBoardSelector(mapResponse(mapWire), board)
	if err != nil {
		return nil, "", status.Error(codes.FailedPrecondition, err.Error()) //nolint:wrapcheck // The gRPC status is the plugin boundary error.
	}
	return c, identifier, nil
}

// validateBoardASICFrame rejects a frame no board could produce, so a faulty
// controller cannot push an oversized or self-contradicting sample to viewers.
func validateBoardASICFrame(frame *mdk.StreamASICHealthResponse) error {
	entries := frame.GetLayout().GetEntries()
	if len(frame.GetAsics()) > maxBoardASICs || len(entries) > maxBoardASICs {
		return status.Errorf(codes.DataLoss, "controller streamed %d readings and %d layout entries; a board has at most %d ASICs", len(frame.GetAsics()), len(entries), maxBoardASICs)
	}
	seen := make(map[int32]struct{}, len(frame.GetAsics()))
	for _, asic := range frame.GetAsics() {
		if _, dup := seen[asic.GetIndex()]; dup {
			return status.Errorf(codes.DataLoss, "controller streamed ASIC %d more than once", asic.GetIndex())
		}
		seen[asic.GetIndex()] = struct{}{}
	}
	clear(seen)
	for _, entry := range entries {
		if _, dup := seen[entry.GetIndex()]; dup {
			return status.Errorf(codes.DataLoss, "controller placed ASIC %d more than once", entry.GetIndex())
		}
		seen[entry.GetIndex()] = struct{}{}
	}
	return nil
}

func boardASICFrame(frame *mdk.StreamASICHealthResponse) sdk.BoardASICFrame {
	out := sdk.BoardASICFrame{MACAddress: frame.GetMacAddress(), ASICs: make([]sdk.BoardASICReading, 0, len(frame.GetAsics()))}
	if frame.Timestamp != nil {
		out.ObservedAt = frame.Timestamp.AsTime()
	}
	if layout := frame.GetLayout(); layout != nil {
		out.Layout = &sdk.BoardASICLayout{StackCount: layout.GetStackCount(), ASICsPerStack: layout.GetAsicsPerStack(), TotalASICs: layout.GetTotalAsics(), Positions: make([]sdk.BoardASICPosition, 0, len(layout.GetEntries()))}
		for _, entry := range layout.GetEntries() {
			out.Layout.Positions = append(out.Layout.Positions, sdk.BoardASICPosition{Index: entry.GetIndex(), StackID: entry.GetStackId(), StackIndex: entry.GetStackIndex(), X: entry.GetX(), Y: entry.GetY()})
		}
	}
	// Debug health and eFuse metadata are engineering data and stay on the
	// controller.
	for _, asic := range frame.GetAsics() {
		out.ASICs = append(out.ASICs, sdk.BoardASICReading{
			Index:              asic.GetIndex(),
			TemperatureC:       float64(asic.GetTemperatureDc()) / 10,
			HashrateGHs:        float64(asic.GetHashrateGhs10()) / 10,
			VoltageV:           float64(asic.GetVoltageMv()) / 1000,
			FrequencyMHz:       float64(asic.GetFrequencyMhz()),
			PassRatePercent:    float64(asic.GetPassRateBp()) / 100,
			ThermalQuarantined: asic.ThermalQuarantined,
		})
	}
	return out
}
