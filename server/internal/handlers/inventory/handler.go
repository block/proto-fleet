// Package inventory is the Connect-RPC surface for InventoryService.
package inventory

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/inventory/v1"
	"github.com/block/proto-fleet/server/generated/grpc/inventory/v1/inventoryv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/inventory"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the InventoryService Connect-RPC surface.
type Handler struct {
	service *inventory.Service
}

var _ inventoryv1connect.InventoryServiceHandler = &Handler{}

// NewHandler returns an InventoryService handler bound to the supplied
// domain service.
func NewHandler(service *inventory.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListParts(ctx context.Context, req *connect.Request[pb.ListPartsRequest]) (*connect.Response[pb.ListPartsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	rows, err := h.service.ListParts(ctx, toListFilter(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListPartsResponse(rows)), nil
}

func (h *Handler) GetPart(ctx context.Context, req *connect.Request[pb.GetPartRequest]) (*connect.Response[pb.GetPartResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	part, err := h.service.GetPart(ctx, info.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetPartResponse{
		Part: toProtoPart(part),
	}), nil
}

func (h *Handler) GetInsights(ctx context.Context, req *connect.Request[pb.GetInsightsRequest]) (*connect.Response[pb.GetInsightsResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
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
	info, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	rows, err := h.service.ListPartsBySite(ctx, info.OrganizationID, req.Msg.GetSiteId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toListPartsBySiteResponse(rows)), nil
}

func (h *Handler) CreatePart(ctx context.Context, req *connect.Request[pb.CreatePartRequest]) (*connect.Response[pb.CreatePartResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	part, err := h.service.CreatePart(ctx, toCreateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreatePartResponse{
		Part: toProtoPart(part),
	}), nil
}

func (h *Handler) UpdatePart(ctx context.Context, req *connect.Request[pb.UpdatePartRequest]) (*connect.Response[pb.UpdatePartResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	part, err := h.service.UpdatePart(ctx, toUpdateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdatePartResponse{
		Part: toProtoPart(part),
	}), nil
}

func (h *Handler) DeletePart(ctx context.Context, req *connect.Request[pb.DeletePartRequest]) (*connect.Response[pb.DeletePartResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if err := h.service.DeletePart(ctx, info.OrganizationID, req.Msg.GetId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeletePartResponse{}), nil
}

func (h *Handler) ImportCsvPreview(ctx context.Context, req *connect.Request[pb.ImportCsvPreviewRequest]) (*connect.Response[pb.ImportCsvPreviewResponse], error) {
	_, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	rows, err := h.service.ParseCsvPreview(ctx, req.Msg.GetCsvData())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(toImportCsvPreviewResponse(rows)), nil
}

func (h *Handler) ConfirmCsvImport(ctx context.Context, req *connect.Request[pb.ConfirmCsvImportRequest]) (*connect.Response[pb.ConfirmCsvImportResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	previewRows := fromProtoPreviewRows(req.Msg.GetRows())
	created, err := h.service.ConfirmCsvImport(ctx, info.OrganizationID, previewRows)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ConfirmCsvImportResponse{
		CreatedCount: created,
	}), nil
}
