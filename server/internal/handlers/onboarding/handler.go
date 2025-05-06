package onboarding

import (
	"context"

	"github.com/btc-mining/proto-fleet/server/internal/domain/auth"
	"github.com/btc-mining/proto-fleet/server/internal/domain/onboarding"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"
)

// Handler handles authentication requests
type Handler struct {
	authSvc       *auth.Service
	onboardingSvc *onboarding.Service
}

var _ onboardingv1connect.OnboardingServiceHandler = &Handler{}

// NewHandler initializes Handler
func NewHandler(authSvc *auth.Service, onboardingSvc *onboarding.Service) *Handler {
	return &Handler{authSvc: authSvc, onboardingSvc: onboardingSvc}
}

// CreateAdminLogin authenticates a user and returns a JWT token
func (s *Handler) CreateAdminLogin(ctx context.Context, r *connect.Request[pb.CreateAdminLoginRequest]) (*connect.Response[pb.CreateAdminLoginResponse], error) {
	resp, err := s.authSvc.CreateAdminUser(ctx, r.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

func (s *Handler) GetFleetOnboardingStatus(ctx context.Context, _ *connect.Request[pb.GetFleetOnboardingStatusRequest]) (*connect.Response[pb.GetFleetOnboardingStatusResponse], error) {
	status, err := s.onboardingSvc.GetFleetOnboardingStatus(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pb.GetFleetOnboardingStatusResponse{
		Status: status,
	}), nil
}
