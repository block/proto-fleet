package hostedaccounts_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestCreateChildRejectsAdministratorRoleBeyondCreatorSources(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	limited := hostedaccounts.View | hostedaccounts.ManageMembers | hostedaccounts.Delegate
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, parent.AccountID, f.root, limited))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, parent.AccountID, limited, false))
	recipient := addUser(t, f.db, f.org)
	child, err := f.svc.CreateChild(t.Context(), f.host, f.org, parent.AccountID, "Unbacked administrator", recipient, limited)
	if err == nil {
		require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, parent.AccountID, hostedaccounts.AllCapabilities, false))
		require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, false))
		access, resolveErr := f.svc.Resolve(t.Context(), recipient, f.org, child.AccountID)
		require.NoError(t, resolveErr)
		require.Equal(t, hostedaccounts.AllCapabilities, access.Capabilities, "latent role expands after the ceiling is restored")
		creator, resolveErr := f.svc.Resolve(t.Context(), f.host, f.org, parent.AccountID)
		require.NoError(t, resolveErr)
		require.Equal(t, limited, creator.Capabilities, "creator never held the newly minted pool powers")
	}
	require.True(t, fleeterror.IsForbiddenError(err), "narrow source authority cannot mint a durable administrator: %v", err)
	var children, memberships int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM hosted_account WHERE parent_id=$1`, parent.ID).Scan(&children))
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM hosted_account_membership WHERE user_id=$1 AND organization_id=$2`, recipient, f.org).Scan(&memberships))
	require.Zero(t, children, "rejected creation persists no child")
	require.Zero(t, memberships, "rejected creation persists no membership")
}

func TestCreateChildAllowsFullSourcesUnderReducedHierarchyCeiling(t *testing.T) {
	for _, source := range []string{"direct_administrator", "full_root_grant"} {
		t.Run(source, func(t *testing.T) {
			f := setup(t)
			limited := hostedaccounts.View | hostedaccounts.ManageMembers | hostedaccounts.Delegate
			parent := f.client(t, "Reduced parent", f.user, hostedaccounts.AllCapabilities)
			actor := f.user
			if source == "full_root_grant" {
				require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, parent.AccountID, f.root, hostedaccounts.AllCapabilities))
				actor = f.host
			}
			require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, parent.AccountID, limited, false))
			recipient := addUser(t, f.db, f.org)
			child, err := f.svc.CreateChild(t.Context(), actor, f.org, parent.AccountID, "Permitted child", recipient, limited)
			require.NoError(t, err, "full source authority may grant a durable role while hierarchy temporarily limits it")
			access, err := f.svc.Resolve(t.Context(), recipient, f.org, child.AccountID)
			require.NoError(t, err)
			require.Equal(t, hostedaccounts.RoleAdministrator, access.Role)
			require.Equal(t, limited, access.Capabilities)
		})
	}
}

func TestCreateChildPreservesRootHostAdministrationUnderReducedCeiling(t *testing.T) {
	f := setup(t)
	limited := hostedaccounts.View | hostedaccounts.ManageMembers | hostedaccounts.Delegate
	_, err := f.db.ExecContext(t.Context(), `UPDATE hosted_account SET capabilities=$1 WHERE organization_id=$2 AND depth=0`, limited, f.org)
	require.NoError(t, err)
	child, err := f.svc.CreateChild(t.Context(), f.host, f.org, f.root, "Root-delegated customer", f.user, limited)
	require.NoError(t, err, "root host permission remains the authority for initial customer administration")
	access, err := f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.NoError(t, err)
	require.Equal(t, hostedaccounts.RoleAdministrator, access.Role)
	require.Equal(t, limited, access.Capabilities)
	_, err = f.svc.CreateChild(t.Context(), f.user, f.org, f.root, "Not a host administrator", f.user, limited)
	require.True(t, fleeterror.IsForbiddenError(err), "root creation still requires host account management permission")
}
