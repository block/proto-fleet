package command

import (
	"context"
	"log/slog"

	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/minercommand/v1/minercommandv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/command"
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
	resp, err := h.commandSvc.Reboot(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) StopMining(
	ctx context.Context,
	req *connect.Request[pb.StopMiningRequest],
) (*connect.Response[pb.StopMiningResponse], error) {
	resp, err := h.commandSvc.StopMining(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) StartMining(
	ctx context.Context,
	req *connect.Request[pb.StartMiningRequest],
) (*connect.Response[pb.StartMiningResponse], error) {
	resp, err := h.commandSvc.StartMining(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) SetCoolingMode(
	ctx context.Context,
	req *connect.Request[pb.SetCoolingModeRequest],
) (*connect.Response[pb.SetCoolingModeResponse], error) {
	resp, err := h.commandSvc.SetCoolingMode(ctx, req.Msg.DeviceSelector, req.Msg.Mode)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) UpdateMiningPools(
	ctx context.Context,
	req *connect.Request[pb.UpdateMiningPoolsRequest],
) (*connect.Response[pb.UpdateMiningPoolsResponse], error) {
	var backup1PoolID, backup2PoolID *int64

	if req.Msg.Backup_1PoolId != nil {
		value := *req.Msg.Backup_1PoolId
		backup1PoolID = &value
	}

	if req.Msg.Backup_2PoolId != nil {
		value := *req.Msg.Backup_2PoolId
		backup2PoolID = &value
	}

	resp, err := h.commandSvc.UpdateMiningPools(ctx, req.Msg.DeviceSelector, req.Msg.DefaultPoolId, backup1PoolID, backup2PoolID)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) DownloadLogs(
	ctx context.Context,
	req *connect.Request[pb.DownloadLogsRequest],
) (*connect.Response[pb.DownloadLogsResponse], error) {
	resp, err := h.commandSvc.DownloadLogs(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) BlinkLED(ctx context.Context, req *connect.Request[pb.BlinkLEDRequest]) (*connect.Response[pb.BlinkLEDResponse], error) {
	resp, err := h.commandSvc.BlinkLED(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (h *Handler) FirmwareUpdate(ctx context.Context, req *connect.Request[pb.FirmwareUpdateRequest]) (*connect.Response[pb.FirmwareUpdateResponse], error) {
	resp, err := h.commandSvc.FirmwareUpdate(ctx, req.Msg.DeviceSelector)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
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
