package auth

import (
	"context"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	pb "github.com/proto-at-block/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/proto-at-block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	"github.com/proto-at-block/proto-fleet/server/internal/domain/auth"
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

// Authenticate authenticates a user and creates a session.
// The session cookie is set in the response headers.
func (s *Handler) Authenticate(ctx context.Context, req *connect.Request[pb.AuthenticateRequest]) (*connect.Response[pb.AuthenticateResponse], error) {
	userAgent := req.Header().Get("User-Agent")
	ipAddress := extractIPAddress(req.Header())

	resp, cookie, err := s.authSvc.AuthenticateUser(ctx, req.Msg, userAgent, ipAddress)
	if err != nil {
		return nil, err
	}

	response := connect.NewResponse(resp)
	response.Header().Set("Set-Cookie", cookie.String())

	return response, nil
}

// Logout invalidates the current session.
func (s *Handler) Logout(ctx context.Context, _ *connect.Request[pb.LogoutRequest]) (*connect.Response[pb.LogoutResponse], error) {
	cookie, err := s.authSvc.Logout(ctx)
	if err != nil {
		return nil, err
	}

	response := connect.NewResponse(&pb.LogoutResponse{})
	response.Header().Set("Set-Cookie", cookie.String())

	return response, nil
}

// extractIPAddress extracts the client IP from request headers.
// Returns empty string if no proxy headers are present (e.g., direct connections).
// Note: We don't use RemoteAddr because Connect RPC doesn't expose it directly,
// and the server may be behind a reverse proxy anyway.
func extractIPAddress(header http.Header) string {
	// Check X-Forwarded-For first (for reverse proxy setups)
	if xff := header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain (original client IP)
		if idx := strings.Index(xff, ","); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	// Fall back to X-Real-IP
	if xri := header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	return ""
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

func (s *Handler) GetUserAuditInfo(ctx context.Context, _ *connect.Request[pb.GetUserAuditInfoRequest]) (*connect.Response[pb.GetUserAuditInfoResponse], error) {
	resp, err := s.authSvc.GetUserAuditInfo(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

// CreateUser creates a new user with a temporary password (Super Admin only)
func (s *Handler) CreateUser(ctx context.Context, req *connect.Request[pb.CreateUserRequest]) (*connect.Response[pb.CreateUserResponse], error) {
	resp, err := s.authSvc.CreateUser(ctx, req.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

// ListUsers returns all users in the organization (Super Admin only)
func (s *Handler) ListUsers(ctx context.Context, _ *connect.Request[pb.ListUsersRequest]) (*connect.Response[pb.ListUsersResponse], error) {
	resp, err := s.authSvc.ListUsers(ctx)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

// ResetUserPassword generates a new temporary password for a user (Super Admin only)
func (s *Handler) ResetUserPassword(ctx context.Context, req *connect.Request[pb.ResetUserPasswordRequest]) (*connect.Response[pb.ResetUserPasswordResponse], error) {
	resp, err := s.authSvc.ResetUserPassword(ctx, req.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}

// DeactivateUser soft-deletes a user (Super Admin only)
func (s *Handler) DeactivateUser(ctx context.Context, req *connect.Request[pb.DeactivateUserRequest]) (*connect.Response[pb.DeactivateUserResponse], error) {
	resp, err := s.authSvc.DeactivateUser(ctx, req.Msg)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(resp), nil
}
