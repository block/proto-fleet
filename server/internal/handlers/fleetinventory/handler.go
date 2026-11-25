package fleetinventory

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/containers"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct{ service *fleetinventory.Service }

func NewHandler(service *fleetinventory.Service) *Handler { return &Handler{service: service} }

func (h *Handler) ListTankOptions(ctx context.Context, req *connect.Request[pb.ListTankOptionsRequest]) (*connect.Response[pb.ListTankOptionsResponse], error) {
	orgID, scopes, err := readScopes(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.service.TankOptions(ctx, orgID, req.Msg, scopes)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) GetModuleHashboards(ctx context.Context, req *connect.Request[pb.GetModuleHashboardsRequest]) (*connect.Response[pb.GetModuleHashboardsResponse], error) {
	orgID, scopes, err := readScopes(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.service.Hashboards(ctx, orgID, req.Msg, scopes)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) ListHashboardHistory(ctx context.Context, req *connect.Request[pb.ListHashboardHistoryRequest]) (*connect.Response[pb.ListHashboardHistoryResponse], error) {
	orgID, scopes, err := readScopes(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.service.HashboardHistory(ctx, orgID, req.Msg, scopes)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func readScopes(ctx context.Context) (int64, []fleetinventory.Scope, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return 0, nil, err
	}
	scopes := make([]fleetinventory.Scope, 0, 3)
	for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead} {
		orgWide, sites, err := middleware.SiteScopeForPermission(ctx, permission)
		if err != nil {
			return 0, nil, err
		}
		scopes = append(scopes, fleetinventory.Scope{OrgWide: orgWide, Sites: sites, TelemetryOnly: permission != authz.PermSiteRead})
	}
	return info.OrganizationID, scopes, nil
}

func (h *Handler) ListInventory(ctx context.Context, req *connect.Request[pb.ListInventoryRequest]) (*connect.Response[pb.ListInventoryResponse], error) {
	orgID, scopes, err := readScopes(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.View == pb.InventoryView_INVENTORY_VIEW_EQUIPMENT_GROUPS {
		orgWide, sites, scopeErr := middleware.SiteScopeForPermission(ctx, authz.PermRackRead)
		if scopeErr != nil {
			return nil, scopeErr
		}
		scopes = append(scopes, fleetinventory.Scope{OrgWide: orgWide, Sites: sites, RackOnly: true})
	}
	result, err := h.service.List(ctx, orgID, req.Msg, scopes)
	if err != nil {
		return nil, err
	}
	for _, item := range result.Items {
		if container := item.GetContainer(); container != nil {
			container.CanAssignSite = containers.CanAssignSite(ctx, container)
		}
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) ListModuleHistory(ctx context.Context, req *connect.Request[pb.ListModuleHistoryRequest]) (*connect.Response[pb.ListModuleHistoryResponse], error) {
	orgID, scopes, err := readScopes(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.service.History(ctx, orgID, req.Msg, scopes)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (h *Handler) GetTankHistory(ctx context.Context, req *connect.Request[pb.GetTankHistoryRequest]) (*connect.Response[pb.GetTankHistoryResponse], error) {
	orgID, scopes, err := readScopes(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.service.TankHistory(ctx, orgID, req.Msg, scopes)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
