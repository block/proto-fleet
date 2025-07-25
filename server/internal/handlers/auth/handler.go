package auth

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/btc-mining/proto-fleet/server/internal/domain/auth"
)

// Handler handles authentication requests
type Handler struct {
	authSvc *auth.Service
}

var _ authv1connect.AuthServiceHandler = &Handler{}

// NewHandler initializes AuthService
func NewHandler(authSvc *auth.Service) *Handler {
	return &Handler{authSvc: authSvc}
}

// Authenticate authenticates a user and returns a JWT token
func (s *Handler) Authenticate(ctx context.Context, req *connect.Request[pb.AuthenticateRequest]) (*connect.Response[pb.AuthenticateResponse], error) {
	resp, err := s.authSvc.AuthenticateUser(ctx, req.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

// UpdatePassword updates the password of the currently logged-in user
func (s *Handler) UpdatePassword(ctx context.Context, r *connect.Request[pb.UpdatePasswordRequest]) (*connect.Response[pb.UpdatePasswordResponse], error) {
	err := s.authSvc.UpdatePassword(ctx, r.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.UpdatePasswordResponse{}), nil
}

func (s *Handler) UpdateUsername(ctx context.Context, r *connect.Request[pb.UpdateUsernameRequest]) (*connect.Response[pb.UpdateUsernameResponse], error) {
	err := s.authSvc.UpdateUsername(ctx, r.Msg.Username)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&pb.UpdateUsernameResponse{}), nil
}
