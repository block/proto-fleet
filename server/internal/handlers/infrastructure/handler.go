// Package infrastructure is the Connect-RPC surface for
// InfrastructureService.
//
// All RPCs enforce site-scoped RBAC: reads require site:read and
// writes site:manage evaluated against the device's site
// (ResourceContext{SiteID}), so a caller whose org-wide grant is
// narrowed away for a site cannot read or mutate that site's device
// configuration. Get/Update/Delete resolve the device under org scope
// first and then authorize against its current site — the same
// resolve-then-authorize pattern (and accepted same-org existence
// leak) as buildings.GetBuildingStats.
package infrastructure

import (
	"context"

	"connectrpc.com/connect"

	pb "github.com/block/proto-fleet/server/generated/grpc/infrastructure/v1"
	"github.com/block/proto-fleet/server/generated/grpc/infrastructure/v1/infrastructurev1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

// Handler implements the InfrastructureService Connect-RPC surface.
type Handler struct {
	service *infrastructure.Service
}

var _ infrastructurev1connect.InfrastructureServiceHandler = &Handler{}

// NewHandler returns an InfrastructureService handler bound to the
// supplied domain service.
func NewHandler(service *infrastructure.Service) *Handler {
	return &Handler{service: service}
}

// sessionInfo resolves the caller's session, mapping a missing
// session to Unauthenticated the same way RequirePermission does.
func sessionInfo(ctx context.Context) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	return info, nil
}

// canReadSite reports whether the caller holds site:read for the
// given site (via a site-scoped assignment or an unnarrowed org-wide
// grant).
func canReadSite(ctx context.Context, siteID int64) bool {
	_, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{SiteID: &siteID})
	return err == nil
}

func requireSiteManage(ctx context.Context, siteID int64) error {
	_, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{SiteID: &siteID})
	return err
}

func (h *Handler) ListInfrastructureDevices(ctx context.Context, req *connect.Request[pb.ListInfrastructureDevicesRequest]) (*connect.Response[pb.ListInfrastructureDevicesResponse], error) {
	sess, err := sessionInfo(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := h.service.List(ctx, toListFilter(req.Msg, sess.OrganizationID))
	if err != nil {
		return nil, err
	}
	// Filter to sites the caller can read rather than gating on an
	// org-wide grant, so site-narrowed operators see their sites'
	// devices and nothing else.
	authorized := make([]models.Device, 0, len(devices))
	for _, device := range devices {
		if canReadSite(ctx, device.SiteID) {
			authorized = append(authorized, device)
		}
	}
	return connect.NewResponse(toListResponse(authorized)), nil
}

func (h *Handler) GetInfrastructureDevice(ctx context.Context, req *connect.Request[pb.GetInfrastructureDeviceRequest]) (*connect.Response[pb.GetInfrastructureDeviceResponse], error) {
	sess, err := sessionInfo(ctx)
	if err != nil {
		return nil, err
	}
	device, err := h.service.Get(ctx, sess.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if _, err := middleware.RequirePermission(ctx, authz.PermSiteRead, authz.ResourceContext{SiteID: &device.SiteID}); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetInfrastructureDeviceResponse{
		Device: toProtoDevice(device),
	}), nil
}

func (h *Handler) CreateInfrastructureDevice(ctx context.Context, req *connect.Request[pb.CreateInfrastructureDeviceRequest]) (*connect.Response[pb.CreateInfrastructureDeviceResponse], error) {
	siteID := req.Msg.GetSiteId()
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{SiteID: &siteID})
	if err != nil {
		return nil, err
	}
	device, err := h.service.Create(ctx, toCreateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateInfrastructureDeviceResponse{
		Device: toProtoDevice(device),
	}), nil
}

func (h *Handler) UpdateInfrastructureDevice(ctx context.Context, req *connect.Request[pb.UpdateInfrastructureDeviceRequest]) (*connect.Response[pb.UpdateInfrastructureDeviceResponse], error) {
	sess, err := sessionInfo(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := h.service.Get(ctx, sess.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	// Authorize against the device's current site, and additionally
	// against the target site when the update moves it — a manager of
	// only the target site must not be able to pull a device out of a
	// site they don't manage, and vice versa.
	if err := requireSiteManage(ctx, existing.SiteID); err != nil {
		return nil, err
	}
	if req.Msg.GetSiteId() != existing.SiteID {
		if err := requireSiteManage(ctx, req.Msg.GetSiteId()); err != nil {
			return nil, err
		}
	}
	device, err := h.service.Update(ctx, toUpdateParams(req.Msg, sess.OrganizationID))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateInfrastructureDeviceResponse{
		Device: toProtoDevice(device),
	}), nil
}

func (h *Handler) DeleteInfrastructureDevice(ctx context.Context, req *connect.Request[pb.DeleteInfrastructureDeviceRequest]) (*connect.Response[pb.DeleteInfrastructureDeviceResponse], error) {
	sess, err := sessionInfo(ctx)
	if err != nil {
		return nil, err
	}
	device, err := h.service.Get(ctx, sess.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := requireSiteManage(ctx, device.SiteID); err != nil {
		return nil, err
	}
	if err := h.service.Delete(ctx, sess.OrganizationID, req.Msg.GetId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteInfrastructureDeviceResponse{}), nil
}
