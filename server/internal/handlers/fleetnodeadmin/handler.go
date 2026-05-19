package fleetnodeadmin

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeenrollment"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodepairing"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

type Handler struct {
	fleetnodeadminv1connect.UnimplementedFleetNodeAdminServiceHandler

	enrollment *fleetnodeenrollment.Service
	pairing    *fleetnodepairing.Service
}

var _ fleetnodeadminv1connect.FleetNodeAdminServiceHandler = &Handler{}

func NewHandler(enrollment *fleetnodeenrollment.Service, pairing *fleetnodepairing.Service) *Handler {
	return &Handler{enrollment: enrollment, pairing: pairing}
}

func (h *Handler) CreateEnrollmentCode(ctx context.Context, _ *connect.Request[pb.CreateEnrollmentCodeRequest]) (*connect.Response[pb.CreateEnrollmentCodeResponse], error) {
	info, err := h.requireAdminSession(ctx)
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
	info, err := h.requireAdminSession(ctx)
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
			IdentityFingerprint: fleetnodeenrollment.IdentityFingerprint(n.IdentityPubkey),
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
	info, err := h.requireAdminSession(ctx)
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
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.enrollment.RevokeFleetNode(ctx, req.Msg.GetFleetNodeId(), info.OrganizationID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevokeFleetNodeResponse{}), nil
}

func (h *Handler) PairDeviceToFleetNode(ctx context.Context, req *connect.Request[pb.PairDeviceToFleetNodeRequest]) (*connect.Response[pb.PairDeviceToFleetNodeResponse], error) {
	info, err := h.requireAdminSession(ctx)
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
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.pairing.UnpairDevice(ctx, req.Msg.GetDeviceId(), info.OrganizationID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UnpairDeviceResponse{}), nil
}

func (h *Handler) ListFleetNodeDevices(ctx context.Context, req *connect.Request[pb.ListFleetNodeDevicesRequest]) (*connect.Response[pb.ListFleetNodeDevicesResponse], error) {
	info, err := h.requireAdminSession(ctx)
	if err != nil {
		return nil, err
	}
	var pairs []fleetnodepairing.FleetNodeDevice
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

func (h *Handler) requireAdminSession(ctx context.Context) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	if info.Role != domainAuth.SuperAdminRoleName && info.Role != domainAuth.AdminRoleName {
		return nil, fleeterror.NewForbiddenError("only admins can manage fleet nodes")
	}
	return info, nil
}

// AWAITING_CONFIRMATION lives only on pending_enrollment, so a PENDING fleet
// node whose pending row is AWAITING_CONFIRMATION surfaces as such instead.
func deriveDisplayStatus(n fleetnodeenrollment.FleetNodeListing) pb.FleetNodeEnrollmentStatus {
	switch n.EnrollmentStatus {
	case fleetnodeenrollment.FleetNodeStatusPending:
		if n.PendingEnrollmentStatus == fleetnodeenrollment.StatusAwaitingConfirmation {
			return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_AWAITING_CONFIRMATION
		}
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_PENDING
	case fleetnodeenrollment.FleetNodeStatusConfirmed:
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_CONFIRMED
	case fleetnodeenrollment.FleetNodeStatusRevoked:
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_REVOKED
	}
	return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_UNSPECIFIED
}
