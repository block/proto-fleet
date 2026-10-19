package hostedaccounts_test

import (
	"testing"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/hosted/v1"
	domain "github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHostedRPCConfigureAccountRequiresSelectedParentWorkspace(t *testing.T) {
	f := newRPCFixture(t)
	username := "shared-customer-admin"
	actor := f.addUser(t, username, false)
	parentA := f.account(t, "Parent A", actor, domain.AllCapabilities)
	parentB := f.account(t, "Parent B", actor, domain.AllCapabilities)
	child, err := f.accounts.CreateChild(t.Context(), actor, f.org, parentB.AccountID, "B child", actor, domain.DefaultCapabilities)
	require.NoError(t, err)
	cookie := f.login(t, username).Header().Get("Set-Cookie")
	type policy struct {
		capabilities int64
		suspended    bool
		revision     int64
	}
	stored := func() policy {
		t.Helper()
		var result policy
		require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT capabilities, suspended, revision FROM hosted_account WHERE organization_id=$1 AND account_id=$2`, f.org, child.AccountID).Scan(&result.capabilities, &result.suspended, &result.revision))
		return result
	}
	before := stored()
	capabilities := uint64(domain.View | domain.ManageMembers)
	for _, namedParent := range []string{parentA.AccountID, parentB.AccountID} {
		_, err := f.client.ConfigureAccount(t.Context(), rpcRequest(&pb.ConfigureAccountRequest{
			AccountId: namedParent, ChildAccountId: child.AccountID, Capabilities: capabilities, Suspended: true,
		}, cookie, parentA.AccountID))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "authority over both parents must not let workspace A mutate B's child")
		require.Equal(t, before, stored(), "denied RPC preserves the child's capabilities, suspension, and revision")
	}
	_, err = f.client.ConfigureAccount(t.Context(), rpcRequest(&pb.ConfigureAccountRequest{
		AccountId: parentB.AccountID, ChildAccountId: child.AccountID, Capabilities: capabilities, Suspended: true,
	}, cookie, parentB.AccountID))
	require.NoError(t, err, "selecting B allows the same actor to configure its directly delegated child")
	require.Equal(t, policy{capabilities: int64(domain.View | domain.ManageMembers), suspended: true, revision: before.revision + 1}, stored())
}

func TestHostedRPCCreateAccountInitialAdministrator(t *testing.T) {
	f := newRPCFixture(t)
	eligible := f.addUser(t, "eligible-administrator", false)
	var eligibleExternal string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT user_id FROM "user" WHERE id=$1`, eligible).Scan(&eligibleExternal))
	foreignExternal := uuid.NewString()
	var foreignOrg, foreignUser int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization (org_id,name) VALUES ($1,'Foreign host') RETURNING id`, uuid.NewString()).Scan(&foreignOrg))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO "user" (user_id,username,password_hash) VALUES ($1,$2,'hash') RETURNING id`, foreignExternal, uuid.NewString()).Scan(&foreignUser))
	_, err := f.db.ExecContext(t.Context(), `INSERT INTO user_organization (user_id,organization_id,role_id) VALUES ($1,$2,NULL)`, foreignUser, foreignOrg)
	require.NoError(t, err)
	unknown := uuid.NewString()
	cases := []struct {
		name          string
		administrator *string
		wantAdmin     int64
		wantCode      connect.Code
	}{
		{name: "eligible explicit administrator", administrator: &eligibleExternal, wantAdmin: eligible},
		{name: "foreign organization administrator", administrator: &foreignExternal, wantCode: connect.CodePermissionDenied},
		{name: "unknown administrator", administrator: &unknown, wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var before int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account WHERE organization_id=$1 AND parent_id=(SELECT id FROM hosted_account WHERE account_id=$2)`, f.org, f.root).Scan(&before))
			response, err := f.client.CreateAccount(t.Context(), rpcRequest(&pb.CreateAccountRequest{
				AccountId: f.root, Name: tc.name, AdministratorUserId: tc.administrator, Capabilities: uint64(domain.DefaultCapabilities),
			}, f.cookie, f.root))
			var after int
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account WHERE organization_id=$1 AND parent_id=(SELECT id FROM hosted_account WHERE account_id=$2)`, f.org, f.root).Scan(&after))
			if tc.wantAdmin == 0 {
				require.Equal(t, tc.wantCode, connect.CodeOf(err))
				require.Nil(t, response)
				require.Equal(t, before, after, "invalid administrator must leave no child account")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, response.Msg.Account)
			require.Equal(t, before+1, after)
			require.Equal(t, f.root, response.Msg.Account.ParentAccountId)
			var administrator int64
			var role string
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT m.user_id, m.role FROM hosted_account_membership m JOIN hosted_account a ON a.id=m.account_id AND a.organization_id=m.organization_id WHERE a.organization_id=$1 AND a.account_id=$2 AND m.revoked_at IS NULL`, f.org, response.Msg.Account.AccountId).Scan(&administrator, &role))
			require.Equal(t, tc.wantAdmin, administrator)
			require.Equal(t, string(domain.RoleAdministrator), role)
		})
	}
}

func TestHostedRPCCreatingClientDoesNotAddHostToClientTeam(t *testing.T) {
	f := newRPCFixture(t)
	response, err := f.client.CreateAccount(t.Context(), rpcRequest(&pb.CreateAccountRequest{
		AccountId: f.root, Name: "Client with separate host administration", Capabilities: uint64(domain.AllCapabilities),
	}, f.cookie, f.root))
	require.NoError(t, err)
	require.NotNil(t, response.Msg.Account)
	var memberships int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_membership m JOIN hosted_account a ON a.id=m.account_id AND a.organization_id=m.organization_id WHERE a.organization_id=$1 AND a.account_id=$2 AND m.revoked_at IS NULL`, f.org, response.Msg.Account.AccountId).Scan(&memberships))
	require.Zero(t, memberships, "creating a client must not create a customer login for the host")
	require.Equal(t, pb.AccountRole_ACCOUNT_ROLE_UNSPECIFIED, response.Msg.Account.Role)
	accountID := response.Msg.Account.AccountId
	require.Equal(t, uint64(domain.AllCapabilities), response.Msg.Account.EffectiveCapabilities)
	members, err := f.client.ListMembers(t.Context(), rpcRequest(&pb.ListMembersRequest{AccountId: accountID}, f.cookie, accountID))
	require.NoError(t, err)
	require.Empty(t, members.Msg.Members)
	otherHost := f.addUser(t, "another-host-administrator", true)
	access, err := f.accounts.Resolve(t.Context(), otherHost, f.org, accountID)
	require.NoError(t, err)
	require.Equal(t, domain.AllCapabilities, access.Capabilities)
	require.Empty(t, access.Role, "host access is not a client team role")
	outsider := f.addUser(t, "unaffiliated-user", false)
	_, err = f.accounts.Resolve(t.Context(), outsider, f.org, accountID)
	require.Error(t, err, "organization affiliation alone does not confer host access")
	clients, err := f.accounts.ListChildren(t.Context(), f.host, f.org, f.root)
	require.NoError(t, err)
	for _, client := range clients {
		if client.AccountID == response.Msg.Account.AccountId {
			require.NotNil(t, client.ManagementSummary)
			require.Zero(t, client.ManagementSummary.LoginCount, "client login counts include only its actual team")
		}
	}
	issued := f.invite(t, accountID, pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR)
	f.register(t, issued.Token, "actual-client-admin")
	members, err = f.client.ListMembers(t.Context(), rpcRequest(&pb.ListMembersRequest{AccountId: accountID}, f.cookie, accountID))
	require.NoError(t, err)
	require.Len(t, members.Msg.Members, 1)
	require.Equal(t, "actual-client-admin", members.Msg.Members[0].Username)
	clients, err = f.accounts.ListChildren(t.Context(), f.host, f.org, f.root)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	require.EqualValues(t, 1, clients[0].ManagementSummary.LoginCount)
	// Replacing the sole host role immediately removes inherited authority;
	// the independent client administrator remains a member.
	_, err = f.db.ExecContext(t.Context(), `UPDATE user_organization_role SET deleted_at=NOW() WHERE organization_id=$1 AND user_id=$2`, f.org, otherHost)
	require.NoError(t, err)
	_, err = f.accounts.Resolve(t.Context(), otherHost, f.org, accountID)
	require.Error(t, err)
}

func TestHostedRPCDefaultSubclientAdministrationFollowsCreatorPersona(t *testing.T) {
	f := newRPCFixture(t)
	created, err := f.client.CreateAccount(t.Context(), rpcRequest(&pb.CreateAccountRequest{AccountId: f.root, Name: "Parent", Capabilities: uint64(domain.AllCapabilities)}, f.cookie, f.root))
	require.NoError(t, err)
	parentID := created.Msg.Account.AccountId
	issued := f.invite(t, parentID, pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR)
	customer := f.register(t, issued.Token, "customer-subclient-admin")
	for _, tc := range []struct {
		name, cookie string
		members      int
	}{
		{"host-created subclient", f.cookie, 0},
		{"customer-created subclient", customer.Header().Get("Set-Cookie"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub, err := f.client.CreateAccount(t.Context(), rpcRequest(&pb.CreateAccountRequest{AccountId: parentID, Name: tc.name, Capabilities: uint64(domain.AllCapabilities)}, tc.cookie, parentID))
			require.NoError(t, err)
			subID := sub.Msg.Account.AccountId
			members, err := f.client.ListMembers(t.Context(), rpcRequest(&pb.ListMembersRequest{AccountId: subID}, tc.cookie, subID))
			require.NoError(t, err)
			require.Len(t, members.Msg.Members, tc.members)
			if tc.members == 0 {
				require.Equal(t, pb.AccountRole_ACCOUNT_ROLE_UNSPECIFIED, sub.Msg.Account.Role)
			} else {
				require.Equal(t, "customer-subclient-admin", members.Msg.Members[0].Username)
			}
		})
	}
}
