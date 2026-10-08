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
		{name: "omitted administrator defaults to caller", wantAdmin: f.host},
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
			if tc.administrator == nil {
				require.Equal(t, pb.AccountRole_ACCOUNT_ROLE_ADMINISTRATOR, response.Msg.Account.Role)
				require.Equal(t, uint64(domain.DefaultCapabilities), response.Msg.Account.EffectiveCapabilities)
			}
		})
	}
}
