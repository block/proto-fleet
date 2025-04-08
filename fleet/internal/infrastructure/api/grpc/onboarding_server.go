package grpc

import (
	"context"

	"connectrpc.com/connect"
	authv1 "github.com/btc-mining/miner-firmware/fleet/generated/grpc/onboarding/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/onboarding/v1/onboardingv1connect"
	"github.com/btc-mining/miner-firmware/fleet/internal/application"
)

// OnboardingServer handles authentication requests
type OnboardingServer struct {
	authUseCases *application.AuthUseCases
}

var _ onboardingv1connect.OnboardingServiceHandler = &OnboardingServer{}

// NewOnboardingServer initializes OnboardingServer
func NewOnboardingServer(authSvc *application.AuthUseCases) *OnboardingServer {
	return &OnboardingServer{authUseCases: authSvc}
}

// CreateAdminLogin authenticates a user and returns a JWT token
func (s *OnboardingServer) CreateAdminLogin(ctx context.Context, req *connect.Request[authv1.CreateAdminLoginRequest]) (*connect.Response[authv1.CreateAdminLoginResponse], error) {
	userID, err := s.authUseCases.CreateAdminUser(ctx, req.Msg.Username, req.Msg.Password)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&authv1.CreateAdminLoginResponse{
		UserId: string(userID),
	}), nil
}
