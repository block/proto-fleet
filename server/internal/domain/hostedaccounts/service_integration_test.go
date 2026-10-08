package hostedaccounts_test

import (
	"database/sql"
	"sync"
	"testing"

	"connectrpc.com/authn"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	domainauth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
)

type fixture struct {
	db              *sql.DB
	svc             *hostedaccounts.Service
	org, host, user int64
	root            string
}

func setup(t *testing.T) fixture {
	t.Helper()
	conn := dbtest.GetTestDB(t)
	var org int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO organization (org_id, name) VALUES ($1, 'Hosting test') RETURNING id`, uuid.NewString()).Scan(&org))
	require.NoError(t, authz.Reconcile(t.Context(), conn))
	host := addUser(t, conn, org)
	var role int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id = $1 AND builtin_key = 'SUPER_ADMIN'`, org).Scan(&role))
	_, err := sqlc.New(conn).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: host, OrganizationID: org, RoleID: role, ScopeType: "org"})
	require.NoError(t, err)
	var root string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT account_id FROM hosted_account WHERE organization_id = $1 AND depth = 0`, org).Scan(&root))
	return fixture{conn, hostedaccounts.NewService(conn), org, host, addUser(t, conn, org), root}
}

func addUser(t *testing.T, conn *sql.DB, org int64) int64 {
	t.Helper()
	var id int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO "user" (user_id, username, password_hash) VALUES ($1, $2, 'test-hash') RETURNING id`, uuid.NewString(), uuid.NewString()).Scan(&id))
	_, err := conn.ExecContext(t.Context(), `INSERT INTO user_organization (user_id, organization_id, role_id) SELECT $1, $2, id FROM role WHERE organization_id = $2 AND builtin_key = 'FIELD_TECH'`, id, org)
	require.NoError(t, err)
	return id
}

func (f fixture) client(t *testing.T, name string, admin int64, capabilities hostedaccounts.Capabilities) hostedaccounts.Account {
	t.Helper()
	a, err := f.svc.CreateChild(t.Context(), f.host, f.org, f.root, name, admin, capabilities)
	require.NoError(t, err)
	return a
}

func TestMembershipsAreAccountSpecificAndDoNotGrantHostPermissions(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	b := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	require.NotEqual(t, a.AccountID, b.AccountID, "display names are not identity")
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, a.AccountID, f.user, hostedaccounts.RolePoolOperator))
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, b.AccountID, f.user, hostedaccounts.RoleViewer))
	access, err := f.svc.Resolve(t.Context(), f.user, f.org, a.AccountID)
	require.NoError(t, err)
	require.True(t, access.Has(hostedaccounts.ChangePools))
	access, err = f.svc.Resolve(t.Context(), f.user, f.org, b.AccountID)
	require.NoError(t, err)
	require.True(t, access.Has(hostedaccounts.View))
	require.False(t, access.Has(hostedaccounts.ChangePools))
	effective, err := authz.LoadEffectiveTx(t.Context(), sqlc.New(f.db), f.user, f.org)
	require.NoError(t, err)
	require.Empty(t, effective.Keys(), "account roles must never become organization grants")
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, a.AccountID, f.user))
	_, err = f.svc.Resolve(t.Context(), f.user, f.org, a.AccountID)
	require.Error(t, err)
	access, err = f.svc.Resolve(t.Context(), f.user, f.org, b.AccountID)
	require.NoError(t, err)
	require.True(t, access.Has(hostedaccounts.View))
}

func TestHierarchyRequiresDelegationAndDoesNotInheritPrivateAccess(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, a.AccountID, "Subclient", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	require.Equal(t, int32(2), child.Depth)
	_, err = f.svc.Resolve(t.Context(), f.user, f.org, child.AccountID)
	require.Error(t, err, "parent administration does not imply private child access")
	_, err = f.svc.Resolve(t.Context(), f.host, f.org, a.AccountID)
	require.Error(t, err, "host operation does not imply private customer access")
	_, err = f.svc.CreateChild(t.Context(), f.host, f.org, child.AccountID, "Too deep", f.host, hostedaccounts.DefaultCapabilities)
	require.Error(t, err)
	b := f.client(t, "No delegation", f.user, hostedaccounts.DefaultCapabilities)
	_, err = f.svc.CreateChild(t.Context(), f.user, f.org, b.AccountID, "Not permitted", f.user, hostedaccounts.DefaultCapabilities)
	require.Error(t, err, "administrator role alone does not grant delegation")
}

func TestAncestorRestrictionsAndSuspensionAreResolvedFromLiveState(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Client", f.user, hostedaccounts.AllCapabilities)
	child, err := f.svc.CreateChild(t.Context(), f.user, f.org, a.AccountID, "Subclient", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	access, err := f.svc.Resolve(t.Context(), f.host, f.org, child.AccountID)
	require.NoError(t, err)
	require.True(t, access.Has(hostedaccounts.ChangePools))
	// Restrict through the service; descendants cannot restore the parent ceiling.
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.View|hostedaccounts.ManageMembers|hostedaccounts.Delegate, false))
	access, err = f.svc.Resolve(t.Context(), f.host, f.org, child.AccountID)
	require.NoError(t, err)
	require.False(t, access.Has(hostedaccounts.ChangePools))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.AllCapabilities, true))
	_, err = f.svc.Resolve(t.Context(), f.host, f.org, child.AccountID)
	require.Error(t, err)
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.AllCapabilities, false))
	require.Error(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, child.AccountID, f.host), "a sole admin cannot be revoked")
	access, err = f.svc.Resolve(t.Context(), f.host, f.org, child.AccountID)
	require.NoError(t, err)
	require.Equal(t, hostedaccounts.RoleAdministrator, access.Role, "rejected revocation rolls back")
}

func TestConcurrentDemotionsKeepAnAdministrator(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Client", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, a.AccountID, f.user, hostedaccounts.RoleAdministrator))
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, actor := range []int64{f.host, f.user} {
		wg.Go(func() {
			<-start
			errs <- f.svc.SetMembership(t.Context(), actor, f.org, a.AccountID, actor, hostedaccounts.RoleViewer)
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	var admins int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM hosted_account_membership WHERE account_id = $1 AND role = 'account_administrator' AND revoked_at IS NULL`, a.ID).Scan(&admins))
	require.Equal(t, 1, admins)
}

// Exercise the existing global deactivation path for a host user who also
// administers a customer account. Customer-only login is not exposed yet.
func (f fixture) deactivation(t *testing.T) func() error {
	t.Helper()
	var roleID int64
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id = $1 AND builtin_key = 'FIELD_TECH'`, f.org).Scan(&roleID))
	_, err := sqlc.New(f.db).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.user, OrganizationID: f.org, RoleID: roleID, ScopeType: "org"})
	require.NoError(t, err)
	store := sqlstores.NewSQLUserStore(f.db)
	host, err := store.GetUserByID(t.Context(), f.host)
	require.NoError(t, err)
	target, err := store.GetUserByID(t.Context(), f.user)
	require.NoError(t, err)
	svc := domainauth.NewService(store, store, sqlstores.NewSQLTransactor(f.db), nil, nil, nil, nil, authz.NewPermissionResolver(f.db))
	ctx := authn.SetInfo(t.Context(), &session.Info{UserID: f.host, OrganizationID: f.org, ExternalUserID: host.UserID, Username: host.Username})
	return func() error {
		_, err := svc.DeactivateUser(ctx, &authv1.DeactivateUserRequest{UserId: target.UserID})
		return err
	}
}

func TestGlobalDeactivationRequiresAnotherCustomerAdministrator(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Client", f.user, hostedaccounts.DefaultCapabilities)
	deactivate := f.deactivation(t)
	require.ErrorContains(t, deactivate(), "assign another account administrator")
	_, err := f.svc.Resolve(t.Context(), f.user, f.org, a.AccountID)
	require.NoError(t, err)
	// Explicit membership is still required for the replacement host user.
	require.NoError(t, f.svc.SetMembership(t.Context(), f.user, f.org, a.AccountID, f.host, hostedaccounts.RoleAdministrator))
	require.NoError(t, deactivate())
	_, err = f.svc.Resolve(t.Context(), f.user, f.org, a.AccountID)
	require.Error(t, err, "global deactivation must revoke account access")
}

func TestConcurrentDeactivationAndDemotionKeepAnAdministrator(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Client", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, a.AccountID, f.user, hostedaccounts.RoleAdministrator))
	deactivate := f.deactivation(t)
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, change := range []func() error{
		deactivate,
		func() error {
			return f.svc.SetMembership(t.Context(), f.host, f.org, a.AccountID, f.host, hostedaccounts.RoleViewer)
		},
	} {
		wg.Go(func() { <-start; errs <- change() })
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	var admins int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM hosted_account_membership m JOIN "user" u ON u.id = m.user_id WHERE account_id = $1 AND role = 'account_administrator' AND revoked_at IS NULL AND u.deleted_at IS NULL`, a.ID).Scan(&admins))
	require.Equal(t, 1, admins)
}

func TestOrganizationAndIdentityBoundariesFailClosed(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Client", f.user, hostedaccounts.DefaultCapabilities)
	_, err := f.svc.Resolve(t.Context(), f.user, f.org+9999, a.AccountID)
	require.Error(t, err)
	_, err = f.svc.Resolve(t.Context(), f.user, f.org, uuid.NewString())
	require.Error(t, err)
	_, err = f.svc.CreateChild(t.Context(), f.user, f.org, f.root, "Escalation", f.user, hostedaccounts.DefaultCapabilities)
	require.Error(t, err)
	_, err = f.db.ExecContext(t.Context(), `UPDATE "user" SET deleted_at = now() WHERE id = $1`, f.user)
	require.NoError(t, err)
	_, err = f.svc.Resolve(t.Context(), f.user, f.org, a.AccountID)
	require.Error(t, err, "global deactivation revokes account access")
}

func TestReinstatementDoesNotRestoreRevokedMemberships(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Client", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, a.AccountID, f.user, hostedaccounts.RoleViewer))
	require.NoError(t, f.svc.RevokeMembership(t.Context(), f.host, f.org, a.AccountID, f.user))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.DefaultCapabilities, true))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, a.AccountID, hostedaccounts.DefaultCapabilities, false))
	_, err := f.svc.Resolve(t.Context(), f.user, f.org, a.AccountID)
	require.Error(t, err)
}

func TestMembershipAuthorityIsRecheckedAndAuditCommitsWithMutation(t *testing.T) {
	f := setup(t)
	a := f.client(t, "Client", f.host, hostedaccounts.DefaultCapabilities)
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, a.AccountID, f.user, hostedaccounts.RoleAdministrator))
	require.NoError(t, f.svc.SetMembership(t.Context(), f.host, f.org, a.AccountID, f.user, hostedaccounts.RoleViewer))
	err := f.svc.SetMembership(t.Context(), f.user, f.org, a.AccountID, f.user, hostedaccounts.RoleAdministrator)
	require.Error(t, err, "a demoted member cannot reuse earlier administration authority")
	var before, after int
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM activity_log WHERE organization_id = $1`, f.org).Scan(&before))
	require.Equal(t, 3, before)
	err = f.svc.SetMembership(t.Context(), f.host, f.org, a.AccountID, f.host, hostedaccounts.RoleViewer)
	require.Error(t, err)
	require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM activity_log WHERE organization_id = $1`, f.org).Scan(&after))
	require.Equal(t, before, after, "rolled-back mutations produce no audit event")
}

func TestCustomerAffiliationDoesNotRequireOrganizationRole(t *testing.T) {
	f := setup(t)
	_, err := f.db.ExecContext(t.Context(), `UPDATE user_organization SET role_id = NULL WHERE organization_id = $1 AND user_id = $2`, f.org, f.user)
	require.NoError(t, err)
	role, err := sqlstores.NewSQLUserStore(f.db).GetUserRoleName(t.Context(), f.user, f.org)
	require.NoError(t, err)
	require.Empty(t, role)
	effective, err := authz.LoadEffectiveTx(t.Context(), sqlc.New(f.db), f.user, f.org)
	require.NoError(t, err)
	require.Empty(t, effective.Keys())
	a := f.client(t, "Customer", f.user, hostedaccounts.DefaultCapabilities)
	access, err := f.svc.Resolve(t.Context(), f.user, f.org, a.AccountID)
	require.NoError(t, err)
	require.True(t, access.Has(hostedaccounts.View))
}
