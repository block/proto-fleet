package hostedaccounts_test

import (
	"context"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestExplicitAncestorGrantsArePrivateAndRevocable(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	_, err = f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.Error(t, err)
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID, hostedaccounts.View))
	access, err := f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.NoError(t, err)
	require.True(t, access.Has(hostedaccounts.View))
	require.False(t, access.Has(hostedaccounts.ManageMembers))
	require.NoError(t, f.svc.RevokeAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID))
	_, err = f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.Error(t, err)
	sibling := f.client(t, "Sibling", f.host, hostedaccounts.AllCapabilities)
	require.Error(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, sibling.AccountID, hostedaccounts.View))
}

func TestDirectAndAncestorCapabilitiesCombineWithoutInheritingSourceRole(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, child.AccountID, f.user, hostedaccounts.RoleViewer))
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID, hostedaccounts.View|hostedaccounts.ManagePools))
	access, err := f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.NoError(t, err)
	require.True(t, access.Has(hostedaccounts.ManagePools))
	require.False(t, access.Has(hostedaccounts.ChangePools))
	require.False(t, access.Has(hostedaccounts.ManageMembers))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, parent.AccountID, hostedaccounts.View|hostedaccounts.ManageMembers|hostedaccounts.Delegate, false))
	access, err = f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.NoError(t, err)
	require.False(t, access.Has(hostedaccounts.ManagePools), "live ceiling bounds the union")
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, parent.AccountID, hostedaccounts.AllCapabilities, true))
	_, err = f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.Error(t, err, "suspension denies both authority paths")
}

func TestSitePoliciesIntersectAndCannotGrantForeignOrganizationSites(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	var first, second int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site (name,org_id,slug) VALUES ('First',$1,'first') RETURNING id`, f.org).Scan(&first))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site (name,org_id,slug) VALUES ('Second',$1,'second') RETURNING id`, f.org).Scan(&second))
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.host, f.org, parent.AccountID, true, []int64{first}))
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.host, f.org, child.AccountID, true, []int64{second}))
	sites, err := f.svc.SitePolicy(t.Context(), f.host, f.org, child.AccountID)
	require.NoError(t, err)
	require.True(t, sites.EffectiveRestricted)
	require.Empty(t, sites.EffectiveSiteIDs)
	require.NoError(t, f.svc.ConfigureSites(t.Context(), f.host, f.org, child.AccountID, false, nil))
	sites, err = f.svc.SitePolicy(t.Context(), f.host, f.org, child.AccountID)
	require.NoError(t, err)
	require.False(t, sites.Restricted)
	require.Equal(t, []int64{first}, sites.EffectiveSiteIDs, "child cannot widen ancestor site policy")
	var foreignOrg, foreign int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO organization (org_id,name) VALUES ($1,'Other hosting organization') RETURNING id`, uuid.NewString()).Scan(&foreignOrg))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site (name,org_id,slug) VALUES ('Foreign',$1,'foreign') RETURNING id`, foreignOrg).Scan(&foreign))
	require.Error(t, f.svc.ConfigureSites(t.Context(), f.host, f.org, child.AccountID, true, []int64{foreign}))
	sites, err = f.svc.SitePolicy(t.Context(), f.host, f.org, child.AccountID)
	require.NoError(t, err)
	require.False(t, sites.Restricted, "rejected site edits roll back")
}

func TestParentSiteCeilingSurvivesChildPolicyEdits(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	parentCtx, err := hostedaccounts.WithRequestAccountID(t.Context(), parent.AccountID)
	require.NoError(t, err)
	childCtx, err := hostedaccounts.WithRequestAccountID(t.Context(), child.AccountID)
	require.NoError(t, err)
	var first, second int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site (name,org_id,slug) VALUES ('First',$1,'first') RETURNING id`, f.org).Scan(&first))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `INSERT INTO site (name,org_id,slug) VALUES ('Second',$1,'second') RETURNING id`, f.org).Scan(&second))
	allSites := []int64{first, second}
	require.NoError(t, f.svc.ConfigureSites(parentCtx, f.user, f.org, parent.AccountID, true, allSites))
	require.NoError(t, f.svc.ConfigureSites(parentCtx, f.user, f.org, child.AccountID, true, []int64{first}))
	require.NoError(t, f.svc.ConfigureSites(childCtx, f.host, f.org, child.AccountID, false, nil))
	check := func(ctx context.Context, actor int64, restricted bool, configured, effective []int64) {
		t.Helper()
		sites, err := f.svc.SitePolicy(ctx, actor, f.org, child.AccountID)
		require.NoError(t, err)
		require.Equal(t, restricted, sites.Restricted)
		require.ElementsMatch(t, configured, sites.SiteIDs)
		require.True(t, sites.EffectiveRestricted)
		require.ElementsMatch(t, effective, sites.EffectiveSiteIDs)
	}
	check(childCtx, f.host, false, nil, []int64{first})
	check(parentCtx, f.user, true, []int64{first}, []int64{first})
	// Even when the parent also holds private child authority, the selected
	// parent workspace must keep reading and writing the delegated ceiling.
	require.NoError(t, f.svc.SetMembership(childCtx, f.host, f.org, child.AccountID, f.user, hostedaccounts.RoleAdministrator))
	require.NoError(t, f.svc.ConfigureSites(childCtx, f.host, f.org, child.AccountID, true, []int64{second}))
	check(childCtx, f.host, true, []int64{second}, nil)
	check(parentCtx, f.user, true, []int64{first}, nil)
	require.NoError(t, f.svc.ConfigureSites(parentCtx, f.user, f.org, child.AccountID, true, allSites))
	check(childCtx, f.host, true, []int64{second}, []int64{second})
	check(parentCtx, f.user, true, allSites, []int64{second})
	require.NoError(t, f.svc.ConfigureSites(parentCtx, f.user, f.org, child.AccountID, false, nil))
	check(childCtx, f.host, true, []int64{second}, []int64{second})
	check(parentCtx, f.user, false, nil, []int64{second})
	require.NoError(t, f.svc.ConfigureSites(childCtx, f.host, f.org, child.AccountID, false, nil))
	check(childCtx, f.host, false, nil, allSites)
	// A restricted empty ceiling denies every site and cannot be cleared by
	// the child. Policy edits also change the invitation authority witness.
	q := sqlc.New(f.db)
	before, err := q.ResolveHostedAccountAuthority(childCtx, sqlc.ResolveHostedAccountAuthorityParams{OrganizationID: f.org, AccountID: child.AccountID, UserID: f.host})
	require.NoError(t, err)
	require.NoError(t, f.svc.ConfigureSites(parentCtx, f.user, f.org, child.AccountID, true, nil))
	after, err := q.ResolveHostedAccountAuthority(childCtx, sqlc.ResolveHostedAccountAuthorityParams{OrganizationID: f.org, AccountID: child.AccountID, UserID: f.host})
	require.NoError(t, err)
	require.NotEqual(t, before.AuthorityState, after.AuthorityState)
	require.NoError(t, f.svc.ConfigureSites(childCtx, f.host, f.org, child.AccountID, false, nil))
	check(childCtx, f.host, false, nil, nil)
}

func TestInvitationAuthorityWitnessSurvivesRenameButNotRevocationOrPolicyCycles(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.host, hostedaccounts.AllCapabilities)
	q := sqlc.New(f.db)
	state := func() string {
		row, err := q.ResolveHostedAccountAuthority(t.Context(), sqlc.ResolveHostedAccountAuthorityParams{OrganizationID: f.org, AccountID: a.AccountID, UserID: f.host})
		require.NoError(t, err)
		return row.AuthorityState
	}
	before := state()
	require.NoError(t, f.svc.Rename(t.Context(), f.host, f.org, a.AccountID, "New label"))
	require.Equal(t, before, state(), "labels must not invalidate authority")
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, a.AccountID, f.user, hostedaccounts.RoleAdministrator))
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.user, f.org, a.AccountID, f.host))
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, a.AccountID, f.host, hostedaccounts.RoleAdministrator))
	require.NotEqual(t, before, state(), "revoked and regranted membership must not resurrect invitations")
	before = state()
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.AllCapabilities, true))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.AllCapabilities, false))
	require.NotEqual(t, before, state(), "suspend/resume does not resurrect invitations")
}

func TestExplicitAncestorCanReplaceDirectAdministrator(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID, hostedaccounts.DefaultCapabilities))
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, child.AccountID, f.host))
	require.Error(t, f.svc.RevokeAncestorGrant(t.Context(), f.user, f.org, child.AccountID, parent.AccountID), "sole ancestor administration must survive")
	require.Error(t, f.svc.SetMembership(t.Context(), f.user, f.org, parent.AccountID, f.user, hostedaccounts.RoleViewer), "parent demotion cannot strand its child")
	require.Error(t, f.svc.SetMembership(t.Context(), f.user, f.org, child.AccountID, f.host, hostedaccounts.RoleAdministrator), "a limited ancestor grant cannot assign a full durable administrator role")
	// Establish full source authority before narrowing the mutable ceiling.
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, false))
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID, hostedaccounts.AllCapabilities))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.DefaultCapabilities, false))
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, child.AccountID, f.host, hostedaccounts.RoleAdministrator))
	require.NoError(t, f.svc.RevokeAncestorGrant(t.Context(), f.user, f.org, child.AccountID, parent.AccountID))
}

func TestHostExplicitGrantDoesNotCreateImplicitCustomerAccess(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Private", f.user, hostedaccounts.DefaultCapabilities)
	_, err := f.svc.ListMembers(t.Context(), f.host, f.org, a.AccountID)
	require.Error(t, err)
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, a.AccountID, f.root, hostedaccounts.View|hostedaccounts.ManageMembers))
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, a.AccountID, f.user))
	members, err := f.svc.ListMembers(t.Context(), f.host, f.org, a.AccountID)
	require.NoError(t, err)
	require.Empty(t, members)
	require.Error(t, f.svc.RevokeAncestorGrant(t.Context(), f.host, f.org, a.AccountID, f.root))
	_, err = f.db.ExecContext(t.Context(), `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`, f.host, f.org)
	require.NoError(t, err)
	_, err = f.svc.Resolve(t.Context(), f.host, f.org, a.AccountID)
	require.Error(t, err, "host grant requires live host organization authority")
}

func TestPrivateAccountListsAndRename(t *testing.T) {
	f := setup(t)
	a := f.client(t, "First", f.user, hostedaccounts.DefaultCapabilities)
	_ = f.client(t, "Private sibling", f.host, hostedaccounts.DefaultCapabilities)
	list, err := f.svc.ListAccounts(t.Context(), f.user, f.org)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, a.AccountID, list[0].AccountID)
	require.NoError(t, f.svc.Rename(t.Context(), f.user, f.org, a.AccountID, "Renamed"))
	list, err = f.svc.ListAccounts(t.Context(), f.user, f.org)
	require.NoError(t, err)
	require.Equal(t, "Renamed", list[0].Name)
	require.Error(t, f.svc.Rename(t.Context(), f.host, f.org, a.AccountID, "Forbidden"))
}

func TestSelectedCustomerScopeDoesNotBorrowHostRecoveryAuthority(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.user, hostedaccounts.AllCapabilities)
	ctx, err := hostedaccounts.WithRequestAccountID(t.Context(), a.AccountID)
	require.NoError(t, err)
	require.Error(t, f.svc.SetAncestorGrant(ctx, f.host, f.org, a.AccountID, f.root, hostedaccounts.View|hostedaccounts.ManageMembers))
	require.Error(t, f.svc.ConfigureSites(ctx, f.host, f.org, a.AccountID, true, nil))
	_, err = f.svc.SitePolicy(ctx, f.host, f.org, a.AccountID)
	require.Error(t, err)
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, a.AccountID, f.root, hostedaccounts.View|hostedaccounts.ManageMembers))
	access, err := f.svc.Resolve(ctx, f.host, f.org, a.AccountID)
	require.NoError(t, err)
	require.True(t, access.Has(hostedaccounts.ManageMembers))
}

func TestDelegatedMembershipManagerCannotMintBroaderPrivateRoles(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, child.AccountID, f.user, hostedaccounts.RoleViewer))
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID, hostedaccounts.View|hostedaccounts.ManageMembers))
	for _, role := range []hostedaccounts.Role{hostedaccounts.RolePoolOperator, hostedaccounts.RoleAdministrator} {
		require.Error(t, f.svc.SetMembership(t.Context(), f.user, f.org, child.AccountID, f.user, role), "a team manager cannot promote themselves beyond their live private authority")
		_, err := invitations(f).Create(t.Context(), f.user, f.org, child.AccountID, role, 0)
		require.Error(t, err, "a team manager cannot issue an invitation granting broader authority")
	}
	recipient := addUser(t, f.db, f.org)
	invite, err := invitations(f).Create(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.RoleViewer, 0)
	require.NoError(t, err)
	_, err = invitations(f).Accept(t.Context(), invite.Token, recipient, f.org)
	require.NoError(t, err)
	require.NoError(t, f.svc.RevokeAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID))
	access, err := f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.NoError(t, err)
	require.Equal(t, hostedaccounts.View, access.Capabilities)
}

func TestMutableCeilingCannotBroadenDurableMembership(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	manager := hostedaccounts.View | hostedaccounts.ManageMembers
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, child.AccountID, f.user, hostedaccounts.RoleViewer))
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID, manager))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, manager, false))
	for _, role := range []hostedaccounts.Role{hostedaccounts.RoleAdministrator, hostedaccounts.RolePoolOperator} {
		require.Error(t, f.svc.SetMembership(t.Context(), f.user, f.org, child.AccountID, f.user, role), "the full durable role must fit the manager's source authority")
	}
	// A direct full administrator remains able to assign a full role while
	// the account ceiling temporarily restricts its effective capabilities.
	recipient := addUser(t, f.db, f.org)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, child.AccountID, recipient, hostedaccounts.RoleAdministrator))
	access, err := f.svc.Resolve(t.Context(), recipient, f.org, child.AccountID)
	require.NoError(t, err)
	require.Equal(t, manager, access.Capabilities)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, false))
	access, err = f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.NoError(t, err)
	require.Equal(t, manager, access.Capabilities, "restoring the ceiling must not broaden the limited manager")
	access, err = f.svc.Resolve(t.Context(), recipient, f.org, child.AccountID)
	require.NoError(t, err)
	require.Equal(t, hostedaccounts.AllCapabilities, access.Capabilities)
	require.NoError(t, f.svc.RevokeAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID))
	access, err = f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.NoError(t, err)
	require.Equal(t, hostedaccounts.View, access.Capabilities, "rejected promotion leaves only the existing viewer membership")
}

func TestChildListsReturnOnlyExplicitPrivateAuthority(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	owned, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Owned", f.user, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	private, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Private", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	children, err := f.svc.ListChildren(t.Context(), f.user, f.org, parent.AccountID)
	require.NoError(t, err)
	require.Len(t, children, 2)
	for _, child := range children {
		if child.AccountID == owned.AccountID {
			require.Equal(t, hostedaccounts.RoleAdministrator, child.Role)
			require.Equal(t, hostedaccounts.DefaultCapabilities, child.EffectiveCapabilities)
		} else {
			require.Equal(t, private.AccountID, child.AccountID)
			require.Empty(t, child.Role)
			require.Zero(t, child.EffectiveCapabilities, "custody alone does not grant private access")
		}
	}
}
