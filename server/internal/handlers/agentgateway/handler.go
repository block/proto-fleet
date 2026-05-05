package agentgateway

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/agentgateway/v1/agentgatewayv1connect"
	"github.com/block/proto-fleet/server/internal/domain/agentauth"
	"github.com/block/proto-fleet/server/internal/domain/agentenrollment"
)

type Handler struct {
	agentgatewayv1connect.UnimplementedAgentGatewayServiceHandler

	enrollment *agentenrollment.Service
	auth       *agentauth.Service
}

var _ agentgatewayv1connect.AgentGatewayServiceHandler = &Handler{}

func NewHandler(enrollment *agentenrollment.Service, auth *agentauth.Service) *Handler {
	return &Handler{enrollment: enrollment, auth: auth}
}

func (h *Handler) Register(ctx context.Context, req *connect.Request[pb.RegisterRequest]) (*connect.Response[pb.RegisterResponse], error) {
	agent, _, err := h.enrollment.RegisterAgent(ctx, req.Msg.GetEnrollmentToken(), req.Msg.GetName(), req.Msg.GetIdentityPubkey(), req.Msg.GetMinerSigningPubkey())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RegisterResponse{
		AgentId:             agent.ID,
		EnrollmentStatus:    pb.EnrollmentStatus_ENROLLMENT_STATUS_PENDING,
		IdentityFingerprint: agentenrollment.IdentityFingerprint(agent.IdentityPubkey),
	}), nil
}

func (h *Handler) BeginAuthHandshake(ctx context.Context, req *connect.Request[pb.BeginAuthHandshakeRequest]) (*connect.Response[pb.BeginAuthHandshakeResponse], error) {
	challenge, expiresAt, err := h.auth.BeginHandshake(ctx, req.Msg.GetApiKey(), req.Msg.GetIdentityPubkey())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.BeginAuthHandshakeResponse{
		Challenge: challenge,
		ExpiresAt: timestamppb.New(expiresAt),
	}), nil
}

func (h *Handler) CompleteAuthHandshake(ctx context.Context, req *connect.Request[pb.CompleteAuthHandshakeRequest]) (*connect.Response[pb.CompleteAuthHandshakeResponse], error) {
	token, expiresAt, err := h.auth.CompleteHandshake(ctx, req.Msg.GetChallenge(), req.Msg.GetSignature())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CompleteAuthHandshakeResponse{
		SessionToken: token,
		ExpiresAt:    timestamppb.New(expiresAt),
	}), nil
}
