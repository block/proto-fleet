package onboarding

import (
	"context"

	"github.com/block/proto-fleet/server/generated/grpc/onboarding/v1/onboardingv1connect"

	"github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/onboarding"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/onboarding/v1"
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
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

func (s *Handler) GetFleetInitStatus(ctx context.Context, _ *connect.Request[pb.GetFleetInitStatusRequest]) (*connect.Response[pb.GetFleetInitStatusResponse], error) {
	status, err := s.onboardingSvc.GetFleetInitStatus(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.GetFleetInitStatusResponse{
		Status: status,
	}), nil
}

func (s *Handler) GetFleetOnboardingStatus(ctx context.Context, _ *connect.Request[pb.GetFleetOnboardingStatusRequest]) (*connect.Response[pb.GetFleetOnboardingStatusResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authz.PermFleetRead, authz.ResourceContext{}); err != nil {
		return nil, err
	}
	status, err := s.onboardingSvc.GetFleetOnboardingStatus(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.GetFleetOnboardingStatusResponse{
		Status: status,
	}), nil
}
