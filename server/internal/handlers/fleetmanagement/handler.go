package fleetmanagement

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type fleetManagementService interface {
	ListMinerStateSnapshots(context.Context, *pb.ListMinerStateSnapshotsRequest) (*pb.ListMinerStateSnapshotsResponse, error)
	RefreshMinerResourceContexts(context.Context, *pb.RefreshMinersRequest) (map[string]authz.ResourceContext, error)
	RefreshMiners(context.Context, *pb.RefreshMinersRequest) (*pb.RefreshMinersResponse, error)
	ExportMinerListCsv(context.Context, *pb.ExportMinerListCsvRequest, func(*pb.ExportMinerListCsvResponse) error) error
	GetMinerStateCounts(context.Context, *pb.GetMinerStateCountsRequest) (*pb.GetMinerStateCountsResponse, error)
	GetMinerPoolAssignments(context.Context, *pb.GetMinerPoolAssignmentsRequest) (*pb.GetMinerPoolAssignmentsResponse, error)
	GetMinerCoolingMode(context.Context, *pb.GetMinerCoolingModeRequest) (*pb.GetMinerCoolingModeResponse, error)
	DeleteMiners(context.Context, *pb.DeleteMinersRequest) (*pb.DeleteMinersResponse, error)
	GetMinerModelGroups(context.Context, *pb.GetMinerModelGroupsRequest) (*pb.GetMinerModelGroupsResponse, error)
	RenameMiners(context.Context, *pb.RenameMinersRequest) (*pb.RenameMinersResponse, error)
	UpdateWorkerNames(context.Context, *pb.UpdateWorkerNamesRequest) (*pb.UpdateWorkerNamesResponse, error)
}

// Handler handles the Connect-RPC endpoints
type Handler struct {
	fleetMgmtSvc fleetManagementService
}

var _ fleetmanagementv1connect.FleetManagementServiceHandler = &Handler{}

func NewHandler(fleetMgmtSvc fleetManagementService) *Handler {
	return &Handler{
		fleetMgmtSvc: fleetMgmtSvc,
	}
}

func (h *Handler) ListMinerStateSnapshots(ctx context.Context, r *connect.Request[pb.ListMinerStateSnapshotsRequest]) (*connect.Response[pb.ListMinerStateSnapshotsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.ListMinerStateSnapshots(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) RefreshMiners(ctx context.Context, r *connect.Request[pb.RefreshMinersRequest]) (*connect.Response[pb.RefreshMinersResponse], error) {
	resourceContexts, err := h.fleetMgmtSvc.RefreshMinerResourceContexts(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	if len(resourceContexts) == 0 {
		if _, err := middleware.RequirePermission(ctx, authz.PermMinerRead, authz.ResourceContext{}); err != nil {
			return nil, err
		}
	}
	for _, resourceContext := range resourceContexts {
		if _, err := middleware.RequirePermission(ctx, authz.PermMinerRead, resourceContext); err != nil {
			return nil, err
		}
	}

	result, err := h.fleetMgmtSvc.RefreshMiners(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) ExportMinerListCsv(ctx context.Context, r *connect.Request[pb.ExportMinerListCsvRequest], stream *connect.ServerStream[pb.ExportMinerListCsvResponse]) error {
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerExportCSV, authz.ResourceContext{}); err != nil {
		return err
	}
	return h.fleetMgmtSvc.ExportMinerListCsv(ctx, r.Msg, func(chunk *pb.ExportMinerListCsvResponse) error {
		return stream.Send(chunk)
	})
}

func (h *Handler) GetMinerStateCounts(ctx context.Context, r *connect.Request[pb.GetMinerStateCountsRequest]) (*connect.Response[pb.GetMinerStateCountsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.GetMinerStateCounts(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerPoolAssignments(ctx context.Context, r *connect.Request[pb.GetMinerPoolAssignmentsRequest]) (*connect.Response[pb.GetMinerPoolAssignmentsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.GetMinerPoolAssignments(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerCoolingMode(ctx context.Context, r *connect.Request[pb.GetMinerCoolingModeRequest]) (*connect.Response[pb.GetMinerCoolingModeResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.GetMinerCoolingMode(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) DeleteMiners(ctx context.Context, r *connect.Request[pb.DeleteMinersRequest]) (*connect.Response[pb.DeleteMinersResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerDelete, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.DeleteMiners(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) GetMinerModelGroups(ctx context.Context, r *connect.Request[pb.GetMinerModelGroupsRequest]) (*connect.Response[pb.GetMinerModelGroupsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.GetMinerModelGroups(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) RenameMiners(ctx context.Context, r *connect.Request[pb.RenameMinersRequest]) (*connect.Response[pb.RenameMinersResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerRename, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.RenameMiners(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}

func (h *Handler) UpdateWorkerNames(ctx context.Context, r *connect.Request[pb.UpdateWorkerNamesRequest]) (*connect.Response[pb.UpdateWorkerNamesResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerUpdateWorkerName, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	result, err := h.fleetMgmtSvc.UpdateWorkerNames(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(result), nil
}
