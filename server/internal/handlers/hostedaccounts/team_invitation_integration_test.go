package hostedaccounts_test

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	authpb "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	invpb "github.com/block/proto-fleet/server/generated/grpc/invitation/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func (f rpcFixture) builtinRoleID(t *testing.T, key authz.BuiltinKey) int64 {
	t.Helper()
	var roleID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key=$2`, f.org, string(key)).Scan(&roleID))
	return roleID
}

func (f rpcFixture) addTeamUser(t *testing.T, username string, key authz.BuiltinKey) int64 {
	t.Helper()
	userID := f.addUser(t, username, false)
	roleID := f.builtinRoleID(t, key)
	_, err := f.db.ExecContext(t.Context(), `UPDATE user_organization SET role_id=$3 WHERE user_id=$1 AND organization_id=$2`, userID, f.org, roleID)
	require.NoError(t, err)
	_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: userID, OrganizationID: f.org, RoleID: roleID, ScopeType: "org"})
	require.NoError(t, err)
	return userID
}

func (f rpcFixture) teamInvite(t *testing.T, cookie string, roleID int64) *invpb.CreateTeamInvitationResponse {
	t.Helper()
	response, err := f.invitations.CreateTeamInvitation(t.Context(), rpcRequest(&invpb.CreateTeamInvitationRequest{RoleId: strconv.FormatInt(roleID, 10)}, cookie, ""))
	require.NoError(t, err)
	require.Regexp(t, `^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`, response.Msg.Token)
	return response.Msg
}

func (f rpcFixture) listedTeamInvitation(t *testing.T, invitationID string) *invpb.TeamInvitation {
	t.Helper()
	listed, err := f.invitations.ListTeamInvitations(t.Context(), rpcRequest(&invpb.ListTeamInvitationsRequest{PageSize: 100}, f.cookie, ""))
	require.NoError(t, err)
	for _, invitation := range listed.Msg.Invitations {
		if invitation.InvitationId == invitationID {
			return invitation
		}
	}
	return nil
}

func (f rpcFixture) teamInvitationStatus(t *testing.T, invitationID string) *invpb.TeamInvitation {
	t.Helper()
	invitation := f.listedTeamInvitation(t, invitationID)
	require.NotNil(t, invitation, "invitation %s is listed", invitationID)
	return invitation
}

func TestTeamInvitationRegistersLoginWithOrganizationRole(t *testing.T) {
	f := newRPCFixture(t)
	issued := f.teamInvite(t, f.cookie, f.builtinRoleID(t, authz.BuiltinKeyFieldTech))
	manualCode := strings.ToLower(strings.ReplaceAll(issued.Token, "-", " "))
	preview, err := f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: manualCode}, "", ""))
	require.NoError(t, err)
	require.Equal(t, "RPC hosting", preview.Msg.GetTeam().GetOrganizationName())
	require.Equal(t, "FIELD_TECH", preview.Msg.GetTeam().GetRoleName())
	require.Nil(t, preview.Msg.GetAccount())

	registered := f.register(t, manualCode, "invited-field-tech")
	joined := registered.Msg.GetTeam().GetUserInfo()
	require.Equal(t, "FIELD_TECH", joined.GetRole())
	require.NotEmpty(t, joined.GetPermissions())
	require.False(t, joined.GetRequiresPasswordChange(), "recipients choose their own password")
	cookie := registered.Header().Get("Set-Cookie")
	listed, err := f.client.ListAccounts(t.Context(), rpcRequest(&pb.ListAccountsRequest{}, cookie, ""))
	require.NoError(t, err, "registration cookie authenticates the new session")
	require.True(t, listed.Msg.HostAccess)
	login := f.login(t, "invited-field-tech")
	require.Equal(t, "FIELD_TECH", login.Msg.UserInfo.Role)
	require.ElementsMatch(t, joined.GetPermissions(), login.Msg.UserInfo.Permissions)
	users, err := f.auth.ListUsers(t.Context(), rpcRequest(&authpb.ListUsersRequest{}, f.cookie, ""))
	require.NoError(t, err)
	var listedUser bool
	for _, user := range users.Msg.Users {
		listedUser = listedUser || user.Username == "invited-field-tech"
	}
	require.True(t, listedUser, "the new login appears in the host team")

	_, err = f.invitations.RegisterInvitedUser(t.Context(), rpcRequest(&invpb.RegisterInvitedUserRequest{Token: issued.Token, Username: "replayed-tech", Password: rpcPassword}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "codes are single-use")
	require.Nil(t, f.listedTeamInvitation(t, issued.Invitation.InvitationId), "accepted invitations leave the list")

	var activity string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COALESCE(string_agg(row_to_json(a)::text,E'\n'),'') FROM activity_log a WHERE organization_id=$1`, f.org).Scan(&activity))
	require.Contains(t, activity, "create_team_invitation")
	require.Contains(t, activity, "accept_team_invitation")
	require.NotContains(t, activity, issued.Token)
	require.NotContains(t, activity, rpcPassword)
}

func TestTeamInvitationPromotesSignedInCustomerLogin(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	customer := f.addUser(t, "customer-login", false)
	require.NoError(t, f.accounts.SetMembership(t.Context(), f.host, f.org, account.AccountID, customer, domain.RoleViewer))
	login := f.login(t, "customer-login")
	require.Empty(t, login.Msg.UserInfo.Role)
	cookie := login.Header().Get("Set-Cookie")
	_, err := f.auth.ListUsers(t.Context(), rpcRequest(&authpb.ListUsersRequest{}, cookie, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	issued := f.teamInvite(t, f.cookie, f.builtinRoleID(t, authz.BuiltinKeyAdmin))
	accepted, err := f.invitations.AcceptInvitation(t.Context(), rpcRequest(&invpb.AcceptInvitationRequest{Token: issued.Token}, cookie, ""))
	require.NoError(t, err)
	require.Equal(t, "ADMIN", accepted.Msg.GetTeam().GetUserInfo().GetRole())
	require.Contains(t, accepted.Msg.GetTeam().GetUserInfo().GetPermissions(), authz.PermUserRead, "the response refreshes the caller's permissions")
	_, err = f.auth.ListUsers(t.Context(), rpcRequest(&authpb.ListUsersRequest{}, cookie, ""))
	require.NoError(t, err, "the existing session gains the role immediately")
	got, err := f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, cookie, account.AccountID))
	require.NoError(t, err, "customer membership is preserved")
	require.Equal(t, pb.AccountRole_ACCOUNT_ROLE_VIEWER, got.Msg.Account.Role)
	require.Equal(t, "ADMIN", f.login(t, "customer-login").Msg.UserInfo.Role)
}

func TestTeamInvitationRejectsExistingMembersAndOtherOrganizations(t *testing.T) {
	f := newRPCFixture(t)
	f.addTeamUser(t, "existing-tech", authz.BuiltinKeyFieldTech)
	issued := f.teamInvite(t, f.cookie, f.builtinRoleID(t, authz.BuiltinKeyAdmin))
	_, err := f.invitations.AcceptInvitation(t.Context(), rpcRequest(&invpb.AcceptInvitationRequest{Token: issued.Token}, f.login(t, "existing-tech").Header().Get("Set-Cookie"), ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Contains(t, err.Error(), "already on this team")
	require.Equal(t, "FIELD_TECH", f.login(t, "existing-tech").Msg.UserInfo.Role, "an invitation cannot change an existing role")
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_PENDING, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)

	var foreignOrg int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization (org_id,name) VALUES ($1,'Foreign team') RETURNING id`, uuid.NewString()).Scan(&foreignOrg))
	foreignName := "foreign-" + uuid.NewString()
	foreignUser := f.addUser(t, foreignName, false)
	_, err = f.db.ExecContext(t.Context(), `UPDATE user_organization SET organization_id=$2 WHERE user_id=$1`, foreignUser, foreignOrg)
	require.NoError(t, err)
	_, err = f.invitations.AcceptInvitation(t.Context(), rpcRequest(&invpb.AcceptInvitationRequest{Token: issued.Token}, f.login(t, foreignName).Header().Get("Set-Cookie"), ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "codes never cross organizations")
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_PENDING, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)
}

func TestTeamInvitationEnforcesRoleHierarchyAtIssuanceAndAcceptance(t *testing.T) {
	f := newRPCFixture(t)
	f.addTeamUser(t, "team-admin", authz.BuiltinKeyAdmin)
	adminCookie := f.login(t, "team-admin").Header().Get("Set-Cookie")
	adminRole := strconv.FormatInt(f.builtinRoleID(t, authz.BuiltinKeyAdmin), 10)
	_, err := f.invitations.CreateTeamInvitation(t.Context(), rpcRequest(&invpb.CreateTeamInvitationRequest{RoleId: adminRole}, adminCookie, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "an Admin cannot mint another Admin")
	superAdminRole := strconv.FormatInt(f.builtinRoleID(t, authz.BuiltinKeySuperAdmin), 10)
	_, err = f.invitations.CreateTeamInvitation(t.Context(), rpcRequest(&invpb.CreateTeamInvitationRequest{RoleId: superAdminRole}, f.cookie, ""))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "ownership transfer is not an invitation")
	_, err = f.invitations.CreateTeamInvitation(t.Context(), rpcRequest(&invpb.CreateTeamInvitationRequest{RoleId: adminRole, ExpiresInSeconds: 86401}, f.cookie, ""))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	f.addUser(t, "customer-only", false)
	_, err = f.invitations.CreateTeamInvitation(t.Context(), rpcRequest(&invpb.CreateTeamInvitationRequest{RoleId: adminRole}, f.login(t, "customer-only").Header().Get("Set-Cookie"), ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	issued := f.teamInvite(t, adminCookie, f.builtinRoleID(t, authz.BuiltinKeyFieldTech))
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.NoError(t, err)
	var adminID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE username='team-admin'`).Scan(&adminID))
	_, err = f.auth.UpdateUserRole(t.Context(), rpcRequest(&authpb.UpdateUserRoleRequest{UserId: adminID, RoleId: strconv.FormatInt(f.builtinRoleID(t, authz.BuiltinKeyFieldTech), 10)}, f.cookie, ""))
	require.NoError(t, err)
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "a demoted inviter's codes stop working")
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_UNAVAILABLE, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status,
		"listing applies the inviter's live hierarchy")
	_, err = f.invitations.RegisterInvitedUser(t.Context(), rpcRequest(&invpb.RegisterInvitedUserRequest{Token: issued.Token, Username: "late-tech", Password: rpcPassword}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	_, err = f.auth.UpdateUserRole(t.Context(), rpcRequest(&authpb.UpdateUserRoleRequest{UserId: adminID, RoleId: adminRole}, f.cookie, ""))
	require.NoError(t, err)
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "re-promoting the inviter does not revive the code")
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_UNAVAILABLE, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)
}

func TestTeamInvitationInvalidatedWhenInviterLeaves(t *testing.T) {
	f := newRPCFixture(t)
	f.addTeamUser(t, "departing-admin", authz.BuiltinKeyAdmin)
	issued := f.teamInvite(t, f.login(t, "departing-admin").Header().Get("Set-Cookie"), f.builtinRoleID(t, authz.BuiltinKeyFieldTech))
	var adminID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE username='departing-admin'`).Scan(&adminID))
	_, err := f.auth.DeactivateUser(t.Context(), rpcRequest(&authpb.DeactivateUserRequest{UserId: adminID}, f.cookie, ""))
	require.NoError(t, err)
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_UNAVAILABLE, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)
	_, err = f.invitations.RegisterInvitedUser(t.Context(), rpcRequest(&invpb.RegisterInvitedUserRequest{Token: issued.Token, Username: "orphaned-tech", Password: rpcPassword}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

func TestTeamInvitationSurvivesPermissionReconciliation(t *testing.T) {
	f := newRPCFixture(t)
	f.addTeamUser(t, "reconciled-admin", authz.BuiltinKeyAdmin)
	issued := f.teamInvite(t, f.login(t, "reconciled-admin").Header().Get("Set-Cookie"), f.builtinRoleID(t, authz.BuiltinKeyFieldTech))
	// Every fleetd start reconciles built-in roles, rewriting their role rows.
	require.NoError(t, authz.Reconcile(t.Context(), f.db))
	_, err := f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.NoError(t, err, "a restart does not invalidate outstanding codes")
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_PENDING, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)
}

func TestTeamInvitationInvalidatedWhenRoleChanges(t *testing.T) {
	f := newRPCFixture(t)
	roles := authz.NewService(f.db, nil)
	role, err := roles.CreateCustomRole(t.Context(), f.host, f.org, "Observers", "", []string{authz.PermFleetRead})
	require.NoError(t, err)
	issued := f.teamInvite(t, f.cookie, role.ID)
	_, err = roles.UpdateCustomRole(t.Context(), f.host, f.org, role.ID, "Observers", "", []string{authz.PermFleetRead, authz.PermMinerRead})
	require.NoError(t, err)
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "broadening the role after issuance invalidates the code")
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_UNAVAILABLE, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)

	_, err = roles.UpdateCustomRole(t.Context(), f.host, f.org, role.ID, "Observers", "", []string{authz.PermFleetRead})
	require.NoError(t, err)
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "restoring the original permissions does not revive the code")
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_UNAVAILABLE, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)

	deleted := f.teamInvite(t, f.cookie, role.ID)
	require.NoError(t, roles.DeleteCustomRole(t.Context(), f.host, f.org, role.ID))
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: deleted.Token}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "a deleted role cannot be granted")
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_UNAVAILABLE, f.teamInvitationStatus(t, deleted.Invitation.InvitationId).Status)
}

func TestTeamInvitationStaysInvalidWhenAnEmptyRoleIsRestored(t *testing.T) {
	f := newRPCFixture(t)
	roles := authz.NewService(f.db, nil)
	role, err := roles.CreateCustomRole(t.Context(), f.host, f.org, "Visitors", "", nil)
	require.NoError(t, err)
	issued := f.teamInvite(t, f.cookie, role.ID)
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.NoError(t, err, "a role without permissions can be invited")
	_, err = roles.UpdateCustomRole(t.Context(), f.host, f.org, role.ID, "Visitors", "", []string{authz.PermFleetRead})
	require.NoError(t, err)
	_, err = roles.UpdateCustomRole(t.Context(), f.host, f.org, role.ID, "Visitors", "", nil)
	require.NoError(t, err)
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "emptying the role again does not revive the code")
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_UNAVAILABLE, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)
	_, err = f.invitations.RegisterInvitedUser(t.Context(), rpcRequest(&invpb.RegisterInvitedUserRequest{Token: issued.Token, Username: "revived-visitor", Password: rpcPassword}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

func TestTeamInvitationRevocation(t *testing.T) {
	f := newRPCFixture(t)
	issued := f.teamInvite(t, f.cookie, f.builtinRoleID(t, authz.BuiltinKeyFieldTech))
	f.addUser(t, "customer-revoker", false)
	_, err := f.invitations.RevokeTeamInvitation(t.Context(), rpcRequest(&invpb.RevokeTeamInvitationRequest{InvitationId: issued.Invitation.InvitationId}, f.login(t, "customer-revoker").Header().Get("Set-Cookie"), ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.invitations.RevokeTeamInvitation(t.Context(), rpcRequest(&invpb.RevokeTeamInvitationRequest{InvitationId: issued.Invitation.InvitationId}, f.cookie, ""))
	require.NoError(t, err)
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	revoked := f.teamInvitationStatus(t, issued.Invitation.InvitationId)
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_REVOKED, revoked.Status)
	require.NotNil(t, revoked.RevokedAt)
	_, err = f.invitations.RevokeTeamInvitation(t.Context(), rpcRequest(&invpb.RevokeTeamInvitationRequest{InvitationId: issued.Invitation.InvitationId}, f.cookie, ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	var revocations int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='revoke_team_invitation' AND description='Revoked user invitation'`, f.org).Scan(&revocations))
	require.Equal(t, 1, revocations, "only the effective revocation is audited")
}

func TestTeamInvitationExpiredCodesAreRejected(t *testing.T) {
	f := newRPCFixture(t)
	issued := f.teamInvite(t, f.cookie, f.builtinRoleID(t, authz.BuiltinKeyFieldTech))
	_, err := f.db.ExecContext(t.Context(), `UPDATE organization_invitation SET created_at=now()-interval '2 days', expires_at=now()-interval '1 second' WHERE invitation_id=$1`, issued.Invitation.InvitationId)
	require.NoError(t, err)
	_, err = f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: issued.Token}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	_, err = f.invitations.RegisterInvitedUser(t.Context(), rpcRequest(&invpb.RegisterInvitedUserRequest{Token: issued.Token, Username: "expired-tech", Password: rpcPassword}, "", ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	f.addUser(t, "expired-customer", false)
	_, err = f.invitations.AcceptInvitation(t.Context(), rpcRequest(&invpb.AcceptInvitationRequest{Token: issued.Token}, f.login(t, "expired-customer").Header().Get("Set-Cookie"), ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Empty(t, f.login(t, "expired-customer").Msg.UserInfo.Role)
	var created int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM "user" WHERE username='expired-tech'`).Scan(&created))
	require.Zero(t, created)
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_EXPIRED, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)
}

func TestTeamInvitationConcurrentRegistrationsAdmitOneLogin(t *testing.T) {
	f := newRPCFixture(t)
	issued := f.teamInvite(t, f.cookie, f.builtinRoleID(t, authz.BuiltinKeyFieldTech))
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, username := range []string{"racing-tech-a", "racing-tech-b"} {
		wg.Go(func() {
			<-start
			_, err := f.invitations.RegisterInvitedUser(t.Context(), rpcRequest(&invpb.RegisterInvitedUserRequest{Token: issued.Token, Username: username, Password: rpcPassword}, "", ""))
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	admitted := 0
	for err := range results {
		if err == nil {
			admitted++
			continue
		}
		require.Contains(t, []connect.Code{connect.CodeFailedPrecondition, connect.CodeResourceExhausted}, connect.CodeOf(err), "%v", err)
	}
	require.Equal(t, 1, admitted)
	var created int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM "user" WHERE username IN ('racing-tech-a','racing-tech-b')`).Scan(&created))
	require.Equal(t, 1, created, "the losing registration leaves no login behind")
}

func TestTeamInvitationTakenUsernameKeepsCodePending(t *testing.T) {
	f := newRPCFixture(t)
	f.addUser(t, "taken-name", false)
	issued := f.teamInvite(t, f.cookie, f.builtinRoleID(t, authz.BuiltinKeyFieldTech))
	_, err := f.invitations.RegisterInvitedUser(t.Context(), rpcRequest(&invpb.RegisterInvitedUserRequest{Token: issued.Token, Username: "taken-name", Password: rpcPassword}, "", ""))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_PENDING, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)
	f.register(t, issued.Token, "untaken-name")
}

func TestTeamInvitationLifetime(t *testing.T) {
	f := newRPCFixture(t)
	roleID := strconv.FormatInt(f.builtinRoleID(t, authz.BuiltinKeyFieldTech), 10)
	for _, tc := range []struct {
		seconds uint32
		want    time.Duration
	}{{3600, time.Hour}, {0, 24 * time.Hour}} {
		created, err := f.invitations.CreateTeamInvitation(t.Context(), rpcRequest(&invpb.CreateTeamInvitationRequest{RoleId: roleID, ExpiresInSeconds: tc.seconds}, f.cookie, ""))
		require.NoError(t, err)
		require.WithinDuration(t, time.Now().Add(tc.want), created.Msg.Invitation.ExpiresAt.AsTime(), 5*time.Second, "expires_in_seconds=%d", tc.seconds)
	}
}

func TestTeamInvitationListPagination(t *testing.T) {
	f := newRPCFixture(t)
	fieldTech := f.builtinRoleID(t, authz.BuiltinKeyFieldTech)
	var issued []string
	for range 3 {
		issued = append(issued, f.teamInvite(t, f.cookie, fieldTech).Invitation.InvitationId)
	}
	first, err := f.invitations.ListTeamInvitations(t.Context(), rpcRequest(&invpb.ListTeamInvitationsRequest{PageSize: 2}, f.cookie, ""))
	require.NoError(t, err)
	require.Len(t, first.Msg.Invitations, 2)
	require.NotEmpty(t, first.Msg.NextPageToken)
	second, err := f.invitations.ListTeamInvitations(t.Context(), rpcRequest(&invpb.ListTeamInvitationsRequest{PageSize: 2, PageToken: first.Msg.NextPageToken}, f.cookie, ""))
	require.NoError(t, err)
	require.Len(t, second.Msg.Invitations, 1)
	require.Empty(t, second.Msg.NextPageToken)
	var listed []string
	for _, invitation := range append(first.Msg.Invitations, second.Msg.Invitations...) {
		listed = append(listed, invitation.InvitationId)
	}
	require.Equal(t, []string{issued[2], issued[1], issued[0]}, listed, "newest first without repeats")

	for _, token := range []string{
		"not-a-token",
		base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("1|%d|%s|%s", f.org+1, time.Now().UTC().Format(time.RFC3339Nano), issued[0]))),
	} {
		_, err = f.invitations.ListTeamInvitations(t.Context(), rpcRequest(&invpb.ListTeamInvitationsRequest{PageToken: token}, f.cookie, ""))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "page tokens are bound to their organization")
	}
}

func TestTeamInvitationManagementIsOrganizationScoped(t *testing.T) {
	f := newRPCFixture(t)
	issued := f.teamInvite(t, f.cookie, f.builtinRoleID(t, authz.BuiltinKeyFieldTech))
	foreign := f.foreignTeamOwner(t)

	listed, err := f.invitations.ListTeamInvitations(t.Context(), rpcRequest(&invpb.ListTeamInvitationsRequest{PageSize: 100}, foreign, ""))
	require.NoError(t, err)
	require.Empty(t, listed.Msg.Invitations, "another organization never sees these invitations")
	_, err = f.invitations.RevokeTeamInvitation(t.Context(), rpcRequest(&invpb.RevokeTeamInvitationRequest{InvitationId: issued.Invitation.InvitationId}, foreign, ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_PENDING, f.teamInvitationStatus(t, issued.Invitation.InvitationId).Status)

	_, err = f.invitations.RevokeTeamInvitation(t.Context(), rpcRequest(&invpb.RevokeTeamInvitationRequest{InvitationId: issued.Invitation.InvitationId}, f.cookie, ""))
	require.NoError(t, err)
	_, err = f.invitations.DeleteTeamInvitation(t.Context(), rpcRequest(&invpb.DeleteTeamInvitationRequest{InvitationId: issued.Invitation.InvitationId}, foreign, ""))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.NotNil(t, f.listedTeamInvitation(t, issued.Invitation.InvitationId), "another organization cannot delete it")
}

// foreignTeamOwner signs in a Super Admin of a second organization in the same
// database and returns the session cookie.
func (f rpcFixture) foreignTeamOwner(t *testing.T) string {
	t.Helper()
	var org int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization (org_id,name) VALUES ($1,'Foreign team') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, authz.Reconcile(t.Context(), f.db))
	username := "foreign-owner-" + uuid.NewString()
	userID := f.addUser(t, username, false)
	var roleID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='SUPER_ADMIN'`, org).Scan(&roleID))
	_, err := f.db.ExecContext(t.Context(), `UPDATE user_organization SET organization_id=$2, role_id=$3 WHERE user_id=$1`, userID, org, roleID)
	require.NoError(t, err)
	_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: userID, OrganizationID: org, RoleID: roleID, ScopeType: "org"})
	require.NoError(t, err)
	return f.login(t, username).Header().Get("Set-Cookie")
}

func TestTeamInvitationDeletionRemovesOnlyDeadInvitations(t *testing.T) {
	f := newRPCFixture(t)
	fieldTech := f.builtinRoleID(t, authz.BuiltinKeyFieldTech)
	pending := f.teamInvite(t, f.cookie, fieldTech)
	revoked := f.teamInvite(t, f.cookie, fieldTech)
	expired := f.teamInvite(t, f.cookie, fieldTech)
	accepted := f.teamInvite(t, f.cookie, fieldTech)
	deleteInvitation := func(invitationID, cookie string) error {
		_, err := f.invitations.DeleteTeamInvitation(t.Context(), rpcRequest(&invpb.DeleteTeamInvitationRequest{InvitationId: invitationID}, cookie, ""))
		return err
	}
	_, err := f.invitations.RevokeTeamInvitation(t.Context(), rpcRequest(&invpb.RevokeTeamInvitationRequest{InvitationId: revoked.Invitation.InvitationId}, f.cookie, ""))
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE organization_invitation SET created_at=now()-interval '2 days', expires_at=now()-interval '1 day' WHERE invitation_id=$1`, expired.Invitation.InvitationId)
	require.NoError(t, err)
	f.register(t, accepted.Token, "accepted-tech")
	require.Equal(t, invpb.TeamInvitationStatus_TEAM_INVITATION_STATUS_EXPIRED, f.teamInvitationStatus(t, expired.Invitation.InvitationId).Status)

	f.addUser(t, "customer-deleter", false)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(deleteInvitation(revoked.Invitation.InvitationId, f.login(t, "customer-deleter").Header().Get("Set-Cookie"))))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(deleteInvitation(pending.Invitation.InvitationId, f.cookie)), "pending codes must be revoked first")
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(deleteInvitation(accepted.Invitation.InvitationId, f.cookie)), "accepted invitations are kept")
	require.NoError(t, deleteInvitation(revoked.Invitation.InvitationId, f.cookie))
	require.NoError(t, deleteInvitation(expired.Invitation.InvitationId, f.cookie))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(deleteInvitation(revoked.Invitation.InvitationId, f.cookie)))

	listed, err := f.invitations.ListTeamInvitations(t.Context(), rpcRequest(&invpb.ListTeamInvitationsRequest{PageSize: 100}, f.cookie, ""))
	require.NoError(t, err)
	require.Len(t, listed.Msg.Invitations, 1)
	require.Equal(t, pending.Invitation.InvitationId, listed.Msg.Invitations[0].InvitationId)
	var acceptedRows int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM organization_invitation WHERE accepted_at IS NOT NULL`).Scan(&acceptedRows))
	require.Equal(t, 1, acceptedRows, "the accepted invitation still links its login")
	var deletions int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE organization_id=$1 AND event_type='delete_team_invitation'`, f.org).Scan(&deletions))
	require.Equal(t, 2, deletions)
}

func TestInvitationCodesResolveToTheirOwnKind(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	accountCode := f.invite(t, account.AccountID, pb.AccountRole_ACCOUNT_ROLE_VIEWER)
	teamCode := f.teamInvite(t, f.cookie, f.builtinRoleID(t, authz.BuiltinKeyFieldTech))
	accountPreview, err := f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: accountCode.Token}, "", ""))
	require.NoError(t, err)
	require.Equal(t, "Customer", accountPreview.Msg.GetAccount().GetAccountName())
	teamPreview, err := f.invitations.PreviewInvitation(t.Context(), rpcRequest(&invpb.PreviewInvitationRequest{Token: teamCode.Token}, "", ""))
	require.NoError(t, err)
	require.NotNil(t, teamPreview.Msg.GetTeam())
	username := "account-only-" + uuid.NewString()
	registered := f.register(t, accountCode.Token, username)
	require.Equal(t, account.AccountID, registered.Msg.GetAccount().GetAccountId())
	require.Empty(t, f.login(t, username).Msg.GetUserInfo().GetRole(), "a customer code never grants a team role")
}
