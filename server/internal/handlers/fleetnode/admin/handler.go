package admin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/ha"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	fleetnodeadminv1connect.UnimplementedFleetNodeAdminServiceHandler

	enrollment   *enrollment.Service
	pairing      *pairing.Service
	discovery    *discovery.Service
	pairingState pairingStateAnnotator
	registry     *control.Registry
	ha           *ha.Service
}

type pairingStateAnnotator interface {
	AnnotateAlreadyPaired(ctx context.Context, orgID int64, devices []*pairingpb.Device) error
}

var _ fleetnodeadminv1connect.FleetNodeAdminServiceHandler = &Handler{}

func NewHandler(
	enrollment *enrollment.Service,
	pairing *pairing.Service,
	discoverySvc *discovery.Service,
	registry *control.Registry,
	haSvc *ha.Service,
	pairingState pairingStateAnnotator,
) *Handler {
	return &Handler{
		enrollment:   enrollment,
		pairing:      pairing,
		discovery:    discoverySvc,
		pairingState: pairingState,
		registry:     registry,
		ha:           haSvc,
	}
}

func (h *Handler) CreateEnrollmentCode(ctx context.Context, req *connect.Request[pb.CreateEnrollmentCodeRequest]) (*connect.Response[pb.CreateEnrollmentCodeResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	var (
		code                string
		pendingEnrollmentID int64
		expiresAt           time.Time
	)
	if req.Msg.StandbyForNodeId != nil {
		code, pendingEnrollmentID, expiresAt, err = h.enrollment.CreateStandbyCode(ctx, info.UserID, info.OrganizationID, 0, req.Msg.GetStandbyForNodeId())
	} else {
		code, pendingEnrollmentID, expiresAt, err = h.enrollment.CreateCodeWithEnrollmentID(ctx, info.UserID, info.OrganizationID, 0)
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateEnrollmentCodeResponse{
		Code:                code,
		ExpiresAt:           timestamppb.New(expiresAt),
		PendingEnrollmentId: pendingEnrollmentID,
	}), nil
}

func (h *Handler) ListFleetNodes(ctx context.Context, _ *connect.Request[pb.ListFleetNodesRequest]) (*connect.Response[pb.ListFleetNodesResponse], error) {
	info, err := middleware.RequireAnyPermission(ctx, []string{authz.PermFleetnodeRead, authz.PermFleetnodeManage, authz.PermMinerPair}, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	_, detailsErr := middleware.RequireAnyPermission(ctx, []string{authz.PermFleetnodeRead, authz.PermFleetnodeManage}, authz.ResourceContext{})
	includeAdministrativeDetails := detailsErr == nil
	fleetNodes, err := h.enrollment.ListFleetNodes(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	var pairedCounts map[int64]int64
	if includeAdministrativeDetails {
		pairedCounts, err = h.pairing.CountDevicesByNode(ctx, info.OrganizationID)
		if err != nil {
			return nil, err
		}
	}
	connected := make(map[int64]struct{})
	for _, fleetNodeID := range h.registry.ConnectedFleetNodeIDs() {
		connected[fleetNodeID] = struct{}{}
	}
	names := make(map[int64]string, len(fleetNodes))
	for _, n := range fleetNodes {
		names[n.ID] = n.Name
	}
	resp := &pb.ListFleetNodesResponse{FleetNodes: make([]*pb.FleetNodeSummary, 0, len(fleetNodes))}
	for _, n := range fleetNodes {
		_, controlStreamConnected := connected[n.ID]
		protocolUpgradeRequired := h.registry.CommandProtocolUpgradeRequired(n.ID)
		summary := &pb.FleetNodeSummary{
			EnrollmentStatus:                  deriveDisplayStatus(n),
			CommandProtocolUpgradeRequired:    protocolUpgradeRequired,
			CommandProtocolUpgradeRecommended: h.registry.CommandProtocolUpgradeRecommended(n.ID),
			ControlStreamConnected:            controlStreamConnected,
			DiscoveryCoverageDegraded:         n.DiscoveryCoverageDegraded(controlStreamConnected, protocolUpgradeRequired),
		}
		if includeAdministrativeDetails {
			summary.FleetNodeId = n.ID
			if config := h.registry.DiscoveryConfig(n.ID); config != nil {
				summary.DiscoveryRanges = config.GetRanges()
				summary.DiscoveryConfigReported = true
			}
			summary.PairedDeviceCount = pairedCounts[n.ID]
			summary.Name = n.Name
			summary.IpAddresses = n.IPAddresses
			summary.Version = n.Version
			summary.EnrolledBy = n.EnrolledBy
			summary.IdentityFingerprint = enrollment.IdentityFingerprint(n.IdentityPubkey)
			summary.CreatedAt = timestamppb.New(n.CreatedAt)
			if n.PendingEnrollmentID != nil {
				summary.PendingEnrollmentId = n.PendingEnrollmentID
			}
			if n.LastSeenAt != nil {
				summary.LastSeenAt = timestamppb.New(*n.LastSeenAt)
			}
			setHASummary(summary, n, names)
			if peerID := summary.HaPeerNodeId; peerID != nil {
				config, peer := h.registry.DiscoveryConfig(n.ID), h.registry.DiscoveryConfig(*peerID)
				summary.DiscoveryConfigMismatch = config != nil && peer != nil && !slices.Equal(config.GetRanges(), peer.GetRanges())
			}
		}
		resp.FleetNodes = append(resp.FleetNodes, summary)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ConfirmFleetNode(ctx context.Context, req *connect.Request[pb.ConfirmFleetNodeRequest]) (*connect.Response[pb.ConfirmFleetNodeResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	apiKey, expiresAt, err := h.enrollment.ConfirmExpected(ctx, req.Msg.GetFleetNodeId(), info.OrganizationID, req.Msg.GetPendingEnrollmentId(), info.UserID)
	if err != nil {
		return nil, err
	}
	resp := &pb.ConfirmFleetNodeResponse{ApiKey: apiKey}
	if !expiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(expiresAt)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) RevokeFleetNode(ctx context.Context, req *connect.Request[pb.RevokeFleetNodeRequest]) (*connect.Response[pb.RevokeFleetNodeResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	fleetNodeID := req.Msg.GetFleetNodeId()
	if req.Msg.PendingEnrollmentId != nil {
		if err := h.enrollment.RevokeFleetNodeForEnrollment(ctx, fleetNodeID, info.OrganizationID, req.Msg.GetPendingEnrollmentId()); err != nil {
			return nil, err
		}
		return connect.NewResponse(&pb.RevokeFleetNodeResponse{}), nil
	}
	if err := h.enrollment.RevokeFleetNode(ctx, fleetNodeID, info.OrganizationID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevokeFleetNodeResponse{}), nil
}

func (h *Handler) PairDeviceToFleetNode(ctx context.Context, req *connect.Request[pb.PairDeviceToFleetNodeRequest]) (*connect.Response[pb.PairDeviceToFleetNodeResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	assignedBy := info.UserID
	if err := h.pairing.PairDevice(ctx, req.Msg.GetFleetNodeId(), req.Msg.GetDeviceId(), info.OrganizationID, &assignedBy); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PairDeviceToFleetNodeResponse{}), nil
}

func (h *Handler) UnpairDevice(ctx context.Context, req *connect.Request[pb.UnpairDeviceRequest]) (*connect.Response[pb.UnpairDeviceResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if err := h.pairing.UnpairDevice(ctx, req.Msg.GetDeviceId(), info.OrganizationID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UnpairDeviceResponse{}), nil
}

func (h *Handler) ListFleetNodeDevices(ctx context.Context, req *connect.Request[pb.ListFleetNodeDevicesRequest]) (*connect.Response[pb.ListFleetNodeDevicesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	var pairs []pairing.FleetNodeDevice
	if fleetNodeID := req.Msg.GetFleetNodeId(); fleetNodeID > 0 {
		pairs, err = h.pairing.ListDevicesForFleetNode(ctx, fleetNodeID, info.OrganizationID)
	} else {
		pairs, err = h.pairing.ListPairs(ctx, info.OrganizationID)
	}
	if err != nil {
		return nil, err
	}
	resp := &pb.ListFleetNodeDevicesResponse{Pairs: make([]*pb.FleetNodeDeviceSummary, 0, len(pairs))}
	for _, p := range pairs {
		summary := &pb.FleetNodeDeviceSummary{
			FleetNodeId:      p.FleetNodeID,
			DeviceId:         p.DeviceID,
			DeviceIdentifier: p.DeviceIdentifier,
			DeviceType:       p.DeviceType,
			Manufacturer:     p.Manufacturer,
			Model:            p.Model,
			AssignedAt:       timestamppb.New(p.AssignedAt),
		}
		if p.AssignedBy != nil {
			summary.AssignedBy = p.AssignedBy
		}
		resp.Pairs = append(resp.Pairs, summary)
	}
	return connect.NewResponse(resp), nil
}

// DiscoverOnFleetNode runs discovery on a single CONFIRMED node and streams the
// node's device batches back to the operator. See discovery.RunOnNode for the
// dispatch/drain loop.
func (h *Handler) DiscoverOnFleetNode(ctx context.Context, req *connect.Request[pb.DiscoverOnFleetNodeRequest], stream *connect.ServerStream[pb.DiscoverOnFleetNodeResponse]) error {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
	if err != nil {
		return err
	}
	fleetNodeID := req.Msg.GetFleetNodeId()
	if fleetNodeID <= 0 {
		return fleeterror.NewInvalidArgumentError("fleet_node_id is required")
	}
	discoverReq := req.Msg.GetRequest()
	if discoverReq == nil {
		return fleeterror.NewInvalidArgumentError("request is required")
	}

	node, err := h.enrollment.GetFleetNodeByID(ctx, fleetNodeID, info.OrganizationID)
	if err != nil {
		return err
	}
	if node.EnrollmentStatus != enrollment.FleetNodeStatusConfirmed {
		return fleeterror.NewFailedPreconditionError("fleet node is not CONFIRMED")
	}
	if err := h.ha.RequireNotStandby(ctx, info.OrganizationID, fleetNodeID); err != nil {
		return err
	}

	discoveryCtx, cancel := discovery.NewRequestContext(ctx)
	defer cancel()
	source := fmt.Sprintf("Fleet Node %d", fleetNodeID)
	runErr := h.discovery.RunOnNode(discoveryCtx, fleetNodeID, source, discoverReq, func(batch *pairingpb.DiscoverResponse) error {
		// The shipped Fleet Node can upload at most 40 discovery batches for one
		// request. They fit in the command's 64-event buffer while this lookup runs.
		if err := h.pairingState.AnnotateAlreadyPaired(discoveryCtx, info.OrganizationID, batch.GetDevices()); err != nil {
			if discoveryCtx.Err() != nil {
				return discoveryCtx.Err()
			}
			return fleeterror.NewInternalErrorf("annotate discovered devices with pairing state: %v", err)
		}
		if sendErr := stream.Send(&pb.DiscoverOnFleetNodeResponse{Response: batch}); sendErr != nil {
			return fleeterror.NewInternalErrorf("send batch to operator: %v", sendErr)
		}
		return nil
	})
	if runErr == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return connect.NewError(connect.CodeDeadlineExceeded, ctxErr)
		}
		return nil
	}
	if errors.Is(discoveryCtx.Err(), context.DeadlineExceeded) {
		if err := stream.Send(&pb.DiscoverOnFleetNodeResponse{Response: &pairingpb.DiscoverResponse{
			Warning: fmt.Sprintf("%s: %s", source, discoverylimits.RequestTimeoutWarning),
		}}); err != nil {
			return fleeterror.NewInternalErrorf("send discovery timeout warning: %v", err)
		}
		return nil
	}
	return runErr
}

func setHASummary(summary *pb.FleetNodeSummary, n enrollment.FleetNodeListing, names map[int64]string) {
	var peer *int64
	switch {
	case n.HAActiveNodeID != nil && *n.HAActiveNodeID == n.ID:
		summary.HaRole = pb.FleetNodeHARole_FLEET_NODE_HA_ROLE_ACTIVE
		peer = n.HAStandbyNodeID
	case n.IsHAStandby():
		summary.HaRole = pb.FleetNodeHARole_FLEET_NODE_HA_ROLE_STANDBY
		peer = n.HAActiveNodeID
	case n.StandbyForNodeID != nil:
		summary.HaRole = pb.FleetNodeHARole_FLEET_NODE_HA_ROLE_STANDBY
		peer = n.StandbyForNodeID
	default:
		return
	}
	summary.HaPeerNodeId = peer
	summary.HaPeerNodeName = names[*peer]
	summary.HaStandbyReady = n.HAStandbyReady
	summary.HaStatusReason = n.HAStatusReason
}

// AWAITING_CONFIRMATION lives only on pending_enrollment, so a PENDING fleet
// node whose pending row is AWAITING_CONFIRMATION surfaces as such instead.
func deriveDisplayStatus(n enrollment.FleetNodeListing) pb.FleetNodeEnrollmentStatus {
	switch n.EnrollmentStatus {
	case enrollment.FleetNodeStatusPending:
		if n.PendingEnrollmentStatus == enrollment.StatusAwaitingConfirmation {
			return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_AWAITING_CONFIRMATION
		}
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_PENDING
	case enrollment.FleetNodeStatusConfirmed:
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_CONFIRMED
	case enrollment.FleetNodeStatusRevoked:
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_REVOKED
	}
	return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_UNSPECIFIED
}
