package agentadmin

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/agentadmin/v1"
	"github.com/block/proto-fleet/server/generated/grpc/agentadmin/v1/agentadminv1connect"
	"github.com/block/proto-fleet/server/internal/domain/agentenrollment"
	"github.com/block/proto-fleet/server/internal/domain/session"
)

type Handler struct {
	agentadminv1connect.UnimplementedAgentAdminServiceHandler

	enrollment *agentenrollment.Service
}

var _ agentadminv1connect.AgentAdminServiceHandler = &Handler{}

func NewHandler(enrollment *agentenrollment.Service) *Handler {
	return &Handler{enrollment: enrollment}
}

func (h *Handler) CreateEnrollmentCode(ctx context.Context, _ *connect.Request[pb.CreateEnrollmentCodeRequest]) (*connect.Response[pb.CreateEnrollmentCodeResponse], error) {
	info, err := session.GetInfo(ctx)
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

func (h *Handler) ListAgents(ctx context.Context, _ *connect.Request[pb.ListAgentsRequest]) (*connect.Response[pb.ListAgentsResponse], error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	agents, err := h.enrollment.ListAgents(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	resp := &pb.ListAgentsResponse{Agents: make([]*pb.AgentSummary, 0, len(agents))}
	for _, a := range agents {
		summary := &pb.AgentSummary{
			AgentId:             a.ID,
			Name:                a.Name,
			EnrollmentStatus:    enrollmentStatusToProto(a.EnrollmentStatus),
			IdentityFingerprint: agentenrollment.IdentityFingerprint(a.IdentityPubkey),
			CreatedAt:           timestamppb.New(a.CreatedAt),
		}
		if a.LastSeenAt != nil {
			summary.LastSeenAt = timestamppb.New(*a.LastSeenAt)
		}
		resp.Agents = append(resp.Agents, summary)
	}
	return connect.NewResponse(resp), nil
}

func (h *Handler) ConfirmAgent(ctx context.Context, req *connect.Request[pb.ConfirmAgentRequest]) (*connect.Response[pb.ConfirmAgentResponse], error) {
	info, err := session.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	apiKey, expiresAt, err := h.enrollment.Confirm(ctx, req.Msg.GetAgentId(), info.OrganizationID)
	if err != nil {
		return nil, err
	}
	resp := &pb.ConfirmAgentResponse{ApiKey: apiKey}
	if !expiresAt.IsZero() {
		resp.ExpiresAt = timestamppb.New(expiresAt)
	}
	return connect.NewResponse(resp), nil
}

var agentStatusToProto = map[agentenrollment.AgentStatus]pb.AgentEnrollmentStatus{
	agentenrollment.AgentStatusPending:   pb.AgentEnrollmentStatus_AGENT_ENROLLMENT_STATUS_PENDING,
	agentenrollment.AgentStatusConfirmed: pb.AgentEnrollmentStatus_AGENT_ENROLLMENT_STATUS_CONFIRMED,
	agentenrollment.AgentStatusRevoked:   pb.AgentEnrollmentStatus_AGENT_ENROLLMENT_STATUS_REVOKED,
}

func enrollmentStatusToProto(status agentenrollment.AgentStatus) pb.AgentEnrollmentStatus {
	if v, ok := agentStatusToProto[status]; ok {
		return v
	}
	return pb.AgentEnrollmentStatus_AGENT_ENROLLMENT_STATUS_UNSPECIFIED
}
