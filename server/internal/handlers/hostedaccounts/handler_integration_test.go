package hostedaccounts_test

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/grpc/poolconfigurations/v1/poolconfigurationsv1connect"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	poolhandler "github.com/block/proto-fleet/server/internal/handlers/poolconfigurations"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	authpb "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/block/proto-fleet/server/generated/grpc/auth/v1/authv1connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	"github.com/block/proto-fleet/server/generated/grpc/hosted/v1/hostedv1connect"
	invpb "github.com/block/proto-fleet/server/generated/grpc/invitation/v1"
	"github.com/block/proto-fleet/server/generated/grpc/invitation/v1/invitationv1connect"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/apikey"
	domainauth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	authhandler "github.com/block/proto-fleet/server/internal/handlers/auth"
	hostedhandler "github.com/block/proto-fleet/server/internal/handlers/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	invitationhandler "github.com/block/proto-fleet/server/internal/handlers/invitation"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

var rpcClientSequence atomic.Uint32

const rpcPassword = "customer-password123"

type rpcFixture struct {
	configurations poolconfigurationsv1connect.PoolConfigurationServiceClient
	db             *sql.DB
	accounts       *domain.Service
	keys           *apikey.Service
	client         hostedv1connect.HostedAccountServiceClient
	invitations    invitationv1connect.InvitationServiceClient
	auth           authv1connect.AuthServiceClient
	org            int64
	host           int64
	root           string
	cookie         string
	peerAddress    *atomic.Pointer[string]
}

// The RPC server uses the same identity, session, permission, validation, error
// mapping and logging components as fleetd. Only unrelated services and the
// process lifecycle interceptors are omitted.
func newRPCFixture(t *testing.T) rpcFixture {
	t.Helper()
	db := dbtest.GetTestDB(t)
	var org int64
	require.NoError(t, db.QueryRowContext(t.Context(), `INSERT INTO organization (org_id,name) VALUES ($1,'RPC hosting') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, authz.Reconcile(t.Context(), db))
	users := sqlstores.NewSQLUserStore(db)
	sessions := session.NewService(session.Config{Duration: time.Hour, IDBytes: 32, CookieName: "fleet_session"}, sqlstores.NewSQLSessionStore(db))
	permissions := authz.NewPermissionResolver(db)
	accounts := domain.NewService(db)
	keys := apikey.NewService(sqlstores.NewSQLApiKeyStore(db), nil)
	accountKeys := domain.NewKeyService(accounts, keys)
	authService := domainauth.NewService(users, users, sqlstores.NewSQLTransactor(db), nil, sessions, nil, nil, permissions)
	poolSecrets, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	pools := domain.NewPoolService(accounts, poolSecrets, accountKeys, authService)
	requestAuth := interceptors.NewAuthInterceptor(sessions, users, users, keys, permissions, accountKeys, interceptors.UnauthenticatedProcedures, interceptors.SessionOnlyProcedures, interceptors.FleetNodeAuthenticatedProcedures)
	options := connect.WithInterceptors(
		interceptors.NewErrorMappingInterceptor(),
		interceptors.NewRequestLoggingInterceptor(slog.LevelDebug, interceptors.RedactedRequestProcedures, interceptors.RedactedResponseProcedures),
		requestAuth,
		validate.NewInterceptor(),
	)
	mux := http.NewServeMux()
	mux.Handle(poolconfigurationsv1connect.NewPoolConfigurationServiceHandler(poolhandler.NewHandler(poolconfigurations.NewService(db, poolconfigurations.NewRepository(poolSecrets), pools)), options))
	invitations := domain.NewInvitationService(accounts, sessions)
	mux.Handle(hostedv1connect.NewHostedAccountServiceHandler(hostedhandler.NewHandler(accounts, invitations, accountKeys, pools, users, permissions), options))
	mux.Handle(invitationv1connect.NewInvitationServiceHandler(invitationhandler.NewHandler(invitations, domain.NewTeamInvitationService(accounts, sessions)), options))
	mux.Handle(authv1connect.NewAuthServiceHandler(authhandler.NewHandler(authService), options))
	clientIP, err := middleware.NewClientIPMiddleware("")
	require.NoError(t, err)
	// Each fixture represents an independent trusted peer; sharing the
	// loopback address would spend another fixture's registration allowance.
	peerAddress := &atomic.Pointer[string]{}
	initialPeer := fmt.Sprintf("192.0.2.%d:1234", rpcClientSequence.Add(1))
	peerAddress.Store(&initialPeer)
	wrapped := clientIP.Wrap(mux)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = *peerAddress.Load()
		wrapped.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	f := rpcFixture{db: db, accounts: accounts, keys: keys, org: org, peerAddress: peerAddress,
		configurations: poolconfigurationsv1connect.NewPoolConfigurationServiceClient(server.Client(), server.URL),
		client:         hostedv1connect.NewHostedAccountServiceClient(server.Client(), server.URL),
		invitations:    invitationv1connect.NewInvitationServiceClient(server.Client(), server.URL),
		auth:           authv1connect.NewAuthServiceClient(server.Client(), server.URL)}
	f.host = f.addUser(t, "host-"+uuid.NewString(), true)
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT account_id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&f.root))
	var username string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT username FROM "user" WHERE id=$1`, f.host).Scan(&username))
	f.cookie = f.login(t, username).Header().Get("Set-Cookie")
	return f
}

func (f rpcFixture) addUser(t *testing.T, username string, hostRole bool) int64 {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(rpcPassword), bcrypt.MinCost)
	require.NoError(t, err)
	var userID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO "user" (user_id,username,password_hash) VALUES ($1,$2,$3) RETURNING id`, uuid.NewString(), username, string(hash)).Scan(&userID))
	_, err = f.db.ExecContext(t.Context(), `INSERT INTO user_organization (user_id,organization_id,role_id) VALUES ($1,$2,NULL)`, userID, f.org)
	require.NoError(t, err)
	if hostRole {
		var roleID int64
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='SUPER_ADMIN'`, f.org).Scan(&roleID))
		_, err = f.db.ExecContext(t.Context(), `UPDATE user_organization SET role_id=$3 WHERE user_id=$1 AND organization_id=$2`, userID, f.org, roleID)
		require.NoError(t, err)
		_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: userID, OrganizationID: f.org, RoleID: roleID, ScopeType: "org"})
		require.NoError(t, err)
	}
	return userID
}

func (f rpcFixture) login(t *testing.T, username string) *connect.Response[authpb.AuthenticateResponse] {
	t.Helper()
	response, err := f.auth.Authenticate(t.Context(), connect.NewRequest(&authpb.AuthenticateRequest{Username: username, Password: rpcPassword}))
	require.NoError(t, err)
	require.NotEmpty(t, response.Header().Get("Set-Cookie"))
	return response
}

func (f rpcFixture) account(t *testing.T, name string, adminID int64, capabilities domain.Capabilities) domain.Account {
	t.Helper()
	account, err := f.accounts.CreateChild(t.Context(), f.host, f.org, f.root, name, adminID, capabilities)
	require.NoError(t, err)
	return account.Account
}

func rpcRequest[T any](message *T, cookie, accountID string) *connect.Request[T] {
	request := connect.NewRequest(message)
	if cookie != "" {
		request.Header().Set("Cookie", cookie)
	}
	if accountID != "" {
		request.Header().Set(domain.RequestAccountHeader, accountID)
	}
	return request
}

func (f rpcFixture) invite(t *testing.T, accountID string, role pb.AccountRole) *pb.CreateInvitationResponse {
	t.Helper()
	response, err := f.client.CreateInvitation(t.Context(), rpcRequest(&pb.CreateInvitationRequest{AccountId: accountID, Role: role}, f.cookie, accountID))
	require.NoError(t, err)
	require.Regexp(t, `^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`, response.Msg.Token)
	return response.Msg
}

func (f rpcFixture) register(t *testing.T, token, username string) *connect.Response[invpb.RegisterInvitedUserResponse] {
	t.Helper()
	response, err := f.invitations.RegisterInvitedUser(t.Context(), connect.NewRequest(&invpb.RegisterInvitedUserRequest{Token: token, Username: username, Password: rpcPassword}))
	require.NoError(t, err)
	require.NotEmpty(t, response.Header().Get("Set-Cookie"))
	return response
}

func TestHostedRPCInvitedRegistrationAndRolelessAuthentication(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer team", f.host, domain.DefaultCapabilities)
	issued := f.invite(t, account.AccountID, pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR)
	manualCode := " " + strings.ToLower(strings.ReplaceAll(issued.Token, "-", " ")) + " "
	preview, err := f.invitations.PreviewInvitation(t.Context(), connect.NewRequest(&invpb.PreviewInvitationRequest{Token: manualCode}))
	require.NoError(t, err)
	require.Equal(t, "Customer team", preview.Msg.GetAccount().GetAccountName())
	require.Equal(t, pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR, preview.Msg.GetAccount().GetRole())
	registered := f.register(t, manualCode, "username-without-email")
	require.Equal(t, account.AccountID, registered.Msg.GetAccount().GetAccountId())
	listed, err := f.client.ListAccounts(t.Context(), rpcRequest(&pb.ListAccountsRequest{}, registered.Header().Get("Set-Cookie"), ""))
	require.NoError(t, err, "registration cookie must authenticate the real SQL session")
	require.False(t, listed.Msg.HostAccess)
	require.Len(t, listed.Msg.Accounts, 1)
	require.Equal(t, account.AccountID, listed.Msg.Accounts[0].AccountId)
	login := f.login(t, "username-without-email")
	require.Empty(t, login.Msg.UserInfo.Role)
	require.Empty(t, login.Msg.UserInfo.Permissions)
	cookie := login.Header().Get("Set-Cookie")
	got, err := f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, cookie, account.AccountID))
	require.NoError(t, err)
	require.Equal(t, account.AccountID, got.Msg.Account.AccountId)
	require.Equal(t, pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR, got.Msg.Account.Role)
	members, err := f.client.ListMembers(t.Context(), rpcRequest(&pb.ListMembersRequest{AccountId: account.AccountID}, cookie, account.AccountID))
	require.NoError(t, err)
	require.Len(t, members.Msg.Members, 2)
	var joined bool
	for _, member := range members.Msg.Members {
		if member.Username == "username-without-email" {
			joined = true
			require.Equal(t, login.Msg.UserInfo.UserId, member.UserId)
		}
	}
	require.True(t, joined)
	_, err = f.auth.ListUsers(t.Context(), rpcRequest(&authpb.ListUsersRequest{}, cookie, ""))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "customer membership grants no host directory permission")
	_, err = f.invitations.RegisterInvitedUser(t.Context(), connect.NewRequest(&invpb.RegisterInvitedUserRequest{Token: issued.Token, Username: "replayed-user", Password: rpcPassword}))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	var replayed int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM "user" WHERE username='replayed-user'`).Scan(&replayed))
	require.Zero(t, replayed)
}

func TestHostedRPCInvitationRolesAndRequestIsolation(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Customer", f.host, domain.DefaultCapabilities)
	sibling := f.account(t, "Sibling", f.host, domain.DefaultCapabilities)
	var foreignOrg int64
	var foreignRoot string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization (org_id,name) VALUES ($1,'Foreign host') RETURNING id`, uuid.NewString()).Scan(&foreignOrg))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT account_id FROM hosted_account WHERE organization_id=$1 AND depth=0`, foreignOrg).Scan(&foreignRoot))
	for _, role := range []pb.AccountRole{pb.AccountRole_ACCOUNT_ROLE_VIEWER, pb.AccountRole_ACCOUNT_ROLE_POOL_OPERATOR, pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR} {
		t.Run(role.String(), func(t *testing.T) {
			issued := f.invite(t, account.AccountID, role)
			registered := f.register(t, issued.Token, role.String())
			cookie := registered.Header().Get("Set-Cookie")
			_, err := f.client.CreateInvitation(t.Context(), rpcRequest(&pb.CreateInvitationRequest{AccountId: account.AccountID, Role: pb.AccountRole_ACCOUNT_ROLE_VIEWER}, cookie, account.AccountID))
			if role == pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR {
				require.NoError(t, err)
			} else {
				require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
			}
			_, err = f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: sibling.AccountID}, cookie, account.AccountID))
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "body cannot switch the selected account")
			_, err = f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: sibling.AccountID}, cookie, sibling.AccountID))
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "matching header does not authorize a sibling")
			_, err = f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: uuid.NewString()}, cookie, ""))
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
			_, err = f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: foreignRoot}, cookie, foreignRoot))
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "real foreign-organization IDs stay inaccessible")
		})
	}
	_, err := f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: "invalid"}, f.cookie, ""))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err), "production validation rejects malformed IDs")
	_, err = f.client.CreateInvitation(t.Context(), rpcRequest(&pb.CreateInvitationRequest{AccountId: account.AccountID, Role: pb.AccountRole_ACCOUNT_ROLE_UNSPECIFIED}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	_, err = f.client.GetAccount(t.Context(), connect.NewRequest(&pb.GetAccountRequest{AccountId: account.AccountID}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestHostedRPCSelectedAccountCannotUseHostGrantAuthority(t *testing.T) {
	f := newRPCFixture(t)
	customerAdmin := f.addUser(t, "private-admin", false)
	account := f.account(t, "Private customer", customerAdmin, domain.DefaultCapabilities)
	require.NoError(t, f.accounts.SetMembership(t.Context(), customerAdmin, f.org, account.AccountID, f.host, domain.RoleViewer))
	grant := &pb.SetAncestorGrantRequest{AccountId: account.AccountID, AncestorAccountId: f.root, Capabilities: uint64(domain.View)}
	_, err := f.client.SetAncestorGrant(t.Context(), rpcRequest(grant, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "host permissions cannot widen a selected customer role")
	require.NoError(t, f.accounts.RevokeMembership(t.Context(), customerAdmin, f.org, account.AccountID, f.host))
	_, err = f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "custody alone grants no private account access")
	_, err = f.client.SetAncestorGrant(t.Context(), rpcRequest(grant, f.cookie, ""))
	require.NoError(t, err, "host mode may explicitly establish and audit an ancestor grant")
	got, err := f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.NoError(t, err)
	require.Equal(t, uint64(domain.View), got.Msg.Account.EffectiveCapabilities)
	_, err = f.client.ListMembers(t.Context(), rpcRequest(&pb.ListMembersRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "a view grant still does not grant membership administration")
	_, err = f.client.RevokeAncestorGrant(t.Context(), rpcRequest(&pb.RevokeAncestorGrantRequest{AccountId: account.AccountID, AncestorAccountId: f.root}, f.cookie, ""))
	require.NoError(t, err)
	_, err = f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, f.cookie, account.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "the next request observes revoked access")
}

func TestHostedRPCExistingAcceptanceRequiresSessionAndPreservesIdentity(t *testing.T) {
	f := newRPCFixture(t)
	account := f.account(t, "Accept existing", f.host, domain.DefaultCapabilities)
	userID := f.addUser(t, "existing-customer", false)
	login := f.login(t, "existing-customer")
	issued := f.invite(t, account.AccountID, pb.AccountRole_ACCOUNT_ROLE_VIEWER)
	_, err := f.invitations.AcceptInvitation(t.Context(), connect.NewRequest(&invpb.AcceptInvitationRequest{Token: issued.Token}))
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	key, _, err := f.keys.Create(t.Context(), f.host, f.org, "", "", "Host key", nil)
	require.NoError(t, err)
	keyRequest := connect.NewRequest(&invpb.AcceptInvitationRequest{Token: issued.Token})
	keyRequest.Header().Set("Authorization", "Bearer "+key)
	_, err = f.invitations.AcceptInvitation(t.Context(), keyRequest)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	listRequest := connect.NewRequest(&pb.ListAccountsRequest{})
	listRequest.Header().Set("Authorization", "Bearer "+key)
	_, err = f.client.ListAccounts(t.Context(), listRequest)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "org API keys cannot access the dedicated customer service")
	manualCode := strings.ToLower(strings.ReplaceAll(issued.Token, "-", ""))
	accepted, err := f.invitations.AcceptInvitation(t.Context(), rpcRequest(&invpb.AcceptInvitationRequest{Token: manualCode}, login.Header().Get("Set-Cookie"), ""))
	require.NoError(t, err)
	require.Equal(t, account.AccountID, accepted.Msg.GetAccount().GetAccountId())
	var linked int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT accepted_user_id FROM hosted_account_invitation WHERE invitation_id=$1`, issued.Invitation.InvitationId).Scan(&linked))
	require.Equal(t, userID, linked, "acceptance attaches the existing Fleet identity")
	got, err := f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: account.AccountID}, login.Header().Get("Set-Cookie"), account.AccountID))
	require.NoError(t, err)
	require.Equal(t, pb.AccountRole_ACCOUNT_ROLE_VIEWER, got.Msg.Account.Role)
	reauthenticated := f.login(t, "existing-customer")
	require.Equal(t, login.Msg.UserInfo.UserId, reauthenticated.Msg.UserInfo.UserId)
	require.Empty(t, reauthenticated.Msg.UserInfo.Role)
	require.Empty(t, reauthenticated.Msg.UserInfo.Permissions)
}

func TestHostedRPCParentDelegationControlsPolicyWithoutPrivateChildAccess(t *testing.T) {
	f := newRPCFixture(t)
	parentAdmin := f.addUser(t, "parent-administrator", false)
	childAdmin := f.addUser(t, "child-administrator", false)
	parent := f.account(t, "Parent", parentAdmin, domain.AllCapabilities)
	child, err := f.accounts.CreateChild(t.Context(), parentAdmin, f.org, parent.AccountID, "Subclient", childAdmin, domain.DefaultCapabilities)
	require.NoError(t, err)
	parentCookie := f.login(t, "parent-administrator").Header().Get("Set-Cookie")
	childCookie := f.login(t, "child-administrator").Header().Get("Set-Cookie")
	var siteID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site (name,org_id,slug) VALUES ('Hosted site',$1,'hosted') RETURNING id`, f.org).Scan(&siteID))
	var secondSiteID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site (name,org_id,slug) VALUES ('Second hosted site',$1,'second-hosted') RETURNING id`, f.org).Scan(&secondSiteID))
	_, err = f.client.SetSitePolicy(t.Context(), rpcRequest(&pb.SetSitePolicyRequest{AccountId: parent.AccountID, SitePolicy: &pb.SitePolicy{Restricted: true, SiteIds: []int64{siteID, secondSiteID}}}, parentCookie, parent.AccountID))
	require.NoError(t, err)
	_, err = f.client.SetSitePolicy(t.Context(), rpcRequest(&pb.SetSitePolicyRequest{AccountId: parent.AccountID, ChildAccountId: &child.AccountID, SitePolicy: &pb.SitePolicy{Restricted: true, SiteIds: []int64{siteID}}}, parentCookie, parent.AccountID))
	require.NoError(t, err, "the active parent can configure its directly delegated child's policy")
	policy, err := f.client.GetSitePolicy(t.Context(), rpcRequest(&pb.GetSitePolicyRequest{AccountId: parent.AccountID, ChildAccountId: &child.AccountID}, parentCookie, parent.AccountID))
	require.NoError(t, err)
	require.Equal(t, []int64{siteID}, policy.Msg.EffectiveSiteIds)
	_, err = f.client.ListMembers(t.Context(), rpcRequest(&pb.ListMembersRequest{AccountId: child.AccountID}, parentCookie, child.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "delegation does not authorize a child's private roster")
	_, err = f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: child.AccountID}, parentCookie, child.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.SetSitePolicy(t.Context(), rpcRequest(&pb.SetSitePolicyRequest{AccountId: child.AccountID, SitePolicy: &pb.SitePolicy{}}, parentCookie, child.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "a selected child request cannot fall back to the parent's delegation authority")
	_, err = f.client.GetSitePolicy(t.Context(), rpcRequest(&pb.GetSitePolicyRequest{AccountId: child.AccountID}, parentCookie, child.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = f.client.SetSitePolicy(t.Context(), rpcRequest(&pb.SetSitePolicyRequest{AccountId: child.AccountID, SitePolicy: &pb.SitePolicy{}}, childCookie, child.AccountID))
	require.NoError(t, err, "the child may remove its own restriction")
	policy, err = f.client.GetSitePolicy(t.Context(), rpcRequest(&pb.GetSitePolicyRequest{AccountId: child.AccountID}, childCookie, child.AccountID))
	require.NoError(t, err)
	require.False(t, policy.Msg.SitePolicy.Restricted)
	require.True(t, policy.Msg.EffectiveRestricted)
	require.Equal(t, []int64{siteID}, policy.Msg.EffectiveSiteIds, "the parent's restriction still bounds the child")
	policy, err = f.client.GetSitePolicy(t.Context(), rpcRequest(&pb.GetSitePolicyRequest{AccountId: parent.AccountID, ChildAccountId: &child.AccountID}, parentCookie, parent.AccountID))
	require.NoError(t, err)
	require.True(t, policy.Msg.SitePolicy.Restricted)
	require.Equal(t, []int64{siteID}, policy.Msg.SitePolicy.SiteIds, "child edits preserve the parent's configured ceiling")
	require.NoError(t, f.accounts.SetMembership(t.Context(), childAdmin, f.org, child.AccountID, parentAdmin, domain.RoleAdministrator))
	policy, err = f.client.GetSitePolicy(t.Context(), rpcRequest(&pb.GetSitePolicyRequest{AccountId: parent.AccountID, ChildAccountId: &child.AccountID}, parentCookie, ""))
	require.NoError(t, err)
	require.True(t, policy.Msg.SitePolicy.Restricted, "an explicit child target reads the parent ceiling even without a workspace header")
	require.Equal(t, []int64{siteID}, policy.Msg.SitePolicy.SiteIds)
	_, err = f.client.SetSitePolicy(t.Context(), rpcRequest(&pb.SetSitePolicyRequest{AccountId: parent.AccountID, ChildAccountId: &child.AccountID, SitePolicy: &pb.SitePolicy{Restricted: true, SiteIds: []int64{secondSiteID}}}, parentCookie, ""))
	require.NoError(t, err)
	policy, err = f.client.GetSitePolicy(t.Context(), rpcRequest(&pb.GetSitePolicyRequest{AccountId: child.AccountID}, childCookie, child.AccountID))
	require.NoError(t, err)
	require.False(t, policy.Msg.SitePolicy.Restricted, "an explicit child target writes only the parent ceiling")
	require.Equal(t, []int64{secondSiteID}, policy.Msg.EffectiveSiteIds)
	_, err = f.client.SetSitePolicy(t.Context(), rpcRequest(&pb.SetSitePolicyRequest{AccountId: child.AccountID, SitePolicy: &pb.SitePolicy{}}, f.cookie, child.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "host permissions cannot widen customer policy in a selected child workspace")
	sibling := f.account(t, "Sibling", f.host, domain.AllCapabilities)
	_, err = f.client.GetSitePolicy(t.Context(), rpcRequest(&pb.GetSitePolicyRequest{AccountId: parent.AccountID, ChildAccountId: &sibling.AccountID}, parentCookie, parent.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "optional child IDs cannot select a sibling")
}

func TestHostedRPCBoundedManagerCannotPromoteOrInviteBroaderRoles(t *testing.T) {
	f := newRPCFixture(t)
	username := "bounded-manager-" + uuid.NewString()
	manager := f.addUser(t, username, false)
	parent := f.account(t, "Parent", manager, domain.AllCapabilities)
	child, err := f.accounts.CreateChild(t.Context(), manager, f.org, parent.AccountID, "Child", f.host, domain.AllCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.accounts.SetMembership(t.Context(), f.host, f.org, child.AccountID, manager, domain.RoleViewer))
	require.NoError(t, f.accounts.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID, domain.View|domain.ManageMembers))
	cookie := f.login(t, username).Header().Get("Set-Cookie")
	var publicID string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, manager).Scan(&publicID))
	for _, role := range []pb.AccountRole{pb.AccountRole_ACCOUNT_ROLE_POOL_OPERATOR, pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR} {
		_, err := f.client.SetMemberRole(t.Context(), rpcRequest(&pb.SetMemberRoleRequest{AccountId: child.AccountID, UserId: publicID, Role: role}, cookie, child.AccountID))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		_, err = f.client.CreateInvitation(t.Context(), rpcRequest(&pb.CreateInvitationRequest{AccountId: child.AccountID, Role: role}, cookie, child.AccountID))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	}
	_, err = f.client.SetAncestorGrant(t.Context(), rpcRequest(&pb.SetAncestorGrantRequest{AccountId: child.AccountID, AncestorAccountId: parent.AccountID, Capabilities: uint64(domain.AllCapabilities)}, cookie, child.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	issued, err := f.client.CreateInvitation(t.Context(), rpcRequest(&pb.CreateInvitationRequest{AccountId: child.AccountID, Role: pb.AccountRole_ACCOUNT_ROLE_VIEWER}, cookie, child.AccountID))
	require.NoError(t, err)
	accepted, err := f.invitations.AcceptInvitation(t.Context(), rpcRequest(&invpb.AcceptInvitationRequest{Token: issued.Msg.Token}, cookie, ""))
	require.NoError(t, err)
	require.Equal(t, child.AccountID, accepted.Msg.GetAccount().GetAccountId())
	issued, err = f.client.CreateInvitation(t.Context(), rpcRequest(&pb.CreateInvitationRequest{AccountId: child.AccountID, Role: pb.AccountRole_ACCOUNT_ROLE_VIEWER}, cookie, child.AccountID))
	require.NoError(t, err)
	registered := f.register(t, issued.Msg.Token, "controlled-user-"+uuid.NewString())
	got, err := f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: child.AccountID}, registered.Header().Get("Set-Cookie"), child.AccountID))
	require.NoError(t, err)
	require.Equal(t, uint64(domain.View), got.Msg.Account.EffectiveCapabilities)
	require.NoError(t, f.accounts.RevokeAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID))
	got, err = f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: child.AccountID}, cookie, child.AccountID))
	require.NoError(t, err)
	require.Equal(t, uint64(domain.View), got.Msg.Account.EffectiveCapabilities)
}

func TestHostedRPCSiteOnlyOperatorKeepsFleetAccessWithoutPrivateAccounts(t *testing.T) {
	f := newRPCFixture(t)
	username := "site-operator-" + uuid.NewString()
	userID := f.addUser(t, username, false)
	site, err := sqlc.New(f.db).CreateSite(t.Context(), sqlc.CreateSiteParams{OrgID: f.org, Name: "Authorized site", Slug: "authorized"})
	require.NoError(t, err)
	role, err := authz.NewService(f.db, nil).CreateCustomRole(t.Context(), f.host, f.org, "Site observer", "", []string{authz.PermSiteRead, authz.PermFleetRead, authz.PermMinerRead})
	require.NoError(t, err)
	_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: userID, OrganizationID: f.org, RoleID: role.ID, ScopeType: "site", ScopeID: sql.NullInt64{Int64: site.ID, Valid: true}})
	require.NoError(t, err)
	customer := f.account(t, "Private customer", f.host, domain.AllCapabilities)
	login := f.login(t, username)
	require.Empty(t, login.Msg.UserInfo.Permissions, "site authority must not become organization-wide UI permission")
	cookie := login.Header().Get("Set-Cookie")
	listed, err := f.client.ListAccounts(t.Context(), rpcRequest(&pb.ListAccountsRequest{}, cookie, ""))
	require.NoError(t, err)
	require.True(t, listed.Msg.HostAccess, "site-only operators retain entry to their existing Fleet pages")
	require.Empty(t, listed.Msg.Accounts, "Fleet entry does not create hosted-account membership")
	_, err = f.client.GetAccount(t.Context(), rpcRequest(&pb.GetAccountRequest{AccountId: customer.AccountID}, cookie, customer.AccountID))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "workspace entry does not grant private account access")
}
