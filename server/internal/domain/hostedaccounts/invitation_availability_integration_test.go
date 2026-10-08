package hostedaccounts_test

import (
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/stretchr/testify/require"
)

func TestInvitationListingMatchesLiveAcceptanceAuthority(t *testing.T) {
	for _, source := range []string{"direct", "parent", "root"} {
		t.Run(source, func(t *testing.T) {
			f := setup(t)
			parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
			child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
			require.NoError(t, err)
			inviter := f.user
			switch source {
			case "direct":
				require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, child.AccountID, inviter, hostedaccounts.RoleAdministrator))
			case "parent":
				require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID, hostedaccounts.AllCapabilities))
			case "root":
				inviter = f.host
				require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, f.root, hostedaccounts.AllCapabilities))
				require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, child.AccountID, f.host))
			}
			svc := invitations(f)
			issue := func() hostedaccounts.IssuedInvitation {
				t.Helper()
				issued, err := svc.Create(t.Context(), inviter, f.org, child.AccountID, hostedaccounts.RoleViewer, time.Hour)
				require.NoError(t, err)
				require.True(t, issued.Invitation.Available)
				return issued
			}
			check := func(issued hostedaccounts.IssuedInvitation, available bool) {
				t.Helper()
				rows, _, err := svc.List(t.Context(), f.host, f.org, child.AccountID, 100, "")
				require.NoError(t, err)
				found := false
				for _, row := range rows {
					if row.InvitationID == issued.Invitation.InvitationID {
						found = true
						require.Equal(t, available, row.Available)
					}
				}
				require.True(t, found)
				_, err = svc.Preview(t.Context(), issued.Token)
				if available {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
			stale := issue()
			check(stale, true)
			// Even an unchanged root-policy save invalidates the authority witness.
			require.NoError(t, f.svc.ConfigureSites(t.Context(), f.host, f.org, f.root, false, nil))
			check(stale, false)
			rows, _, err := svc.List(t.Context(), f.host, f.org, child.AccountID, 100, "")
			require.NoError(t, err)
			require.False(t, rows[0].RevokedAt.Valid)
			require.False(t, rows[0].AcceptedAt.Valid)
			require.True(t, rows[0].ExpiresAt.After(time.Now()))
			usable := issue()
			check(usable, true)
			require.NoError(t, svc.Revoke(t.Context(), f.host, f.org, child.AccountID, usable.Invitation.InvitationID))
			check(usable, false)
			expired := issue()
			_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_invitation SET created_at=now()-interval '2 hours', expires_at=now()-interval '1 hour' WHERE invitation_id=$1`, expired.Invitation.InvitationID)
			require.NoError(t, err)
			check(expired, false)
			accepted := issue()
			_, err = svc.Accept(t.Context(), accepted.Token, addUser(t, f.db, f.org), f.org)
			require.NoError(t, err)
			check(accepted, false)
			altered := issue()
			require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, false))
			check(altered, false)
		})
	}
}

func TestInvitationListingDoesNotResurrectRevokedInviter(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleAdministrator))
	svc := invitations(f)
	issued, err := svc.Create(t.Context(), f.user, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, account.AccountID, f.user))
	for _, restore := range []bool{false, true} {
		if restore {
			require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleAdministrator))
		}
		rows, _, err := svc.List(t.Context(), f.host, f.org, account.AccountID, 25, "")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.False(t, rows[0].Available)
		_, err = svc.Preview(t.Context(), issued.Token)
		require.Error(t, err)
	}
}
