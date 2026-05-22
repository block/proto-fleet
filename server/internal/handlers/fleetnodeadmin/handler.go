package fleetnodeadmin

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeenrollment"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	fleetnodeadminv1connect.UnimplementedFleetNodeAdminServiceHandler

	enrollment *fleetnodeenrollment.Service
}

var _ fleetnodeadminv1connect.FleetNodeAdminServiceHandler = &Handler{}

func NewHandler(enrollment *fleetnodeenrollment.Service) *Handler {
	return &Handler{enrollment: enrollment}
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
