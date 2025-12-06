package fleetmanagement

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
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

func (h *Handler) ListMinerStateSnapshots(ctx context.Context, r *connect.Request[pb.ListMinerStateSnapshotsRequest]) (*connect.Response[pb.ListMinerStateSnapshotsResponse], error) {
	result, err := h.fleetMgmtSvc.ListMinerStateSnapshots(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) StreamMinerUpdates(ctx context.Context, r *connect.Request[pb.StreamMinerUpdatesRequest], stream *connect.ServerStream[pb.StreamMinerUpdatesResponse]) error {
	responseChan, err := h.fleetMgmtSvc.StreamMinerUpdates(ctx, r.Msg)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("stream context cancelled: %w", ctx.Err())
		case resp, ok := <-responseChan:
			if !ok {
				// Channel closed, stream complete
				return nil
			}
			if err := stream.Send(resp); err != nil {
				return fleeterror.NewInternalErrorf("failed to send miner update: %v", err)
			}
		}
	}
}

func (h *Handler) StreamMinerListUpdates(ctx context.Context, r *connect.Request[pb.StreamMinerListUpdatesRequest], stream *connect.ServerStream[pb.StreamMinerListUpdatesResponse]) error {
	responseChan, err := h.fleetMgmtSvc.StreamMinerListUpdates(ctx, r.Msg)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("stream context cancelled: %w", ctx.Err())
		case resp, ok := <-responseChan:
			if !ok {
				// Channel closed, stream complete
				return nil
			}
			if err := stream.Send(resp); err != nil {
				return fleeterror.NewInternalErrorf("failed to send miner list update: %v", err)
			}
		}
	}
}

func (h *Handler) GetMinerStateCounts(ctx context.Context, r *connect.Request[pb.GetMinerStateCountsRequest]) (*connect.Response[pb.GetMinerStateCountsResponse], error) {
	result, err := h.fleetMgmtSvc.GetMinerStateCounts(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}
