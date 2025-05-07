package auth

import (
	"context"
	"errors"

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
	resp, err := s.authSvc.AuthenticateUser(ctx, req.Msg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(resp), nil
}

// UpdatePassword updates the password of the currently logged-in user
func (s *Handler) UpdatePassword(ctx context.Context, r *connect.Request[pb.UpdatePasswordRequest]) (*connect.Response[pb.UpdatePasswordResponse], error) {
	err := s.authSvc.UpdatePassword(ctx, r.Msg)

	if errors.Is(err, auth.ErrForbidden) {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	} else if errors.Is(err, auth.ErrInvalidInput) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	} else if err != nil {
		return nil, connect.NewError(connect.CodeUnknown, err)
	}
	return connect.NewResponse(&pb.UpdatePasswordResponse{}), nil
}
