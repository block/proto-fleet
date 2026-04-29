package command

import (
	"context"
	"log/slog"
	"math"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/block/proto-fleet/server/internal/domain/command"
)

// Handler handles the Connect-RPC endpoints
type Handler struct {
	commandSvc *command.Service
}

var _ minercommandv1connect.MinerCommandServiceHandler = &Handler{}

func NewHandler(commandSvc *command.Service) *Handler {
	return &Handler{
		commandSvc: commandSvc,
	}
}

func (h *Handler) Reboot(
	ctx context.Context,
	req *connect.Request[pb.RebootRequest],
) (*connect.Response[pb.RebootResponse], error) {
	result, err := h.commandSvc.Reboot(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RebootResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

func (h *Handler) StopMining(
	ctx context.Context,
	req *connect.Request[pb.StopMiningRequest],
) (*connect.Response[pb.StopMiningResponse], error) {
	result, err := h.commandSvc.StopMining(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.StopMiningResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

func (h *Handler) StartMining(
	ctx context.Context,
	req *connect.Request[pb.StartMiningRequest],
) (*connect.Response[pb.StartMiningResponse], error) {
	result, err := h.commandSvc.StartMining(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.StartMiningResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

func (h *Handler) SetCoolingMode(
	ctx context.Context,
	req *connect.Request[pb.SetCoolingModeRequest],
) (*connect.Response[pb.SetCoolingModeResponse], error) {
	result, err := h.commandSvc.SetCoolingMode(ctx, req.Msg.DeviceSelector, req.Msg.Mode)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetCoolingModeResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

func (h *Handler) SetPowerTarget(
	ctx context.Context,
	req *connect.Request[pb.SetPowerTargetRequest],
) (*connect.Response[pb.SetPowerTargetResponse], error) {
	result, err := h.commandSvc.SetPowerTarget(ctx, req.Msg.DeviceSelector, req.Msg.PerformanceMode)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetPowerTargetResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

func (h *Handler) UpdateMiningPools(
	ctx context.Context,
	req *connect.Request[pb.UpdateMiningPoolsRequest],
) (*connect.Response[pb.UpdateMiningPoolsResponse], error) {
	result, err := h.commandSvc.UpdateMiningPools(
		ctx,
		req.Msg.DeviceSelector,
		req.Msg.DefaultPool,
		req.Msg.Backup_1Pool,
		req.Msg.Backup_2Pool,
		req.Msg.UserUsername,
		req.Msg.UserPassword,
	)
	if err != nil {
		return nil, err
	}
	resp := &pb.UpdateMiningPoolsResponse{BatchIdentifier: result.BatchIdentifier}
	if result.SV2Skips != nil {
		resp.Skips = &pb.PoolAssignmentSkips{
			SkippedCount:      clampToInt32(result.SV2Skips.SkippedCount),
			SelectedCount:     clampToInt32(result.SV2Skips.SelectedCount),
			IncompatibleTypes: result.SV2Skips.IncompatibleTypes,
		}
	}
	return connect.NewResponse(resp), nil
}

// clampToInt32 saturates an int to int32. Device counts in real fleets
// are well below MaxInt32; the clamp is purely to satisfy the gosec
// overflow check.
func clampToInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < math.MinInt32 {
		return math.MinInt32
	}
	return int32(n)
}

func (h *Handler) DownloadLogs(
	ctx context.Context,
	req *connect.Request[pb.DownloadLogsRequest],
) (*connect.Response[pb.DownloadLogsResponse], error) {
	result, err := h.commandSvc.DownloadLogs(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DownloadLogsResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

func (h *Handler) BlinkLED(ctx context.Context, req *connect.Request[pb.BlinkLEDRequest]) (*connect.Response[pb.BlinkLEDResponse], error) {
	result, err := h.commandSvc.BlinkLED(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BlinkLEDResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

func (h *Handler) FirmwareUpdate(ctx context.Context, req *connect.Request[pb.FirmwareUpdateRequest]) (*connect.Response[pb.FirmwareUpdateResponse], error) {
	result, err := h.commandSvc.FirmwareUpdate(ctx, req.Msg.DeviceSelector, req.Msg.GetFirmwareFileId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.FirmwareUpdateResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

func (h *Handler) Unpair(ctx context.Context, req *connect.Request[pb.UnpairRequest]) (*connect.Response[pb.UnpairResponse], error) {
	result, err := h.commandSvc.Unpair(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UnpairResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

func (h *Handler) UpdateMinerPassword(
	ctx context.Context,
	req *connect.Request[pb.UpdateMinerPasswordRequest],
) (*connect.Response[pb.UpdateMinerPasswordResponse], error) {
	result, err := h.commandSvc.UpdateMinerPassword(
		ctx,
		req.Msg.DeviceSelector,
		req.Msg.NewPassword,
		req.Msg.CurrentPassword,
		req.Msg.UserUsername,
		req.Msg.UserPassword,
	)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateMinerPasswordResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

func (h *Handler) StreamCommandBatchUpdates(ctx context.Context, r *connect.Request[pb.StreamCommandBatchUpdatesRequest], stream *connect.ServerStream[pb.StreamCommandBatchUpdatesResponse]) error {
	slog.Debug("handling request to stream command batch updates", "request", r)
	responseChan, err := h.commandSvc.StreamCommandBatchUpdates(ctx, r.Msg)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			slog.Debug("context closed")
			return fleeterror.NewInternalErrorf("context done with error: %v", ctx.Err())
		case resp, ok := <-responseChan:
			if !ok {
				slog.Warn("channel closed")
				return nil
			}
			slog.Debug("sending update", "payload", resp)
			if err := stream.Send(resp); err != nil {
				return fleeterror.NewInternalErrorf("error sending response to stream: %v", err)
			}
		}
	}
}

func (h *Handler) GetCommandBatchLogBundle(
	_ context.Context,
	req *connect.Request[pb.GetCommandBatchLogBundleRequest],
) (*connect.Response[pb.GetCommandBatchLogBundleResponse], error) {
	resp, err := h.commandSvc.GetCommandBatchLogBundle(req.Msg.BatchIdentifier)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) CheckCommandCapabilities(
	ctx context.Context,
	req *connect.Request[pb.CheckCommandCapabilitiesRequest],
) (*connect.Response[pb.CheckCommandCapabilitiesResponse], error) {
	resp, err := h.commandSvc.CheckCommandCapabilities(ctx, req.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

// GetCommandBatchDeviceResults returns the per-device outcome for a command
// batch so the activity log drill-down can show which miners succeeded or
// failed along with any per-miner error messages. Thin pass-through into the
// command service; authorization and response shaping live there.
func (h *Handler) GetCommandBatchDeviceResults(
	ctx context.Context,
	req *connect.Request[pb.GetCommandBatchDeviceResultsRequest],
) (*connect.Response[pb.GetCommandBatchDeviceResultsResponse], error) {
	resp, err := h.commandSvc.GetCommandBatchDeviceResults(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}
