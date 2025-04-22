package auth

import (
	"context"
	"fmt"
	"github.com/btc-mining/proto-fleet/server/internal/domain/auth"

	"connectrpc.com/connect"
	pb "github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/btc-mining/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
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
	token, err := s.authSvc.AuthenticateUser(ctx, &auth.AuthenticateUserRequest{
		Username: req.Msg.Username,
		Password: req.Msg.Password,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pb.AuthenticateResponse{Token: fmt.Sprintf("%v", token)}), nil
}
