// Package inventory is the Connect-RPC surface for InventoryService.
package inventory

import (
	"context"
	"encoding/base64"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/inventory/v1"
	"github.com/block/proto-fleet/server/generated/grpc/inventory/v1/inventoryv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/inventory"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the InventoryService Connect-RPC surface.
type Handler struct {
	service *inventory.Service
}

var _ inventoryv1connect.InventoryServiceHandler = &Handler{}

// Keep this aligned with the csv_data max_len on both inventory import RPCs.
const maxCSVImportBytes = 10 * 1024 * 1024

// RequestReadLimitOption allows base64 expansion for JSON clients plus framing
// overhead. Protobuf validation still limits the decoded CSV to 10 MiB.
func RequestReadLimitOption() connect.HandlerOption {
	return connect.WithReadMaxBytes(base64.StdEncoding.EncodedLen(maxCSVImportBytes) + 1024)
}

// NewHandler returns an InventoryService handler bound to the supplied
// domain service.
func NewHandler(service *inventory.Service) *Handler {
	return &Handler{service: service}
}

func bindMaintenanceScope(ctx context.Context, permission string) (context.Context, error) {
	return middleware.BindAuthorizedSiteScope(ctx, permission)
}

func intersectSiteIDs(requested, allowed []int64) []int64 {
	if len(requested) == 0 {
		return allowed
	}
	allowedSet := make(map[int64]bool, len(allowed))
	for _, siteID := range allowed {
		allowedSet[siteID] = true
	}
	intersection := make([]int64, 0, len(requested))
	for _, siteID := range requested {
		if allowedSet[siteID] {
			intersection = append(intersection, siteID)
		}
	}
	return intersection
}

func (h *Handler) ListInventoryParts(ctx context.Context, req *connect.Request[pb.ListInventoryPartsRequest]) (*connect.Response[pb.ListInventoryPartsResponse], error) {
	info, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermMaintenanceRead)
	if err != nil {
		return nil, err
	}
	filter, err := toListFilter(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	orgWide, scopedSites, err := middleware.SiteScopeForPermission(ctx, authz.PermMaintenanceRead)
	if err != nil {
		return nil, err
	}
	if orgWide {
		filter.ExcludedSiteIDs = scopedSites
	} else {
		filter.SiteIDs = intersectSiteIDs(filter.SiteIDs, scopedSites)
		if len(filter.SiteIDs) == 0 {
			return connect.NewResponse(&pb.ListInventoryPartsResponse{}), nil
		}
	}
	page, err := h.service.ListParts(ctx, filter)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListPartsResponse(page)), nil
}

func (h *Handler) GetInventoryPart(ctx context.Context, req *connect.Request[pb.GetInventoryPartRequest]) (*connect.Response[pb.GetInventoryPartResponse], error) {
	info, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermMaintenanceRead)
	if err != nil {
		return nil, err
	}
	ctx, err = bindMaintenanceScope(ctx, authz.PermMaintenanceRead)
	if err != nil {
		return nil, err
	}
	part, err := h.service.GetPart(ctx, info.OrganizationID, req.Msg.GetId())
	if err != nil {
		if fleeterror.IsForbiddenError(err) {
			return nil, fleeterror.NewNotFoundErrorf("inventory part %d not found", req.Msg.GetId())
		}
		return nil, err
	}
	return connect.NewResponse(&pb.GetInventoryPartResponse{
		Part: toProtoPart(part),
	}), nil
}

func (h *Handler) GetInventoryInsights(ctx context.Context, _ *connect.Request[pb.GetInventoryInsightsRequest]) (*connect.Response[pb.GetInventoryInsightsResponse], error) {
	info, err := middleware.RequireOrgWidePermission(ctx, authz.PermMaintenanceRead)
	if err != nil {
		return nil, err
	}
	insights, err := h.service.GetInsights(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toGetInsightsResponse(insights)), nil
}

func (h *Handler) ListPartsBySite(ctx context.Context, req *connect.Request[pb.ListPartsBySiteRequest]) (*connect.Response[pb.ListPartsBySiteResponse], error) {
	siteID := req.Msg.GetSiteId()
	info, err := middleware.RequirePermission(ctx, authz.PermMaintenanceRead, authz.ResourceContext{SiteID: &siteID})
	if err != nil {
		return nil, err
	}
	rows, err := h.service.ListPartsBySite(ctx, info.OrganizationID, siteID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListPartsBySiteResponse(rows)), nil
}

func (h *Handler) CreateInventoryPart(ctx context.Context, req *connect.Request[pb.CreateInventoryPartRequest]) (*connect.Response[pb.CreateInventoryPartResponse], error) {
	info, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	ctx, err = bindMaintenanceScope(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	part, err := h.service.CreatePart(ctx, toCreateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateInventoryPartResponse{
		Part: toProtoPart(part),
	}), nil
}

func (h *Handler) UpdateInventoryPart(ctx context.Context, req *connect.Request[pb.UpdateInventoryPartRequest]) (*connect.Response[pb.UpdateInventoryPartResponse], error) {
	info, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	ctx, err = bindMaintenanceScope(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	params, err := toUpdateParams(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	part, err := h.service.UpdatePart(ctx, params)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateInventoryPartResponse{
		Part: toProtoPart(part),
	}), nil
}

func (h *Handler) DeleteInventoryPart(ctx context.Context, req *connect.Request[pb.DeleteInventoryPartRequest]) (*connect.Response[pb.DeleteInventoryPartResponse], error) {
	info, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	ctx, err = bindMaintenanceScope(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	params, err := toDeleteParams(req.Msg, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	if err := h.service.DeletePart(ctx, params); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteInventoryPartResponse{}), nil
}

func (h *Handler) ImportInventoryCsv(ctx context.Context, req *connect.Request[pb.ImportInventoryCsvRequest]) (*connect.Response[pb.ImportInventoryCsvResponse], error) {
	info, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	ctx, err = bindMaintenanceScope(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	rows, err := h.service.ParseCsvPreview(ctx, info.OrganizationID, req.Msg.GetCsvData())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toImportCsvPreviewResponse(rows)), nil
}

func (h *Handler) ConfirmInventoryImport(ctx context.Context, req *connect.Request[pb.ConfirmInventoryImportRequest]) (*connect.Response[pb.ConfirmInventoryImportResponse], error) {
	info, err := middleware.RequirePermissionAtAnySite(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	ctx, err = bindMaintenanceScope(ctx, authz.PermMaintenanceManage)
	if err != nil {
		return nil, err
	}
	created, err := h.service.ConfirmCsvImport(ctx, info.OrganizationID, req.Msg.GetCsvData())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ConfirmInventoryImportResponse{
		ImportedCount: created,
	}), nil
}
