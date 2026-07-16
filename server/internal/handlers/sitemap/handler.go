package sitemap

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/sitemap/v1"
	"github.com/block/proto-fleet/server/generated/grpc/sitemap/v1/sitemapv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	sitemapdomain "github.com/block/proto-fleet/server/internal/domain/sitemap"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	service *sitemapdomain.Service
}

var _ sitemapv1connect.SiteMapServiceHandler = &Handler{}

func NewHandler(service *sitemapdomain.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ExportSiteMapCsv(ctx context.Context, _ *connect.Request[pb.ExportSiteMapCsvRequest], stream *connect.ServerStream[pb.ExportSiteMapCsvResponse]) error {
	info, err := middleware.RequireOrgWidePermission(ctx, authz.PermMinerExportCSV)
	if err != nil {
		return err
	}
	if _, err := middleware.RequireOrgWidePermission(ctx, authz.PermSiteRead); err != nil {
		return err
	}
	if _, err := middleware.RequireOrgWidePermission(ctx, authz.PermRackRead); err != nil {
		return err
	}
	return h.service.ExportSiteMapCsv(ctx, info.OrganizationID, func(chunk *pb.ExportSiteMapCsvResponse) error {
		return stream.Send(chunk)
	})
}

func (h *Handler) ImportSiteMapCsv(ctx context.Context, req *connect.Request[pb.ImportSiteMapCsvRequest]) (*connect.Response[pb.ImportSiteMapCsvResponse], error) {
	info, err := middleware.RequireOrgWidePermission(ctx, authz.PermSiteManage)
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequireOrgWidePermission(ctx, authz.PermRackManage); err != nil {
		return nil, err
	}
	canRenameMiners, err := middleware.HasPermission(ctx, authz.PermMinerRename, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	resp, err := h.service.ImportSiteMapCsv(ctx, info.OrganizationID, req.Msg, sitemapdomain.ImportPermissions{
		CanRenameMiners: canRenameMiners,
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(resp), nil
}
