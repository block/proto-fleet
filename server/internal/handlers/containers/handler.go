package containers

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/command"
	sdk "github.com/block/proto-fleet/server/sdk/v1"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/containers"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	service  *containers.Service
	commands *command.Service
}

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
	for _, container := range rows {
		container.CanControlEquipment = canWriteEquipment(ctx, container)
		container.CanAssignSite = CanAssignSite(ctx, container)
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
	container.CanControlEquipment = canWriteEquipment(ctx, container)
	container.CanAssignSite = CanAssignSite(ctx, container)
	return connect.NewResponse(&pb.GetContainerResponse{Container: container}), nil
}

func (h *Handler) WithCommands(commands *command.Service) *Handler { h.commands = commands; return h }

func (h *Handler) SetEquipmentControl(ctx context.Context, req *connect.Request[pb.SetEquipmentControlRequest]) (*connect.Response[pb.SetEquipmentControlResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("equipment commands unavailable")
	}
	result, err := h.commands.SetContainerControl(ctx, req.Msg.DeviceIdentifier, sdk.ContainerControlRequest{Name: req.Msg.Name, Enabled: req.Msg.Enabled}, authorizeEquipmentWrite)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetEquipmentControlResponse{BatchIdentifier: result.BatchIdentifier}), nil
}

// Plant controls require both cooling and power configuration authority at the
// current site. A locator-LED or read-only grant never authorizes plant writes.
func authorizeEquipmentWrite(ctx context.Context, siteID *int64) error {
	for _, permission := range []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead, authz.PermMinerSetCoolingMode, authz.PermMinerSetPowerTarget} {
		if _, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{SiteID: siteID}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) GetEquipmentCommandResult(ctx context.Context, req *connect.Request[pb.GetEquipmentCommandResultRequest]) (*connect.Response[pb.GetEquipmentCommandResultResponse], error) {
	if h.commands == nil {
		return nil, fleeterror.NewUnimplementedError("equipment commands unavailable")
	}
	result, err := h.commands.GetContainerControlResult(ctx, req.Msg.DeviceIdentifier, req.Msg.BatchIdentifier, func(ctx context.Context, site *int64) error {
		allowed, err := canReadContainer(ctx)(site)
		if err != nil {
			return err
		}
		if !allowed {
			return fleeterror.NewForbiddenError("container access denied")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetEquipmentCommandResultResponse{Result: result}), nil
}

func canWriteEquipment(ctx context.Context, container *pb.Container) bool {
	var site *int64
	if container.Site != nil {
		site = &container.Site.Id
	}
	return authorizeEquipmentWrite(ctx, site) == nil
}

// CanAssignSite checks assignment authority at the container's current site.
func CanAssignSite(ctx context.Context, container *pb.Container) bool {
	var site *int64
	if container.Site != nil {
		site = &container.Site.Id
	}
	_, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{SiteID: site})
	return err == nil
}
