package fleetmanagement

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleetmanagement"
)

// Handler handles the Connect-RPC endpoints
type Handler struct {
	fleetMgmtSvc *fleetmanagement.Service
}

var _ fleetmanagementv1connect.FleetManagementServiceHandler = &Handler{}

func NewHandler(fleetMgmtSvc *fleetmanagement.Service) *Handler {
	return &Handler{
		fleetMgmtSvc: fleetMgmtSvc,
	}
}

func (h *Handler) SetDefaultPool(ctx context.Context, r *connect.Request[pb.SetDefaultPoolRequest]) (*connect.Response[pb.SetDefaultPoolResponse], error) {
	pool, err := h.fleetMgmtSvc.UpdateDefaultPool(ctx, r.Msg.PoolId)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(
		&pb.SetDefaultPoolResponse{
			Pool: pool,
		}), nil
}

// ListPairedMiners implements fleetmanagementv1connect.FleetManagementServiceHandler.
func (h *Handler) ListPairedMiners(ctx context.Context, r *connect.Request[pb.ListPairedMinersRequest]) (*connect.Response[pb.ListPairedMinersResponse], error) {
	result, err := h.fleetMgmtSvc.ListPairedMiners(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) ListPools(ctx context.Context, _ *connect.Request[pb.ListPoolsRequest]) (*connect.Response[pb.ListPoolsResponse], error) {
	pools, err := h.fleetMgmtSvc.ListPools(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.ListPoolsResponse{Pools: pools}), nil
}

func (h *Handler) CreatePool(ctx context.Context, r *connect.Request[pb.CreatePoolRequest]) (*connect.Response[pb.CreatePoolResponse], error) {
	pool, err := h.fleetMgmtSvc.CreatePool(ctx, r.Msg.PoolConfig)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.CreatePoolResponse{Pool: pool}), nil
}

func (h *Handler) UpdatePool(ctx context.Context, r *connect.Request[pb.UpdatePoolRequest]) (*connect.Response[pb.UpdatePoolResponse], error) {
	pool, err := h.fleetMgmtSvc.UpdatePool(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.UpdatePoolResponse{Pool: pool}), nil
}

func (h *Handler) UpdatePoolPriority(ctx context.Context, r *connect.Request[pb.UpdatePoolPriorityRequest]) (*connect.Response[pb.UpdatePoolPriorityResponse], error) {
	pools, err := h.fleetMgmtSvc.UpdatePoolPriority(ctx, r.Msg.PoolPriorities)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.UpdatePoolPriorityResponse{Pools: pools}), nil
}

func (h *Handler) DeletePool(ctx context.Context, r *connect.Request[pb.DeletePoolRequest]) (*connect.Response[pb.DeletePoolResponse], error) {
	err := h.fleetMgmtSvc.DeletePool(ctx, r.Msg.PoolId)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.DeletePoolResponse{}), nil
}

func (h *Handler) ListMinerStateSnapshots(ctx context.Context, r *connect.Request[pb.ListMinerStateSnapshotsRequest]) (*connect.Response[pb.ListMinerStateSnapshotsResponse], error) {
	result, err := h.fleetMgmtSvc.ListMinerStateSnapshots(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) StreamMinerUpdates(ctx context.Context, r *connect.Request[pb.StreamMinerUpdatesRequest], stream *connect.ServerStream[pb.StreamMinerUpdatesResponse]) error {
	slog.Debug("handling request to stream miner updates", "request", r)
	responseChan, err := h.fleetMgmtSvc.StreamMinerUpdates(ctx, r.Msg)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			slog.Debug("context closed")
			// nolint:wrapcheck
			return err
		case resp, ok := <-responseChan:
			if !ok {
				slog.Debug("channel closed")
				// Channel closed, stream complete
				return nil
			}
			slog.Debug("sending update", "payload", resp)
			if err := stream.Send(resp); err != nil {
				// nolint:wrapcheck
				return err
			}
		}
	}
}
