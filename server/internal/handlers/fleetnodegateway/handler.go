package fleetnodegateway

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeauth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnodeenrollment"
)

type Handler struct {
	fleetnodegatewayv1connect.UnimplementedFleetNodeGatewayServiceHandler

	enrollment *fleetnodeenrollment.Service
	auth       *fleetnodeauth.Service
}

var _ fleetnodegatewayv1connect.FleetNodeGatewayServiceHandler = &Handler{}

func NewHandler(enrollment *fleetnodeenrollment.Service, auth *fleetnodeauth.Service) *Handler {
	return &Handler{enrollment: enrollment, auth: auth}
}

func (h *Handler) Register(ctx context.Context, req *connect.Request[pb.RegisterRequest]) (*connect.Response[pb.RegisterResponse], error) {
	agent, _, err := h.enrollment.RegisterFleetNode(ctx, req.Msg.GetEnrollmentToken(), req.Msg.GetName(), req.Msg.GetIdentityPubkey(), req.Msg.GetMinerSigningPubkey())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RegisterResponse{
		FleetNodeId:         agent.ID,
		EnrollmentStatus:    pb.EnrollmentStatus_ENROLLMENT_STATUS_PENDING,
		IdentityFingerprint: fleetnodeenrollment.IdentityFingerprint(agent.IdentityPubkey),
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

func (h *Handler) UploadHeartbeat(ctx context.Context, _ *connect.Request[pb.UploadHeartbeatRequest]) (*connect.Response[pb.UploadHeartbeatResponse], error) {
	subject, err := fleetnodeauth.GetSubject(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := h.enrollment.UpdateLastSeen(ctx, subject.FleetNodeID, subject.OrgID, now); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UploadHeartbeatResponse{
		ReceivedAt: timestamppb.New(now),
	}), nil
}
