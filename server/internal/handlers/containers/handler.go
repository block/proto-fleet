package containers

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct{ service *containers.Service }

func NewHandler(service *containers.Service) *Handler { return &Handler{service: service} }
func (h *Handler) ListContainers(ctx context.Context, req *connect.Request[pb.ListContainersRequest]) (*connect.Response[pb.ListContainersResponse], error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	canRead := func(siteID *int64) (bool, error) {
		resource := authz.ResourceContext{SiteID: siteID}
		allowed, err := middleware.HasPermission(ctx, authz.PermSiteRead, resource)
		if err != nil || !allowed {
			return allowed, err
		}
		allowed, err = middleware.HasPermission(ctx, authz.PermFleetRead, resource)
		if err != nil || !allowed {
			return allowed, err
		}
		return middleware.HasPermission(ctx, authz.PermMinerRead, resource)
	}
	rows, err := h.service.List(ctx, info.OrganizationID, req.Msg.SiteIds, req.Msg.IncludeUnassigned, canRead)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ListContainersResponse{Containers: rows}), nil
}
