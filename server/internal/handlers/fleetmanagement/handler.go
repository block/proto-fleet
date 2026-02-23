package fleetmanagement

import (
	"context"

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

func (h *Handler) ListMinerStateSnapshots(ctx context.Context, r *connect.Request[pb.ListMinerStateSnapshotsRequest]) (*connect.Response[pb.ListMinerStateSnapshotsResponse], error) {
	result, err := h.fleetMgmtSvc.ListMinerStateSnapshots(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerStateCounts(ctx context.Context, r *connect.Request[pb.GetMinerStateCountsRequest]) (*connect.Response[pb.GetMinerStateCountsResponse], error) {
	result, err := h.fleetMgmtSvc.GetMinerStateCounts(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerPoolAssignments(ctx context.Context, r *connect.Request[pb.GetMinerPoolAssignmentsRequest]) (*connect.Response[pb.GetMinerPoolAssignmentsResponse], error) {
	result, err := h.fleetMgmtSvc.GetMinerPoolAssignments(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerCoolingMode(ctx context.Context, r *connect.Request[pb.GetMinerCoolingModeRequest]) (*connect.Response[pb.GetMinerCoolingModeResponse], error) {
	result, err := h.fleetMgmtSvc.GetMinerCoolingMode(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) DeleteMiners(ctx context.Context, r *connect.Request[pb.DeleteMinersRequest]) (*connect.Response[pb.DeleteMinersResponse], error) {
	result, err := h.fleetMgmtSvc.DeleteMiners(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerModelGroups(ctx context.Context, r *connect.Request[pb.GetMinerModelGroupsRequest]) (*connect.Response[pb.GetMinerModelGroupsResponse], error) {
	result, err := h.fleetMgmtSvc.GetMinerModelGroups(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}
