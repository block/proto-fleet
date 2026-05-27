package fleetmanagement

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleetmanagement"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
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

// requirePerm is a local thin wrapper so each handler reads as
// "gate then service call" without three lines of boilerplate.
// ResourceContext is always empty here: per-miner site narrowing is
// deferred until DeviceSelector resolution lands.
func requirePerm(ctx context.Context, key string) error {
	_, err := middleware.RequirePermission(ctx, key, authz.ResourceContext{})
	return err
}

func (h *Handler) ListMinerStateSnapshots(ctx context.Context, r *connect.Request[pb.ListMinerStateSnapshotsRequest]) (*connect.Response[pb.ListMinerStateSnapshotsResponse], error) {
	if err := requirePerm(ctx, authz.PermMinerRead); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.ListMinerStateSnapshots(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) ExportMinerListCsv(ctx context.Context, r *connect.Request[pb.ExportMinerListCsvRequest], stream *connect.ServerStream[pb.ExportMinerListCsvResponse]) error {
	if err := requirePerm(ctx, authz.PermMinerExportCSV); err != nil {
		return err
	}
	return h.fleetMgmtSvc.ExportMinerListCsv(ctx, r.Msg, func(chunk *pb.ExportMinerListCsvResponse) error {
		return stream.Send(chunk)
	})
}

func (h *Handler) GetMinerStateCounts(ctx context.Context, r *connect.Request[pb.GetMinerStateCountsRequest]) (*connect.Response[pb.GetMinerStateCountsResponse], error) {
	if err := requirePerm(ctx, authz.PermFleetRead); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.GetMinerStateCounts(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerPoolAssignments(ctx context.Context, r *connect.Request[pb.GetMinerPoolAssignmentsRequest]) (*connect.Response[pb.GetMinerPoolAssignmentsResponse], error) {
	if err := requirePerm(ctx, authz.PermMinerRead); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.GetMinerPoolAssignments(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerCoolingMode(ctx context.Context, r *connect.Request[pb.GetMinerCoolingModeRequest]) (*connect.Response[pb.GetMinerCoolingModeResponse], error) {
	if err := requirePerm(ctx, authz.PermMinerRead); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.GetMinerCoolingMode(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) DeleteMiners(ctx context.Context, r *connect.Request[pb.DeleteMinersRequest]) (*connect.Response[pb.DeleteMinersResponse], error) {
	if err := requirePerm(ctx, authz.PermMinerDelete); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.DeleteMiners(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerModelGroups(ctx context.Context, r *connect.Request[pb.GetMinerModelGroupsRequest]) (*connect.Response[pb.GetMinerModelGroupsResponse], error) {
	if err := requirePerm(ctx, authz.PermFleetRead); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.GetMinerModelGroups(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) RenameMiners(ctx context.Context, r *connect.Request[pb.RenameMinersRequest]) (*connect.Response[pb.RenameMinersResponse], error) {
	if err := requirePerm(ctx, authz.PermMinerRename); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.RenameMiners(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) UpdateWorkerNames(ctx context.Context, r *connect.Request[pb.UpdateWorkerNamesRequest]) (*connect.Response[pb.UpdateWorkerNamesResponse], error) {
	if err := requirePerm(ctx, authz.PermMinerUpdateWorkerName); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.UpdateWorkerNames(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}
