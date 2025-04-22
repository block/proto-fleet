package onboarding

import (
	"context"
	"github.com/btc-mining/miner-firmware/fleet/internal/domain/auth"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/miner-firmware/fleet/generated/grpc/onboarding/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/onboarding/v1/onboardingv1connect"
)

// Handler handles authentication requests
type Handler struct {
	authSvc *auth.Service
}

var _ onboardingv1connect.OnboardingServiceHandler = &Handler{}

// NewHandler initializes Handler
func NewHandler(authSvc *auth.Service) *Handler {
	return &Handler{authSvc: authSvc}
}

// CreateAdminLogin authenticates a user and returns a JWT token
func (s *Handler) CreateAdminLogin(ctx context.Context, r *connect.Request[pb.CreateAdminLoginRequest]) (*connect.Response[pb.CreateAdminLoginResponse], error) {
	userID, err := s.authSvc.CreateAdminUser(ctx, &auth.CreateAdminUserRequest{
		Username: r.Msg.Username,
		Password: r.Msg.Password,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pb.CreateAdminLoginResponse{
		UserId: string(userID),
	}), nil
}
