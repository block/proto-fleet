package grpc

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	authv1 "github.com/btc-mining/miner-firmware/fleet/generated/grpc/auth/v1"
	"github.com/btc-mining/miner-firmware/fleet/generated/grpc/auth/v1/authv1connect"
	"github.com/btc-mining/miner-firmware/fleet/internal/application"
)

// AuthServer handles authentication requests
type AuthServer struct {
	authUseCases *application.AuthUseCases
}

var _ authv1connect.AuthServiceHandler = &AuthServer{}

// NewAuthServer initializes AuthService
func NewAuthServer(authSvc *application.AuthUseCases) *AuthServer {
	return &AuthServer{authUseCases: authSvc}
}

// Authenticate authenticates a user and returns a JWT token
func (s *AuthServer) Authenticate(ctx context.Context, req *connect.Request[authv1.AuthenticateRequest]) (*connect.Response[authv1.AuthenticateResponse], error) {
	token, err := s.authUseCases.AuthenticateUser(ctx, req.Msg.Username, req.Msg.Password)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&authv1.AuthenticateResponse{Token: fmt.Sprintf("%v", token)}), nil
}
