// Package infrastructure is the Connect-RPC surface for
// InfrastructureService.
//
// All RPCs enforce site-scoped RBAC: reads require site:read and
// writes site:manage evaluated against the device's site. Rack names
// are returned only with rack:read, and rack assignments or site moves
// also require rack:read. Permissions are evaluated with
// ResourceContext{SiteID}, so a caller whose org-wide grant is
// narrowed away for a site cannot read or mutate that site's device
// configuration. Get/Update/Delete resolve the device under org scope
// first and then authorize against its current site; a caller without
// site:read there gets NotFound (not PermissionDenied) so device IDs
// cannot be enumerated across site scopes — the same masking
// ResolveSiteBySlug applies. PermissionDenied is reserved for callers
// who can read the device but lack site:manage.
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
// grant). A plain denial is (false, nil); genuine auth/wiring
// failures propagate as errors per middleware.HasPermission.
func canReadSite(ctx context.Context, siteID int64) (bool, error) {
	return middleware.HasPermission(ctx, authz.PermSiteRead, authz.ResourceContext{SiteID: &siteID})
}

// canManageSite reports whether the caller holds site:manage for the
// given site. Read responses use it to decide whether driver_config —
// the OT control topology — is included; site:read callers get the
// display fields only.
func canManageSite(ctx context.Context, siteID int64) (bool, error) {
	return middleware.HasPermission(ctx, authz.PermSiteManage, authz.ResourceContext{SiteID: &siteID})
}

// canReadRack reports whether the caller can see rack inventory at the
// given site. Infrastructure reads use it to redact rack placement
// without hiding the rest of the site-readable device.
func canReadRack(ctx context.Context, siteID int64) (bool, error) {
	return middleware.HasPermission(ctx, authz.PermRackRead, authz.ResourceContext{SiteID: &siteID})
}

func requireSiteManage(ctx context.Context, siteID int64) error {
	_, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{SiteID: &siteID})
	return err
}

func requireRackRead(ctx context.Context, siteID int64) error {
	_, err := middleware.RequirePermission(ctx, authz.PermRackRead, authz.ResourceContext{SiteID: &siteID})
	return err
}

// getReadableDevice resolves the device under org scope and masks a
// site:read denial as NotFound, so a caller scoped away from the
// device's site cannot distinguish an existing device ID from a
// missing one. Get/Update/Delete all resolve through this before any
// further authorization.
func (h *Handler) getReadableDevice(ctx context.Context, orgID, id int64) (*models.Device, error) {
	device, err := h.service.Get(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	readable, err := canReadSite(ctx, device.SiteID)
	if err != nil {
		return nil, err
	}
	if !readable {
		return nil, fleeterror.NewNotFoundErrorf("infrastructure device %d not found", id)
	}
	return device, nil
}

func (h *Handler) ListInfrastructureDevices(ctx context.Context, req *connect.Request[pb.ListInfrastructureDevicesRequest]) (*connect.Response[pb.ListInfrastructureDevicesResponse], error) {
	sess, err := sessionInfo(ctx)
	if err != nil {
		return nil, err
	}
	// Push the caller's readable-site set into the SQL filter so
	// site-narrowed operators see their sites' devices and nothing
	// else, without fetching the whole org and dropping rows here.
	orgWide, scopedSites, err := middleware.SiteScopeForPermission(ctx, authz.PermSiteRead)
	if err != nil {
		return nil, err
	}
	filter := toListFilter(req.Msg, sess.OrganizationID)
	if orgWide {
		// Org-wide grant: exclude only the sites narrowed away.
		filter.ExcludedSiteIDs = scopedSites
	} else {
		// Site-scoped grants only: intersect the request's site filter
		// with the readable allowlist. An empty result means the
		// caller can read no requested site — return empty without
		// querying (an empty SiteIDs filter would mean "all sites").
		allowed := intersectSiteIDs(filter.SiteIDs, scopedSites)
		if len(allowed) == 0 {
			return connect.NewResponse(&pb.ListInfrastructureDevicesResponse{
				Devices: []*pb.InfrastructureDevice{},
			}), nil
		}
		filter.SiteIDs = allowed
	}
	devices, err := h.service.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	// driver_config is included per device only where the caller holds
	// site:manage. The canReadSite check is a fail-closed backstop for
	// the SQL filter above — it costs nothing (in-memory map lookup)
	// and keeps unreadable rows out even if filter composition drifts.
	out := make([]*pb.InfrastructureDevice, 0, len(devices))
	for i := range devices {
		device := &devices[i]
		readable, err := canReadSite(ctx, device.SiteID)
		if err != nil {
			return nil, err
		}
		if !readable {
			continue
		}
		resourceContext := authz.ResourceContext{SiteID: &device.SiteID}
		if req.Msg.GetRequireCurtailmentManage() {
			canManageCurtailment, err := middleware.HasPermission(ctx, authz.PermCurtailmentManage, resourceContext)
			if err != nil {
				return nil, err
			}
			if !canManageCurtailment {
				continue
			}
		}
		manageable, err := canManageSite(ctx, device.SiteID)
		if err != nil {
			return nil, err
		}
		rackReadable, err := canReadRack(ctx, device.SiteID)
		if err != nil {
			return nil, err
		}
		out = append(out, toProtoDevice(device, manageable, rackReadable))
	}
	return connect.NewResponse(&pb.ListInfrastructureDevicesResponse{Devices: out}), nil
}

// intersectSiteIDs returns the requested site ids that are also
// readable, or the full readable allowlist when the request carries no
// site filter.
func intersectSiteIDs(requested, readable []int64) []int64 {
	if len(requested) == 0 {
		return readable
	}
	readableSet := make(map[int64]bool, len(readable))
	for _, id := range readable {
		readableSet[id] = true
	}
	out := make([]int64, 0, len(requested))
	for _, id := range requested {
		if readableSet[id] {
			out = append(out, id)
		}
	}
	return out
}

func (h *Handler) GetInfrastructureDevice(ctx context.Context, req *connect.Request[pb.GetInfrastructureDeviceRequest]) (*connect.Response[pb.GetInfrastructureDeviceResponse], error) {
	sess, err := sessionInfo(ctx)
	if err != nil {
		return nil, err
	}
	device, err := h.getReadableDevice(ctx, sess.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	manageable, err := canManageSite(ctx, device.SiteID)
	if err != nil {
		return nil, err
	}
	rackReadable, err := canReadRack(ctx, device.SiteID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetInfrastructureDeviceResponse{
		Device: toProtoDevice(device, manageable, rackReadable),
	}), nil
}

func (h *Handler) CreateInfrastructureDevice(ctx context.Context, req *connect.Request[pb.CreateInfrastructureDeviceRequest]) (*connect.Response[pb.CreateInfrastructureDeviceResponse], error) {
	siteID := req.Msg.GetSiteId()
	info, err := middleware.RequirePermission(ctx, authz.PermSiteManage, authz.ResourceContext{SiteID: &siteID})
	if err != nil {
		return nil, err
	}
	if req.Msg.GetRackName() != "" {
		if err := requireRackRead(ctx, siteID); err != nil {
			return nil, err
		}
	}
	device, err := h.service.Create(ctx, toCreateParams(req.Msg, info.OrganizationID))
	if err != nil {
		return nil, err
	}
	rackReadable, err := canReadRack(ctx, device.SiteID)
	if err != nil {
		return nil, err
	}
	// The caller proved site:manage, so the config they just wrote is
	// echoed back. Rack placement still follows its independent read gate.
	return connect.NewResponse(&pb.CreateInfrastructureDeviceResponse{
		Device: toProtoDevice(device, true, rackReadable),
	}), nil
}

func (h *Handler) UpdateInfrastructureDevice(ctx context.Context, req *connect.Request[pb.UpdateInfrastructureDeviceRequest]) (*connect.Response[pb.UpdateInfrastructureDeviceResponse], error) {
	sess, err := sessionInfo(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := h.getReadableDevice(ctx, sess.OrganizationID, req.Msg.GetId())
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
	if req.Msg.RackName != nil || req.Msg.GetSiteId() != existing.SiteID {
		if err := requireRackRead(ctx, existing.SiteID); err != nil {
			return nil, err
		}
		if req.Msg.GetSiteId() != existing.SiteID {
			if err := requireRackRead(ctx, req.Msg.GetSiteId()); err != nil {
				return nil, err
			}
		}
	}
	// Predicate the write on the site we authorized against, so a
	// concurrent move between the read above and the write fails
	// closed (NotFound) instead of mutating a device now in a site the
	// caller may not manage.
	params := toUpdateParams(req.Msg, sess.OrganizationID)
	params.ExpectedSiteID = existing.SiteID
	device, err := h.service.Update(ctx, params)
	if err != nil {
		return nil, err
	}
	rackReadable, err := canReadRack(ctx, device.SiteID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateInfrastructureDeviceResponse{
		Device: toProtoDevice(device, true, rackReadable),
	}), nil
}

func (h *Handler) DeleteInfrastructureDevice(ctx context.Context, req *connect.Request[pb.DeleteInfrastructureDeviceRequest]) (*connect.Response[pb.DeleteInfrastructureDeviceResponse], error) {
	sess, err := sessionInfo(ctx)
	if err != nil {
		return nil, err
	}
	device, err := h.getReadableDevice(ctx, sess.OrganizationID, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if err := requireSiteManage(ctx, device.SiteID); err != nil {
		return nil, err
	}
	// Predicate the delete on the authorized site (same stale-move
	// guard as Update).
	if err := h.service.Delete(ctx, sess.OrganizationID, req.Msg.GetId(), device.SiteID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteInfrastructureDeviceResponse{}), nil
}
