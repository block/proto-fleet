package invitation

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"connectrpc.com/connect"
	authpb "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/invitation/v1"
	"github.com/block/proto-fleet/server/generated/grpc/invitation/v1/invitationv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	hostedhandler "github.com/block/proto-fleet/server/internal/handlers/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Handler issues team invitations and redeems codes of either kind. Every
// public preview and redemption request is charged once against the shared
// per-client invitation rate limit before the code is resolved.
type Handler struct {
	accounts *domain.InvitationService
	teams    *domain.TeamInvitationService
}

var _ invitationv1connect.InvitationServiceHandler = (*Handler)(nil)

func NewHandler(accounts *domain.InvitationService, teams *domain.TeamInvitationService) *Handler {
	return &Handler{accounts: accounts, teams: teams}
}

func (h *Handler) CreateTeamInvitation(ctx context.Context, req *connect.Request[pb.CreateTeamInvitationRequest]) (*connect.Response[pb.CreateTeamInvitationResponse], error) {
	info, err := requireTeamManager(ctx)
	if err != nil {
		return nil, err
	}
	issued, err := h.teams.Create(ctx, info.UserID, info.OrganizationID, req.Msg.RoleId, time.Duration(req.Msg.ExpiresInSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateTeamInvitationResponse{Invitation: teamInvitationDTO(issued.Invitation), Token: issued.Token}), nil
}

func (h *Handler) ListTeamInvitations(ctx context.Context, req *connect.Request[pb.ListTeamInvitationsRequest]) (*connect.Response[pb.ListTeamInvitationsResponse], error) {
	info, err := requireTeamManager(ctx)
	if err != nil {
		return nil, err
	}
	rows, next, err := h.teams.List(ctx, info.OrganizationID, req.Msg.PageSize, req.Msg.PageToken)
	if err != nil {
		return nil, err
	}
	invitations := make([]*pb.TeamInvitation, 0, len(rows))
	for _, row := range rows {
		invitations = append(invitations, teamInvitationDTO(row))
	}
	return connect.NewResponse(&pb.ListTeamInvitationsResponse{Invitations: invitations, NextPageToken: next}), nil
}

func (h *Handler) RevokeTeamInvitation(ctx context.Context, req *connect.Request[pb.RevokeTeamInvitationRequest]) (*connect.Response[pb.RevokeTeamInvitationResponse], error) {
	info, err := requireTeamManager(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.teams.Revoke(ctx, info.UserID, info.OrganizationID, req.Msg.InvitationId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevokeTeamInvitationResponse{}), nil
}

func (h *Handler) DeleteTeamInvitation(ctx context.Context, req *connect.Request[pb.DeleteTeamInvitationRequest]) (*connect.Response[pb.DeleteTeamInvitationResponse], error) {
	info, err := requireTeamManager(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.teams.Delete(ctx, info.UserID, info.OrganizationID, req.Msg.InvitationId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteTeamInvitationResponse{}), nil
}

func (h *Handler) PreviewInvitation(ctx context.Context, req *connect.Request[pb.PreviewInvitationRequest]) (*connect.Response[pb.PreviewInvitationResponse], error) {
	if err := h.checkClient(ctx); err != nil {
		return nil, err
	}
	team, err := h.teams.Owns(ctx, req.Msg.Token)
	if err != nil {
		return nil, err
	}
	if team {
		preview, err := h.teams.Preview(ctx, req.Msg.Token)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&pb.PreviewInvitationResponse{
			ExpiresAt: timestamppb.New(preview.ExpiresAt),
			Target: &pb.PreviewInvitationResponse_Team{Team: &pb.TeamInvitationPreview{
				OrganizationName: preview.OrganizationName, RoleName: preview.RoleName,
			}},
		}), nil
	}
	preview, err := h.accounts.Preview(ctx, req.Msg.Token)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.PreviewInvitationResponse{
		ExpiresAt: timestamppb.New(preview.ExpiresAt),
		Target: &pb.PreviewInvitationResponse_Account{Account: &pb.AccountInvitationPreview{
			AccountName: preview.AccountName, Role: hostedhandler.RoleDTO(preview.Role),
		}},
	}), nil
}

func (h *Handler) AcceptInvitation(ctx context.Context, req *connect.Request[pb.AcceptInvitationRequest]) (*connect.Response[pb.AcceptInvitationResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.checkClient(ctx); err != nil {
		return nil, err
	}
	team, err := h.teams.Owns(ctx, req.Msg.Token)
	if err != nil {
		return nil, err
	}
	if team {
		member, err := h.teams.Accept(ctx, req.Msg.Token, info.UserID, info.OrganizationID)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&pb.AcceptInvitationResponse{Target: &pb.AcceptInvitationResponse_Team{Team: joinedTeam(member)}}), nil
	}
	accountID, err := h.accounts.Accept(ctx, req.Msg.Token, info.UserID, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.AcceptInvitationResponse{Target: &pb.AcceptInvitationResponse_Account{Account: &pb.JoinedAccount{AccountId: accountID}}}), nil
}

func (h *Handler) RegisterInvitedUser(ctx context.Context, req *connect.Request[pb.RegisterInvitedUserRequest]) (*connect.Response[pb.RegisterInvitedUserResponse], error) {
	if err := h.checkClient(ctx); err != nil {
		return nil, err
	}
	team, err := h.teams.Owns(ctx, req.Msg.Token)
	if err != nil {
		return nil, err
	}
	ipAddress := middleware.ClientIP(ctx)
	userAgent := req.Header().Get("User-Agent")
	var response *connect.Response[pb.RegisterInvitedUserResponse]
	if team {
		registered, err := h.teams.Register(ctx, req.Msg.Token, req.Msg.Username, req.Msg.Password, userAgent, ipAddress)
		if err != nil {
			return nil, err
		}
		response = connect.NewResponse(&pb.RegisterInvitedUserResponse{Target: &pb.RegisterInvitedUserResponse_Team{Team: joinedTeam(registered.Member)}})
		response.Header().Set("Set-Cookie", registered.Cookie.String())
		return response, nil
	}
	registered, err := h.accounts.Register(ctx, req.Msg.Token, req.Msg.Username, req.Msg.Password, userAgent, ipAddress)
	if err != nil {
		return nil, err
	}
	response = connect.NewResponse(&pb.RegisterInvitedUserResponse{Target: &pb.RegisterInvitedUserResponse_Account{Account: &pb.JoinedAccount{AccountId: registered.AccountID}}})
	response.Header().Set("Set-Cookie", registered.Cookie.String())
	return response, nil
}

func (h *Handler) checkClient(ctx context.Context) error {
	ipAddress := middleware.ClientIP(ctx)
	if ipAddress == "" {
		return fleeterror.NewFailedPreconditionError("invitation client address unavailable")
	}
	return h.accounts.CheckClientRateLimit(ctx, ipAddress)
}

func requireTeamManager(ctx context.Context) (*session.Info, error) {
	info, err := middleware.RequirePermission(ctx, authz.PermUserManage, authz.ResourceContext{})
	if err != nil {
		return nil, err
	}
	if info.AuthMethod == session.AuthMethodAPIKey {
		return nil, fleeterror.NewForbiddenError("team invitations require session authentication")
	}
	return info, nil
}

func sessionPrincipal(ctx context.Context) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil || info.UserID <= 0 || info.OrganizationID <= 0 {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	if info.AuthMethod == session.AuthMethodAPIKey {
		return nil, fleeterror.NewForbiddenError("invitation acceptance requires session authentication")
	}
	return info, nil
}

func joinedTeam(member domain.TeamMember) *pb.JoinedTeam {
	return &pb.JoinedTeam{UserInfo: &authpb.UserInfo{
		UserId: member.UserID, Username: member.Username, Role: member.RoleName,
		RequiresPasswordChange: member.RequiresPasswordChange, Permissions: member.Permissions,
	}}
}

func teamInvitationDTO(i domain.TeamInvitation) *pb.TeamInvitation {
	return &pb.TeamInvitation{
		InvitationId: i.InvitationID, RoleId: strconv.FormatInt(i.RoleID, 10), RoleName: i.RoleName,
		Status: teamInvitationStatus(i), InviterUsername: i.InviterUsername,
		CreatedAt: timestamppb.New(i.CreatedAt), ExpiresAt: timestamppb.New(i.ExpiresAt),
		RevokedAt: nullableTimestamp(i.RevokedAt),
	}
}

func teamInvitationStatus(i domain.TeamInvitation) pb.TeamInvitationStatus {
	switch {
	case i.RevokedAt.Valid:
		return pb.TeamInvitationStatus_TEAM_INVITATION_STATUS_REVOKED
	case !time.Now().Before(i.ExpiresAt):
		return pb.TeamInvitationStatus_TEAM_INVITATION_STATUS_EXPIRED
	case i.Available:
		return pb.TeamInvitationStatus_TEAM_INVITATION_STATUS_PENDING
	default:
		return pb.TeamInvitationStatus_TEAM_INVITATION_STATUS_UNAVAILABLE
	}
}

func nullableTimestamp(t sql.NullTime) *timestamppb.Timestamp {
	if !t.Valid {
		return nil
	}
	return timestamppb.New(t.Time)
}
