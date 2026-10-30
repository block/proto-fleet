package hostedaccounts

import (
	"context"
	"database/sql"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/grpc/hosted/v1/hostedv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct {
	accounts    *domain.Service
	invitations *domain.InvitationService
	keys        *domain.KeyService
	pools       *domain.PoolService
	users       interfaces.UserStore
	permissions *authz.PermissionResolver
}

var _ hostedv1connect.HostedAccountServiceHandler = (*Handler)(nil)

func NewHandler(accounts *domain.Service, invitations *domain.InvitationService, keys *domain.KeyService, pools *domain.PoolService, users interfaces.UserStore, permissions *authz.PermissionResolver) *Handler {
	return &Handler{accounts: accounts, invitations: invitations, keys: keys, pools: pools, users: users, permissions: permissions}
}

func principal(ctx context.Context, accountID string) (*session.Info, error) {
	if err := domain.RequireRequestAccount(ctx, accountID); err != nil {
		return nil, err
	}
	return sessionPrincipal(ctx)
}
func readPrincipal(ctx context.Context, accountID string) (*session.Info, error) {
	if err := domain.RequireRequestAccount(ctx, accountID); err != nil {
		return nil, err
	}
	info, err := session.GetInfo(ctx)
	if err != nil || info.UserID <= 0 || info.OrganizationID <= 0 {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	if info.AuthMethod == session.AuthMethodAPIKey && !info.AccountKey {
		return nil, fleeterror.NewForbiddenError("organization keys cannot access customer accounts")
	}
	return info, nil
}
func sessionPrincipal(ctx context.Context) (*session.Info, error) {
	info, err := session.GetInfo(ctx)
	if err != nil || info.UserID <= 0 || info.OrganizationID <= 0 {
		return nil, fleeterror.NewUnauthenticatedError("authentication required")
	}
	if info.AuthMethod == session.AuthMethodAPIKey {
		return nil, fleeterror.NewForbiddenError("account administration requires session authentication")
	}
	return info, nil
}

func (h *Handler) ListAccounts(ctx context.Context, _ *connect.Request[pb.ListAccountsRequest]) (*connect.Response[pb.ListAccountsResponse], error) {
	info, err := sessionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.accounts.ListAccounts(ctx, info.UserID, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	permissions, err := h.permissions.LoadEffective(ctx, info.UserID, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	response := &pb.ListAccountsResponse{HostAccess: permissions.HasAnyPermission()}
	for _, row := range rows {
		response.Accounts = append(response.Accounts, accountDTO(row))
	}
	return connect.NewResponse(response), nil
}
func (h *Handler) GetAccount(ctx context.Context, req *connect.Request[pb.GetAccountRequest]) (*connect.Response[pb.GetAccountResponse], error) {
	info, err := readPrincipal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	account, err := h.accounts.GetAccount(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	sites, err := h.accounts.SitePolicy(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetAccountResponse{Account: accountDTO(account), SitePolicy: &pb.SitePolicy{Restricted: sites.Restricted, SiteIds: sites.SiteIDs}}), nil
}
func (h *Handler) ListChildren(ctx context.Context, req *connect.Request[pb.ListChildrenRequest]) (*connect.Response[pb.ListChildrenResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	rows, err := h.accounts.ListChildren(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	response := &pb.ListChildrenResponse{}
	for _, row := range rows {
		response.Accounts = append(response.Accounts, accountDTO(row))
	}
	return connect.NewResponse(response), nil
}
func (h *Handler) CreateAccount(ctx context.Context, req *connect.Request[pb.CreateAccountRequest]) (*connect.Response[pb.CreateAccountResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	capabilities, err := capabilitiesDomain(req.Msg.Capabilities)
	if err != nil {
		return nil, err
	}
	var adminID int64
	if req.Msg.AdministratorUserId != nil {
		user, err := h.users.GetUserByExternalID(ctx, req.Msg.GetAdministratorUserId())
		if err != nil {
			return nil, fleeterror.NewInvalidArgumentError("invalid administrator user")
		}
		adminID = user.ID
	}
	created, err := h.accounts.CreateChild(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.Name, adminID, capabilities)
	if err != nil {
		return nil, err
	}
	item := domain.AccountSummary{AccountID: created.AccountID, ParentAccountID: req.Msg.AccountId, Name: created.Name, Depth: created.Depth, Capabilities: domain.Capabilities(created.Capabilities), Revision: created.Revision}
	item.EffectiveCapabilities = created.Access.Capabilities
	item.Role = created.Access.Role
	return connect.NewResponse(&pb.CreateAccountResponse{Account: accountDTO(item)}), nil
}
func (h *Handler) ConfigureAccount(ctx context.Context, req *connect.Request[pb.ConfigureAccountRequest]) (*connect.Response[pb.ConfigureAccountResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	capabilities, err := capabilitiesDomain(req.Msg.Capabilities)
	if err != nil {
		return nil, err
	}
	if _, err := h.requireChild(ctx, info, req.Msg.AccountId, req.Msg.ChildAccountId); err != nil {
		return nil, err
	}
	if err = h.accounts.ConfigureChild(ctx, info.UserID, info.OrganizationID, req.Msg.ChildAccountId, capabilities, req.Msg.Suspended); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.ConfigureAccountResponse{}), nil
}
func (h *Handler) RenameAccount(ctx context.Context, req *connect.Request[pb.RenameAccountRequest]) (*connect.Response[pb.RenameAccountResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if err = h.accounts.Rename(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.Name); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RenameAccountResponse{}), nil
}
func (h *Handler) ListMembers(ctx context.Context, req *connect.Request[pb.ListMembersRequest]) (*connect.Response[pb.ListMembersResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	rows, err := h.accounts.ListMembers(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	response := &pb.ListMembersResponse{}
	for _, row := range rows {
		response.Members = append(response.Members, &pb.Member{UserId: row.UserID, Username: row.Username, Role: RoleDTO(row.Role), Revision: row.Revision, JoinedAt: timestamppb.New(row.JoinedAt)})
	}
	return connect.NewResponse(response), nil
}
func (h *Handler) SetMemberRole(ctx context.Context, req *connect.Request[pb.SetMemberRoleRequest]) (*connect.Response[pb.SetMemberRoleResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	memberID, err := h.accounts.FindMember(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.UserId)
	if err != nil {
		return nil, err
	}
	if err = h.accounts.SetMembership(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, memberID, roleDomain(req.Msg.Role)); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetMemberRoleResponse{}), nil
}
func (h *Handler) RevokeMember(ctx context.Context, req *connect.Request[pb.RevokeMemberRequest]) (*connect.Response[pb.RevokeMemberResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	memberID, err := h.accounts.FindMember(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.UserId)
	if err != nil {
		return nil, err
	}
	if err = h.accounts.RevokeMembership(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, memberID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevokeMemberResponse{}), nil
}
func (h *Handler) ListAncestorGrants(ctx context.Context, req *connect.Request[pb.ListAncestorGrantsRequest]) (*connect.Response[pb.ListAncestorGrantsResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	rows, err := h.accounts.ListAncestorGrants(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	response := &pb.ListAncestorGrantsResponse{}
	for _, row := range rows {
		response.Grants = append(response.Grants, &pb.AncestorGrant{AncestorAccountId: row.AncestorAccountID, Capabilities: uint64(row.Capabilities), Revision: row.Revision}) // #nosec G115 -- stored ancestor grant masks are constrained to 1..31.
	}
	return connect.NewResponse(response), nil
}
func (h *Handler) SetAncestorGrant(ctx context.Context, req *connect.Request[pb.SetAncestorGrantRequest]) (*connect.Response[pb.SetAncestorGrantResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	capabilities, err := capabilitiesDomain(req.Msg.Capabilities)
	if err != nil {
		return nil, err
	}
	if err = h.accounts.SetAncestorGrant(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.AncestorAccountId, capabilities); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetAncestorGrantResponse{}), nil
}
func (h *Handler) RevokeAncestorGrant(ctx context.Context, req *connect.Request[pb.RevokeAncestorGrantRequest]) (*connect.Response[pb.RevokeAncestorGrantResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if err = h.accounts.RevokeAncestorGrant(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.AncestorAccountId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevokeAncestorGrantResponse{}), nil
}
func (h *Handler) SetSitePolicy(ctx context.Context, req *connect.Request[pb.SetSitePolicyRequest]) (*connect.Response[pb.SetSitePolicyResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if req.Msg.SitePolicy == nil {
		return nil, fleeterror.NewInvalidArgumentError("site policy required")
	}
	ctx, target, err := h.policyTarget(ctx, info, req.Msg.AccountId, req.Msg.GetChildAccountId())
	if err != nil {
		return nil, err
	}
	if err = h.accounts.ConfigureSites(ctx, info.UserID, info.OrganizationID, target, req.Msg.SitePolicy.Restricted, req.Msg.SitePolicy.SiteIds); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.SetSitePolicyResponse{}), nil
}
func (h *Handler) GetSitePolicy(ctx context.Context, req *connect.Request[pb.GetSitePolicyRequest]) (*connect.Response[pb.GetSitePolicyResponse], error) {
	info, err := readPrincipal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	ctx, target, err := h.policyTarget(ctx, info, req.Msg.AccountId, req.Msg.GetChildAccountId())
	if err != nil {
		return nil, err
	}
	sites, err := h.accounts.SitePolicy(ctx, info.UserID, info.OrganizationID, target)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.GetSitePolicyResponse{SitePolicy: &pb.SitePolicy{Restricted: sites.Restricted, SiteIds: sites.SiteIDs}, EffectiveRestricted: sites.EffectiveRestricted, EffectiveSiteIds: sites.EffectiveSiteIDs}), nil
}
func (h *Handler) policyTarget(ctx context.Context, info *session.Info, accountID, childID string) (context.Context, string, error) {
	if childID == "" {
		return ctx, accountID, nil
	}
	// Naming a child explicitly selects the parent's delegated ceiling even
	// without a workspace header, and even if the caller is also a child member.
	ctx, err := domain.WithRequestAccountID(ctx, accountID)
	if err != nil {
		return ctx, "", err
	}
	target, err := h.requireChild(ctx, info, accountID, childID)
	return ctx, target, err
}

func (h *Handler) requireChild(ctx context.Context, info *session.Info, accountID, childID string) (string, error) {
	children, err := h.accounts.ListChildren(ctx, info.UserID, info.OrganizationID, accountID)
	if err != nil {
		return "", err
	}
	for _, child := range children {
		if child.AccountID == childID {
			return childID, nil
		}
	}
	return "", fleeterror.NewForbiddenError("account access denied")
}
func (h *Handler) ListInvitations(ctx context.Context, req *connect.Request[pb.ListInvitationsRequest]) (*connect.Response[pb.ListInvitationsResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	rows, next, err := h.invitations.List(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.PageSize, req.Msg.PageToken)
	if err != nil {
		return nil, err
	}
	response := &pb.ListInvitationsResponse{NextPageToken: next}
	for _, row := range rows {
		response.Invitations = append(response.Invitations, invitationDTO(row))
	}
	return connect.NewResponse(response), nil
}
func (h *Handler) CreateInvitation(ctx context.Context, req *connect.Request[pb.CreateInvitationRequest]) (*connect.Response[pb.CreateInvitationResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	issued, err := h.invitations.Create(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, roleDomain(req.Msg.Role), time.Duration(req.Msg.ExpiresInSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateInvitationResponse{Invitation: invitationDTO(issued.Invitation), Token: issued.Token}), nil
}
func (h *Handler) RevokeInvitation(ctx context.Context, req *connect.Request[pb.RevokeInvitationRequest]) (*connect.Response[pb.RevokeInvitationResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if err = h.invitations.Revoke(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.InvitationId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevokeInvitationResponse{}), nil
}

func accountDTO(a domain.AccountSummary) *pb.Account {
	item := &pb.Account{AccountId: a.AccountID, ParentAccountId: a.ParentAccountID, Name: a.Name, Depth: uint32(a.Depth), CapabilityCeiling: uint64(a.Capabilities), EffectiveCapabilities: uint64(a.EffectiveCapabilities), Role: RoleDTO(a.Role), Suspended: a.Suspended, Revision: a.Revision} // #nosec G115 -- account depth is constrained to 0..2 and capability masks to 0..31.
	if counts := a.ManagementSummary; counts != nil {
		item.ManagementSummary = &pb.ClientManagementSummary{
			MinerCount: counts.MinerCount, ModuleCount: counts.ModuleCount, HashboardCount: counts.HashboardCount,
			ContainerCount: counts.ContainerCount, LoginCount: counts.LoginCount,
		}
	}
	return item
}
func RoleDTO(role domain.Role) pb.AccountRole {
	switch role {
	case domain.RoleViewer:
		return pb.AccountRole_ACCOUNT_ROLE_VIEWER
	case domain.RolePoolOperator:
		return pb.AccountRole_ACCOUNT_ROLE_POOL_OPERATOR
	case domain.RoleAdministrator:
		return pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR
	default:
		return pb.AccountRole_ACCOUNT_ROLE_UNSPECIFIED
	}
}
func roleDomain(role pb.AccountRole) domain.Role {
	switch role {
	case pb.AccountRole_ACCOUNT_ROLE_UNSPECIFIED:
		return ""
	case pb.AccountRole_ACCOUNT_ROLE_VIEWER:
		return domain.RoleViewer
	case pb.AccountRole_ACCOUNT_ROLE_POOL_OPERATOR:
		return domain.RolePoolOperator
	case pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR:
		return domain.RoleAdministrator
	default:
		return ""
	}
}
func invitationDTO(i domain.Invitation) *pb.Invitation {
	return &pb.Invitation{InvitationId: i.InvitationID, Role: RoleDTO(domain.Role(i.Role)), ExpiresAt: timestamppb.New(i.ExpiresAt), RevokedAt: nullableTimestamp(i.RevokedAt), AcceptedAt: nullableTimestamp(i.AcceptedAt), CreatedAt: timestamppb.New(i.CreatedAt), Available: i.Available}
}
func nullableTimestamp(t sql.NullTime) *timestamppb.Timestamp {
	if !t.Valid {
		return nil
	}
	return timestamppb.New(t.Time)
}

func capabilitiesDomain(value uint64) (domain.Capabilities, error) {
	if value == 0 || value > uint64(domain.AllCapabilities) {
		return 0, fleeterror.NewInvalidArgumentError("invalid account capabilities")
	}
	return domain.Capabilities(value), nil
}

func (h *Handler) CreateAccountKey(ctx context.Context, req *connect.Request[pb.CreateAccountKeyRequest]) (*connect.Response[pb.CreateAccountKeyResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	capabilities, err := capabilitiesDomain(req.Msg.Capabilities)
	if err != nil {
		return nil, err
	}
	options := domain.KeyOptions{Name: req.Msg.Name, Capabilities: capabilities, DescendantAccountIDs: req.Msg.DescendantAccountIds, IncludeFutureDescendants: req.Msg.IncludeFutureDescendants}
	if req.Msg.ExpiresAt != nil {
		if req.Msg.ExpiresAt.CheckValid() != nil {
			return nil, fleeterror.NewInvalidArgumentError("invalid key expiration")
		}
		expiry := req.Msg.ExpiresAt.AsTime()
		options.ExpiresAt = &expiry
	}
	issued, err := h.keys.Create(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, options)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateAccountKeyResponse{Key: keyDTO(issued.Key), Secret: issued.Secret}), nil
}
func (h *Handler) ListAccountKeys(ctx context.Context, req *connect.Request[pb.ListAccountKeysRequest]) (*connect.Response[pb.ListAccountKeysResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	keys, next, err := h.keys.List(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.PageSize, req.Msg.PageToken)
	if err != nil {
		return nil, err
	}
	response := &pb.ListAccountKeysResponse{NextPageToken: next}
	for _, key := range keys {
		response.Keys = append(response.Keys, keyDTO(key))
	}
	return connect.NewResponse(response), nil
}
func (h *Handler) RevokeAccountKey(ctx context.Context, req *connect.Request[pb.RevokeAccountKeyRequest]) (*connect.Response[pb.RevokeAccountKeyResponse], error) {
	info, err := principal(ctx, req.Msg.AccountId)
	if err != nil {
		return nil, err
	}
	if err = h.keys.Revoke(ctx, info.UserID, info.OrganizationID, req.Msg.AccountId, req.Msg.KeyId); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.RevokeAccountKeyResponse{}), nil
}
func keyDTO(key domain.AccountKey) *pb.AccountKey {
	result := &pb.AccountKey{KeyId: key.KeyID, Name: key.Name, Prefix: key.Prefix, Capabilities: uint64(key.Capabilities), DescendantAccountIds: key.DescendantAccountIds, IncludeFutureDescendants: key.IncludeFutureDescendants, CreatedAt: timestamppb.New(key.CreatedAt), IssuerUserId: key.IssuerUserID, IssuerUsername: key.IssuerUsername} // #nosec G115 -- stored account-key capability masks are constrained to 1..7.
	result.ExpiresAt = nullableTimestamp(key.ExpiresAt)
	result.LastUsedAt = nullableTimestamp(key.LastUsedAt)
	return result
}
