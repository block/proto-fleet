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

	rows, err := h.service.List(ctx, info.OrganizationID, req.Msg.SiteIds, req.Msg.IncludeUnassigned, canReadContainer(ctx))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ListContainersResponse{Containers: rows}), nil
}

func canReadContainer(ctx context.Context) func(*int64) (bool, error) {
	return func(siteID *int64) (bool, error) {
		for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead} {
			allowed, err := middleware.HasPermission(ctx, permission, authz.ResourceContext{SiteID: siteID})
			if err != nil || !allowed {
				return allowed, err
			}
		}
		return true, nil
	}
}
func (h *Handler) GetContainer(ctx context.Context, req *connect.Request[pb.GetContainerRequest]) (*connect.Response[pb.GetContainerResponse], error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	container, err := h.service.Get(ctx, info.OrganizationID, req.Msg.DeviceIdentifier, canReadContainer(ctx))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetContainerResponse{Container: container}), nil
}
