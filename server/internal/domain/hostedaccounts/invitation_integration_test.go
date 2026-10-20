package hostedaccounts_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func invitations(f fixture) *hostedaccounts.InvitationService {
	sessions := session.NewService(session.Config{Duration: time.Hour, IDBytes: 32, CookieName: "fleet_session"}, sqlstores.NewSQLSessionStore(f.db))
	return hostedaccounts.NewInvitationService(f.svc, sessions)
}

func TestInvitationRegistersRolelessIdentityAtomicallyAndOnlyOnce(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	svc := invitations(f)
	issued, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RolePoolOperator, time.Hour)
	require.NoError(t, err)
	require.Regexp(t, `^(?:[A-HJ-NP-Z2-9]{4}-){3}[A-HJ-NP-Z2-9]{4}$`, issued.Token)
	preview, err := svc.Preview(t.Context(), issued.Token)
	require.NoError(t, err)
	require.Equal(t, "Customer", preview.AccountName)
	registered, err := svc.Register(t.Context(), issued.Token, "customer-without-email", "password123", "browser", "127.0.0.1")
	require.NoError(t, err)
	require.Equal(t, account.AccountID, registered.AccountID)
	require.NotEmpty(t, registered.Cookie.Value)
	q := sqlc.New(f.db)
	user, err := q.GetUserByUsername(t.Context(), "customer-without-email")
	require.NoError(t, err)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("password123")))
	role, err := sqlstores.NewSQLUserStore(f.db).GetUserRoleName(t.Context(), user.ID, f.org)
	require.NoError(t, err)
	require.Empty(t, role)
	effective, err := authz.LoadEffectiveTx(t.Context(), q, user.ID, f.org)
	require.NoError(t, err)
	require.Empty(t, effective.Keys())
	access, err := f.svc.Resolve(t.Context(), user.ID, f.org, account.AccountID)
	require.NoError(t, err)
	require.True(t, access.Has(hostedaccounts.ChangePools))
	_, err = svc.Register(t.Context(), issued.Token, "second-user", "password123", "", "")
	require.Error(t, err)
	_, err = q.GetUserByUsername(t.Context(), "second-user")
	require.Error(t, err, "replayed invitation creates no identity")
	var storedToken string
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT token_hash FROM hosted_account_invitation WHERE invitation_id = $1`, issued.Invitation.InvitationID).Scan(&storedToken))
	require.NotEqual(t, issued.Token, storedToken)
}

func TestInvitationRechecksInviterAndDoesNotResurrectAuthority(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	svc := invitations(f)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleAdministrator))
	issued, err := svc.Create(t.Context(), f.user, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, account.AccountID, f.user))
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, account.AccountID, f.user, hostedaccounts.RoleAdministrator))
	_, err = svc.Register(t.Context(), issued.Token, "stale-authority", "password123", "", "")
	require.Error(t, err, "reinstating an inviter cannot resurrect an old invitation")
	_, err = sqlc.New(f.db).GetUserByUsername(t.Context(), "stale-authority")
	require.Error(t, err)
}

func TestMutableCeilingCannotBroadenInvitationIssuance(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	manager := hostedaccounts.View | hostedaccounts.ManageMembers
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID, manager))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, manager, false))
	svc := invitations(f)
	for _, role := range []hostedaccounts.Role{hostedaccounts.RoleAdministrator, hostedaccounts.RolePoolOperator} {
		_, err := svc.Create(t.Context(), f.user, f.org, child.AccountID, role, time.Hour)
		require.Error(t, err, "a reduced ceiling cannot mask an invitation's durable role")
	}
	var count int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM hosted_account_invitation WHERE account_id=$1`, child.ID).Scan(&count))
	require.Zero(t, count, "rejected invitations are not persisted")
	issued, err := svc.Create(t.Context(), f.host, f.org, child.AccountID, hostedaccounts.RoleAdministrator, time.Hour)
	require.NoError(t, err, "a full direct administrator may still invite a full role under a reduced ceiling")
	listed, _, err := svc.List(t.Context(), f.host, f.org, child.AccountID, 25, "")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.True(t, listed[0].Available, "effective ceiling must not replace the durable source check")
	recipient := addUser(t, f.db, f.org)
	_, err = svc.Accept(t.Context(), issued.Token, recipient, f.org)
	require.NoError(t, err)
	access, err := f.svc.Resolve(t.Context(), recipient, f.org, child.AccountID)
	require.NoError(t, err)
	require.Equal(t, manager, access.Capabilities)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, false))
	access, err = f.svc.Resolve(t.Context(), recipient, f.org, child.AccountID)
	require.NoError(t, err)
	require.Equal(t, hostedaccounts.AllCapabilities, access.Capabilities)
	access, err = f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.NoError(t, err)
	require.Equal(t, manager, access.Capabilities, "restoring the ceiling leaves the manager's grant limited")
}

func TestInvitationRejectsDurableRoleBeyondInviterSources(t *testing.T) {
	f := setup(t)
	parent := f.client(t, "Parent", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, parent.AccountID, "Child", f.host, hostedaccounts.AllCapabilities)
	require.NoError(t, err)
	manager := hostedaccounts.View | hostedaccounts.ManageMembers
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, child.AccountID, parent.AccountID, manager))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, manager, false))
	svc := invitations(f)
	issued, err := svc.Create(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	// Represent a broader invitation issued under the old check. The authority
	// fingerprint does not depend on the role and remains valid in this fixture.
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_invitation SET role='account_administrator' WHERE invitation_id=$1`, issued.Invitation.InvitationID)
	require.NoError(t, err)
	_, err = svc.Preview(t.Context(), issued.Token)
	require.Error(t, err)
	recipient := addUser(t, f.db, f.org)
	_, err = svc.Accept(t.Context(), issued.Token, recipient, f.org)
	require.Error(t, err, "acceptance must recheck the full role against the inviter's source authority")
	listed, _, err := svc.List(t.Context(), f.host, f.org, child.AccountID, 25, "")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.False(t, listed[0].Available, "listing must also reject durable roles beyond inviter sources")
	_, err = svc.Register(t.Context(), issued.Token, "broader-durable-role", "password123", "", "")
	require.Error(t, err)
	_, err = sqlc.New(f.db).GetUserByUsername(t.Context(), "broader-durable-role")
	require.Error(t, err, "rejected registration creates no identity")
	var unused bool
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT accepted_at IS NULL FROM hosted_account_invitation WHERE invitation_id=$1`, issued.Invitation.InvitationID).Scan(&unused))
	require.True(t, unused)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, child.AccountID, hostedaccounts.AllCapabilities, false))
	_, err = f.svc.Resolve(t.Context(), recipient, f.org, child.AccountID)
	require.Error(t, err, "rejected acceptance leaves no membership after ceiling restoration")
}

func TestInvitationExpiryRevocationAndConcurrentAcceptance(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	svc := invitations(f)
	expired, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_invitation SET created_at=now()-interval '2 hours', expires_at=now()-interval '1 hour' WHERE invitation_id=$1`, expired.Invitation.InvitationID)
	require.NoError(t, err)
	_, err = svc.Accept(t.Context(), expired.Token, f.user, f.org)
	require.Error(t, err)
	revoked, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	require.NoError(t, svc.Revoke(t.Context(), f.host, f.org, account.AccountID, revoked.Invitation.InvitationID))
	_, err = svc.Accept(t.Context(), revoked.Token, f.user, f.org)
	require.Error(t, err)
	issued, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	other := addUser(t, f.db, f.org)
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, user := range []int64{f.user, other} {
		wg.Go(func() { <-start; _, err := svc.Accept(t.Context(), issued.Token, user, f.org); results <- err })
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	var members int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM hosted_account_membership WHERE account_id=$1 AND user_id<>$2 AND revoked_at IS NULL`, account.ID, f.host).Scan(&members))
	require.Equal(t, 1, members)
}

func TestRootGrantedInvitationDoesNotSurviveHostPermissionRevocation(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.host, hostedaccounts.AllCapabilities)
	roles := authz.NewService(f.db, nil)
	role, err := roles.CreateCustomRole(t.Context(), f.host, f.org, "Account manager", "", []string{authz.PermFleetRead, authz.PermAccountManage})
	require.NoError(t, err)
	_, err = sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: role.ID, ScopeType: "org"})
	require.NoError(t, err)
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, a.AccountID, f.root, hostedaccounts.View|hostedaccounts.ManageMembers))
	svc := invitations(f)
	issued, err := svc.Create(t.Context(), f.user, f.org, a.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	_, err = roles.UpdateCustomRole(t.Context(), f.host, f.org, role.ID, "Account manager", "", []string{authz.PermFleetRead})
	require.NoError(t, err)
	_, err = svc.Preview(t.Context(), issued.Token)
	require.Error(t, err)
	listed, _, err := svc.List(t.Context(), f.host, f.org, a.AccountID, 25, "")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.False(t, listed[0].Available, "revoked host permission invalidates root-granted invitations")
	_, err = roles.UpdateCustomRole(t.Context(), f.host, f.org, role.ID, "Account manager", "", []string{authz.PermFleetRead, authz.PermAccountManage})
	require.NoError(t, err)
	_, err = svc.Accept(t.Context(), issued.Token, addUser(t, f.db, f.org), f.org)
	require.Error(t, err, "restoring a host role cannot resurrect the earlier invitation")
}

func TestInvitationRegistrationPreservesTransientDatabaseFailures(t *testing.T) {
	f := setup(t)
	account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	svc := invitations(f)
	issued, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, 0)
	require.NoError(t, err)
	_, err = f.db.ExecContext(t.Context(), `CREATE FUNCTION reject_invited_registration() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'registration interrupted' USING ERRCODE = '40001'; END $$; CREATE TRIGGER reject_invited_registration BEFORE INSERT ON "user" FOR EACH ROW EXECUTE FUNCTION reject_invited_registration()`)
	require.NoError(t, err)
	_, err = svc.Register(t.Context(), issued.Token, "interrupted-signup", "password123", "", "")
	require.Error(t, err)
	require.False(t, fleeterror.IsInvalidArgumentError(err), "transient storage errors are not unavailable usernames")
	require.True(t, db.IsRetryablePostgresError(err), "the original retryable SQL cause survives transaction retries")
	_, err = f.db.ExecContext(t.Context(), `DROP TRIGGER reject_invited_registration ON "user"; DROP FUNCTION reject_invited_registration()`)
	require.NoError(t, err)
	_, err = svc.Register(t.Context(), issued.Token, "interrupted-signup", "password123", "", "")
	require.NoError(t, err, "the rejected transaction must not consume the invitation")
}

func TestInvitationPreviewDoesNotWaitForAdministrationLocks(t *testing.T) {
	for _, state := range []string{"active", "expired", "revoked", "consumed"} {
		t.Run(state, func(t *testing.T) {
			f := setup(t)
			account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
			svc := invitations(f)
			issued, err := svc.Create(t.Context(), f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
			require.NoError(t, err)
			switch state {
			case "expired":
				_, err = f.db.ExecContext(t.Context(), `UPDATE hosted_account_invitation SET created_at=now()-interval '2 hours', expires_at=now()-interval '1 hour' WHERE invitation_id=$1`, issued.Invitation.InvitationID)
			case "revoked":
				err = svc.Revoke(t.Context(), f.host, f.org, account.AccountID, issued.Invitation.InvitationID)
			case "consumed":
				_, err = svc.Accept(t.Context(), issued.Token, f.user, f.org)
			}
			require.NoError(t, err)
			tx, err := f.db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer func() { require.NoError(t, tx.Rollback()) }()
			q := sqlc.New(tx)
			_, err = q.LockHostedAccountOrganization(t.Context(), f.org)
			require.NoError(t, err)
			_, err = q.LockActiveHostedAccountUser(t.Context(), sqlc.LockActiveHostedAccountUserParams{ID: f.host, OrganizationID: f.org})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			preview, err := svc.Preview(ctx, issued.Token)
			if state == "active" {
				require.NoError(t, err, "read-only preview must not queue behind mutation locks")
				require.Equal(t, "Customer", preview.AccountName)
			} else {
				require.True(t, fleeterror.IsFailedPreconditionError(err), "unavailable previews reject without queueing for administration locks: %v", err)
				_, err = svc.Accept(ctx, issued.Token, f.user, f.org)
				require.True(t, fleeterror.IsFailedPreconditionError(err), "unavailable acceptance must reject before mutation locks: %v", err)
			}
		})
	}
}
