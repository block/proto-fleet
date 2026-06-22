package admin

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/discovery"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/enrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/pairing"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	fleetnodeadminv1connect.UnimplementedFleetNodeAdminServiceHandler

	enrollment *enrollment.Service
	pairing    *pairing.Service
	discovery  *discovery.Service
}

var _ fleetnodeadminv1connect.FleetNodeAdminServiceHandler = &Handler{}

func NewHandler(enrollment *enrollment.Service, pairing *pairing.Service, discoverySvc *discovery.Service) *Handler {
	return &Handler{enrollment: enrollment, pairing: pairing, discovery: discoverySvc}
}

func (h *Handler) CreateEnrollmentCode(ctx context.Context, _ *connect.Request[pb.CreateEnrollmentCodeRequest]) (*connect.Response[pb.CreateEnrollmentCodeResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	code, expiresAt, err := h.enrollment.CreateCode(ctx, info.UserID, info.OrganizationID, 0)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateEnrollmentCodeResponse{
		Code:      code,
		ExpiresAt: timestamppb.New(expiresAt),
	}), nil
}

func (h *Handler) ListFleetNodes(ctx context.Context, _ *connect.Request[pb.ListFleetNodesRequest]) (*connect.Response[pb.ListFleetNodesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	fleetNodes, err := h.enrollment.ListFleetNodes(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListFleetNodesResponse{FleetNodes: make([]*pb.FleetNodeSummary, 0, len(fleetNodes))}
	for _, n := range fleetNodes {
		summary := &pb.FleetNodeSummary{
			FleetNodeId:         n.ID,
			Name:                n.Name,
			EnrollmentStatus:    deriveDisplayStatus(n),
			IdentityFingerprint: enrollment.IdentityFingerprint(n.IdentityPubkey),
			CreatedAt:           timestamppb.New(n.CreatedAt),
		}
		if n.LastSeenAt != nil {
			summary.LastSeenAt = timestamppb.New(*n.LastSeenAt)
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
	apiKey, expiresAt, err := h.enrollment.Confirm(ctx, req.Msg.GetFleetNodeId(), info.OrganizationID)
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
	if err := h.enrollment.RevokeFleetNode(ctx, req.Msg.GetFleetNodeId(), info.OrganizationID); err != nil {
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
			AssignedAt:       timestamppb.New(p.AssignedAt),
		}
		if p.AssignedBy != nil {
			summary.AssignedBy = p.AssignedBy
		}
		resp.Pairs = append(resp.Pairs, summary)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ListFleetNodeDiscoveredDevices(ctx context.Context, req *connect.Request[pb.ListFleetNodeDiscoveredDevicesRequest]) (*connect.Response[pb.ListFleetNodeDiscoveredDevicesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeRead, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	var fleetNodeID *int64
	if id := req.Msg.GetFleetNodeId(); id > 0 {
		fleetNodeID = &id
	}
	var cursor *int64
	if c := req.Msg.GetCursor(); c > 0 {
		cursor = &c
	}
	var limit *int64
	if l := req.Msg.GetLimit(); l > 0 {
		v := int64(l)
		limit = &v
	}
	devices, nextCursor, err := h.pairing.ListDiscoveredDevicesForFleetNode(ctx, info.OrganizationID, fleetNodeID, cursor, limit)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListFleetNodeDiscoveredDevicesResponse{Devices: make([]*pb.FleetNodeDiscoveredDevice, 0, len(devices))}
	if nextCursor != nil {
		resp.NextCursor = *nextCursor
	}
	for _, d := range devices {
		resp.Devices = append(resp.Devices, &pb.FleetNodeDiscoveredDevice{
			FleetNodeId:      d.FleetNodeID,
			DeviceIdentifier: d.DeviceIdentifier,
			IpAddress:        d.IPAddress,
			Port:             d.Port,
			UrlScheme:        d.URLScheme,
			DriverName:       d.DriverName,
			Model:            d.Model,
			Manufacturer:     d.Manufacturer,
			FirmwareVersion:  d.FirmwareVersion,
			LastSeen:         timestamppb.New(d.LastSeen),
			PairingStatus:    d.PairingStatus,
		})
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

	return h.discovery.RunOnNode(ctx, fleetNodeID, discoverReq, func(batch *pairingpb.DiscoverResponse) error {
		if sendErr := stream.Send(&pb.DiscoverOnFleetNodeResponse{Response: batch}); sendErr != nil {
			return fleeterror.NewInternalErrorf("send batch to operator: %v", sendErr)
		}
		return nil
	})
}

func (h *Handler) PairDiscoveredDevicesOnFleetNode(ctx context.Context, req *connect.Request[pb.PairDiscoveredDevicesOnFleetNodeRequest], stream *connect.ServerStream[pb.PairDiscoveredDevicesOnFleetNodeResponse]) error {
	info, err := middleware.RequirePermission(ctx, authz.PermFleetnodeManage, authz.ResourceContext{})
	if err != nil {
		return err
	}
	if _, err := middleware.RequirePermission(ctx, authz.PermMinerPair, authz.ResourceContext{}); err != nil {
		return err
	}
	fleetNodeID := req.Msg.GetFleetNodeId()
	if fleetNodeID <= 0 {
		return fleeterror.NewInvalidArgumentError("fleet_node_id is required")
	}
	if req.Msg.GetPairAllUnpaired() && len(req.Msg.GetDeviceIdentifiers()) > 0 {
		return fleeterror.NewInvalidArgumentError("device_identifiers must be empty when pair_all_unpaired is true")
	}

	node, err := h.enrollment.GetFleetNodeByID(ctx, fleetNodeID, info.OrganizationID)
	if err != nil {
		return err
	}
	if node.EnrollmentStatus != enrollment.FleetNodeStatusConfirmed {
		return fleeterror.NewFailedPreconditionError("fleet node is not CONFIRMED")
	}

	targets, err := h.pairing.PairTargetsForDiscoveredDevices(ctx, info.OrganizationID, fleetNodeID, req.Msg.GetDeviceIdentifiers(), req.Msg.GetPairAllUnpaired())
	if err != nil {
		return err
	}
	pairReq := &pairingpb.FleetNodePairRequest{
		Targets:     targets,
		Credentials: req.Msg.GetCredentials(),
	}
	return h.discovery.RunPairOnNode(ctx, fleetNodeID, pairReq, func(results []*gatewaypb.FleetNodePairResult) error {
		resp := &pb.PairDiscoveredDevicesOnFleetNodeResponse{Results: make([]*pb.DevicePairingResult, 0, len(results))}
		for _, r := range results {
			resp.Results = append(resp.Results, &pb.DevicePairingResult{
				DeviceIdentifier: r.GetDeviceIdentifier(),
				PairingStatus:    pairing.PairingStatusForOutcome(r.GetOutcome()),
				Error:            r.GetErrorMessage(),
			})
		}
		if sendErr := stream.Send(resp); sendErr != nil {
			return fleeterror.NewInternalErrorf("send pair results to operator: %v", sendErr)
		}
		return nil
	})
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
