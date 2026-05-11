package agentadmin

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodeadmin/v1/fleetnodeadminv1connect"
	"github.com/block/proto-fleet/server/internal/domain/agentenrollment"
	domainAuth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

type Handler struct {
	fleetnodeadminv1connect.UnimplementedFleetNodeAdminServiceHandler

	enrollment *agentenrollment.Service
}

var _ fleetnodeadminv1connect.FleetNodeAdminServiceHandler = &Handler{}

func NewHandler(enrollment *agentenrollment.Service) *Handler {
	return &Handler{enrollment: enrollment}
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
	agents, err := h.enrollment.ListAgents(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListFleetNodesResponse{FleetNodes: make([]*pb.FleetNodeSummary, 0, len(agents))}
	for _, a := range agents {
		summary := &pb.FleetNodeSummary{
			FleetNodeId:         a.ID,
			Name:                a.Name,
			EnrollmentStatus:    deriveDisplayStatus(a),
			IdentityFingerprint: agentenrollment.IdentityFingerprint(a.IdentityPubkey),
			CreatedAt:           timestamppb.New(a.CreatedAt),
		}
		if a.LastSeenAt != nil {
			summary.LastSeenAt = timestamppb.New(*a.LastSeenAt)
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
	if err := h.enrollment.RevokeAgent(ctx, req.Msg.GetFleetNodeId(), info.OrganizationID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevokeFleetNodeResponse{}), nil
}

func (h *Handler) requireAdminSession(ctx context.Context) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	if info.Role != domainAuth.SuperAdminRoleName && info.Role != domainAuth.AdminRoleName {
		return nil, fleeterror.NewForbiddenError("only admins can manage agents")
	}
	return info, nil
}

// AWAITING_CONFIRMATION lives only on pending_enrollment, so a PENDING agent
// whose pending row is AWAITING_CONFIRMATION surfaces as such instead.
func deriveDisplayStatus(a agentenrollment.AgentListing) pb.FleetNodeEnrollmentStatus {
	switch a.EnrollmentStatus {
	case agentenrollment.AgentStatusPending:
		if a.PendingEnrollmentStatus == agentenrollment.StatusAwaitingConfirmation {
			return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_AWAITING_CONFIRMATION
		}
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_PENDING
	case agentenrollment.AgentStatusConfirmed:
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_CONFIRMED
	case agentenrollment.AgentStatusRevoked:
		return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_REVOKED
	}
	return pb.FleetNodeEnrollmentStatus_FLEET_NODE_ENROLLMENT_STATUS_UNSPECIFIED
}
