package auth_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/authn"
	authv1 "github.com/block/proto-fleet/server/generated/grpc/auth/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	domainauth "github.com/block/proto-fleet/server/internal/domain/auth"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const customerPassword = "customer-password123"

type customerManagementFixture struct {
	conn               *sql.DB
	svc                *domainauth.Service
	sessions           *session.Service
	transactor         interfaces.Transactor
	operatorInfo       session.Info
	customerInfo       session.Info
	org                int64
	customerID         int64
	customerExternalID string
	accountID          string
}

func customerManagementSetup(t *testing.T) customerManagementFixture {
	t.Helper()
	conn := dbtest.GetTestDB(t)
	host := seedSuperAdmin(t, conn, "customer-boundary")
	require.NoError(t, authz.Reconcile(t.Context(), conn))
	q := sqlc.New(conn)
	operatorRole, err := q.CreateCustomRole(t.Context(), sqlc.CreateCustomRoleParams{Name: "Host user manager", OrganizationID: sql.NullInt64{Int64: host.OrganizationID, Valid: true}})
	require.NoError(t, err)
	var permissionID int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM permission WHERE key=$1`, authz.PermUserManage).Scan(&permissionID))
	require.NoError(t, q.AssignPermissionToRole(t.Context(), sqlc.AssignPermissionToRoleParams{RoleID: operatorRole.ID, PermissionID: permissionID}))
	operatorExternal := uuid.NewString()
	operatorID, err := q.CreateUser(t.Context(), sqlc.CreateUserParams{UserID: operatorExternal, Username: "operator", PasswordHash: "fixture", CreatedAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, q.CreateUserOrganization(t.Context(), sqlc.CreateUserOrganizationParams{UserID: operatorID, OrganizationID: host.OrganizationID, RoleID: sql.NullInt64{Int64: operatorRole.ID, Valid: true}}))
	_, err = q.AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: operatorID, OrganizationID: host.OrganizationID, RoleID: operatorRole.ID, ScopeType: "org"})
	require.NoError(t, err)
	resolver := authz.NewPermissionResolver(conn)
	eff, err := resolver.LoadEffective(t.Context(), operatorID, host.OrganizationID)
	require.NoError(t, err)
	require.True(t, eff.Has(authz.PermUserManage, authz.ResourceContext{}))
	require.False(t, eff.Has(authz.PermRoleManage, authz.ResourceContext{}))
	accounts := hostedaccounts.NewService(conn, nil)
	var root string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT account_id FROM hosted_account WHERE organization_id=$1 AND depth=0`, host.OrganizationID).Scan(&root))
	account, err := accounts.CreateChild(t.Context(), host.ID, host.OrganizationID, root, "Private customer", host.ID, hostedaccounts.DefaultCapabilities)
	require.NoError(t, err)
	_, err = accounts.Resolve(t.Context(), operatorID, host.OrganizationID, account.AccountID)
	require.Error(t, err, "host user management is not a private-account grant")
	sessions := session.NewService(session.Config{Duration: time.Hour, IDBytes: 32, CookieName: "fleet_session"}, sqlstores.NewSQLSessionStore(conn))
	invitations := hostedaccounts.NewInvitationService(accounts, sessions)
	issued, err := invitations.Create(t.Context(), host.ID, host.OrganizationID, account.AccountID, hostedaccounts.RoleAdministrator, time.Hour)
	require.NoError(t, err)
	registered, err := invitations.Register(t.Context(), issued.Token, "customer-admin", customerPassword, "browser", "")
	require.NoError(t, err)
	customer, err := q.GetUserByUsername(t.Context(), "customer-admin")
	require.NoError(t, err)
	users := sqlstores.NewSQLUserStore(conn)
	role, err := users.GetUserRoleName(t.Context(), customer.ID, host.OrganizationID)
	require.NoError(t, err)
	require.Empty(t, role)
	transactor := sqlstores.NewSQLTransactor(conn)
	svc := domainauth.NewService(users, users, transactor, nil, sessions, nil, activity.NewService(sqlstores.NewSQLActivityStore(conn)), resolver)
	return customerManagementFixture{conn: conn, svc: svc, sessions: sessions, transactor: transactor,
		operatorInfo: session.Info{UserID: operatorID, OrganizationID: host.OrganizationID, ExternalUserID: operatorExternal, Username: "operator", AuthMethod: session.AuthMethodSession},
		customerInfo: session.Info{UserID: customer.ID, OrganizationID: host.OrganizationID, ExternalUserID: customer.UserID, Username: customer.Username, SessionID: registered.Cookie.Value, AuthMethod: session.AuthMethodSession},
		org:          host.OrganizationID, customerID: customer.ID, customerExternalID: customer.UserID, accountID: account.AccountID}
}

type customerState struct {
	hash                            string
	changed                         bool
	updated, deleted                sql.NullTime
	sessions, memberships, activity int
}

func (f customerManagementFixture) state(t *testing.T) customerState {
	t.Helper()
	var s customerState
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT password_hash,requires_password_change,password_updated_at,deleted_at FROM "user" WHERE id=$1`, f.customerID).Scan(&s.hash, &s.changed, &s.updated, &s.deleted))
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM session WHERE user_id=$1 AND revoked_at IS NULL`, f.customerID).Scan(&s.sessions))
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM hosted_account_membership WHERE user_id=$1 AND revoked_at IS NULL`, f.customerID).Scan(&s.memberships))
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM activity_log WHERE event_type IN ('reset_password','deactivate_user')`).Scan(&s.activity))
	return s
}

func TestOrganizationManagementRejectsInvitedCustomerIdentity(t *testing.T) {
	for _, operation := range []string{"reset", "deactivate"} {
		t.Run(operation, func(t *testing.T) {
			f := customerManagementSetup(t)
			before := f.state(t)
			var err error
			if operation == "reset" {
				_, err = f.svc.ResetUserPassword(authn.SetInfo(t.Context(), &f.operatorInfo), &authv1.ResetUserPasswordRequest{UserId: f.customerExternalID})
			} else {
				_, err = f.svc.DeactivateUser(authn.SetInfo(t.Context(), &f.operatorInfo), &authv1.DeactivateUserRequest{UserId: f.customerExternalID})
			}
			require.True(t, fleeterror.IsInvalidArgumentError(err), "roleless private-account identity is outside organization user management")
			require.Equal(t, before, f.state(t), "rejected management preserves password, sessions, memberships and activity")
		})
	}
}

// This hook models a committed role removal after the optimistic authorization
// and password hashing, immediately before the real mutation transaction.
type beforeCustomerManagementTx struct {
	interfaces.Transactor
	before func(context.Context) error
}

func (s beforeCustomerManagementTx) RunInTx(ctx context.Context, action func(context.Context) error) error {
	if err := s.before(ctx); err != nil {
		return fmt.Errorf("fixture before transaction: %w", err)
	}
	return s.Transactor.RunInTx(ctx, action)
}

func TestOrganizationManagementRechecksLostRoleBeforeMutation(t *testing.T) {
	for _, operation := range []string{"reset", "deactivate"} {
		t.Run(operation, func(t *testing.T) {
			f := customerManagementSetup(t)
			q := sqlc.New(f.conn)
			role, err := q.CreateCustomRole(t.Context(), sqlc.CreateCustomRoleParams{Name: "Temporary organization role", OrganizationID: sql.NullInt64{Int64: f.org, Valid: true}})
			require.NoError(t, err)
			_, err = f.conn.ExecContext(t.Context(), `UPDATE user_organization SET role_id=$1 WHERE user_id=$2 AND organization_id=$3`, role.ID, f.customerID, f.org)
			require.NoError(t, err)
			users := sqlstores.NewSQLUserStore(f.conn)
			transactor := beforeCustomerManagementTx{Transactor: f.transactor, before: func(ctx context.Context) error {
				_, err := f.conn.ExecContext(ctx, `UPDATE user_organization SET role_id=NULL WHERE user_id=$1 AND organization_id=$2`, f.customerID, f.org)
				if err != nil {
					return fmt.Errorf("fixture remove organization role: %w", err)
				}
				return nil
			}}
			svc := domainauth.NewService(users, users, transactor, nil, f.sessions, nil, activity.NewService(sqlstores.NewSQLActivityStore(f.conn)), authz.NewPermissionResolver(f.conn))
			before := f.state(t)
			if operation == "reset" {
				_, err = svc.ResetUserPassword(authn.SetInfo(t.Context(), &f.operatorInfo), &authv1.ResetUserPasswordRequest{UserId: f.customerExternalID})
			} else {
				_, err = svc.DeactivateUser(authn.SetInfo(t.Context(), &f.operatorInfo), &authv1.DeactivateUserRequest{UserId: f.customerExternalID})
			}
			require.True(t, fleeterror.IsInvalidArgumentError(err), "role admission must be rechecked inside mutation transaction")
			require.Equal(t, before, f.state(t))
		})
	}
}

func TestCustomerKeepsOwnLoginAndPasswordChange(t *testing.T) {
	f := customerManagementSetup(t)
	_, cookie, err := f.svc.AuthenticateUser(t.Context(), &authv1.AuthenticateRequest{Username: "customer-admin", Password: customerPassword}, "browser", "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, cookie)
	_, cookie, err = f.svc.UpdatePassword(authn.SetInfo(t.Context(), &f.customerInfo), &authv1.UpdatePasswordRequest{CurrentPassword: customerPassword, NewPassword: "replacement-password123"}, "browser", "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, cookie)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(f.state(t).hash), []byte("replacement-password123")))
	_, _, err = f.svc.AuthenticateUser(t.Context(), &authv1.AuthenticateRequest{Username: "customer-admin", Password: "replacement-password123"}, "browser", "127.0.0.1")
	require.NoError(t, err)
}

func (f customerManagementFixture) addOrganizationRole(t *testing.T) int64 {
	t.Helper()
	role, err := sqlc.New(f.conn).CreateCustomRole(t.Context(), sqlc.CreateCustomRoleParams{Name: "Organization viewer", OrganizationID: sql.NullInt64{Int64: f.org, Valid: true}})
	require.NoError(t, err)
	_, err = f.conn.ExecContext(t.Context(), `UPDATE user_organization SET role_id=$1 WHERE user_id=$2 AND organization_id=$3`, role.ID, f.customerID, f.org)
	require.NoError(t, err)
	_, err = sqlc.New(f.conn).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.customerID, OrganizationID: f.org, RoleID: role.ID, ScopeType: "org"})
	require.NoError(t, err)
	return role.ID
}

func TestOrganizationManagementPreservesActiveOrganizationUsers(t *testing.T) {
	for _, operation := range []string{"reset", "deactivate"} {
		t.Run(operation, func(t *testing.T) {
			f := customerManagementSetup(t)
			f.addOrganizationRole(t)
			if operation == "reset" {
				response, err := f.svc.ResetUserPassword(authn.SetInfo(t.Context(), &f.operatorInfo), &authv1.ResetUserPasswordRequest{UserId: f.customerExternalID})
				require.NoError(t, err)
				require.NotEmpty(t, response.TemporaryPassword)
				after := f.state(t)
				require.NoError(t, bcrypt.CompareHashAndPassword([]byte(after.hash), []byte(response.TemporaryPassword)))
				require.True(t, after.changed)
				require.Zero(t, after.sessions)
				require.Equal(t, 1, after.activity)
			} else {
				_, err := f.svc.DeactivateUser(authn.SetInfo(t.Context(), &f.operatorInfo), &authv1.DeactivateUserRequest{UserId: f.customerExternalID})
				require.NoError(t, err)
				require.True(t, f.state(t).deleted.Valid)
			}
		})
	}
}

func TestOrganizationManagementRejectsInactiveOrganizationRole(t *testing.T) {
	for _, operation := range []string{"reset", "deactivate"} {
		t.Run(operation, func(t *testing.T) {
			f := customerManagementSetup(t)
			roleID := f.addOrganizationRole(t)
			_, err := f.conn.ExecContext(t.Context(), `UPDATE role SET deleted_at=now() WHERE id=$1`, roleID)
			require.NoError(t, err)
			before := f.state(t)
			if operation == "reset" {
				_, err = f.svc.ResetUserPassword(authn.SetInfo(t.Context(), &f.operatorInfo), &authv1.ResetUserPasswordRequest{UserId: f.customerExternalID})
			} else {
				_, err = f.svc.DeactivateUser(authn.SetInfo(t.Context(), &f.operatorInfo), &authv1.DeactivateUserRequest{UserId: f.customerExternalID})
			}
			require.True(t, fleeterror.IsInvalidArgumentError(err))
			require.Equal(t, before, f.state(t))
		})
	}
}

func TestOrganizationPasswordResetAllowsConcurrentAffiliationForeignKeys(t *testing.T) {
	f := customerManagementSetup(t)
	roleID := f.addOrganizationRole(t)
	otherID, err := sqlc.New(f.conn).CreateUser(t.Context(), sqlc.CreateUserParams{UserID: uuid.NewString(), Username: "concurrent-affiliation", PasswordHash: "fixture", CreatedAt: time.Now()})
	require.NoError(t, err)
	tx, err := f.conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback()) }()
	// The inserted FK keeps an organization KEY SHARE lock until rollback.
	require.NoError(t, sqlc.New(tx).CreateUserOrganization(t.Context(), sqlc.CreateUserOrganizationParams{UserID: otherID, OrganizationID: f.org, RoleID: sql.NullInt64{Int64: roleID, Valid: true}}))
	ctx, cancel := context.WithTimeout(authn.SetInfo(t.Context(), &f.operatorInfo), 2*time.Second)
	defer cancel()
	_, err = f.svc.ResetUserPassword(ctx, &authv1.ResetUserPasswordRequest{UserId: f.customerExternalID})
	require.NoError(t, err, "reset must not block on an unrelated affiliation's foreign-key lock")
}

type pausedAdminPasswordWrite struct {
	interfaces.UserManagementStore
	entered chan struct{}
	proceed chan struct{}
}

func (s pausedAdminPasswordWrite) AdminResetUserPassword(ctx context.Context, userID int64, hash string) (int64, error) {
	close(s.entered)
	<-s.proceed
	return s.UserManagementStore.AdminResetUserPassword(ctx, userID, hash)
}

func TestOrganizationPasswordResetHoldsRoleAdmissionThroughWrite(t *testing.T) {
	f := customerManagementSetup(t)
	f.addOrganizationRole(t)
	users := sqlstores.NewSQLUserStore(f.conn)
	paused := pausedAdminPasswordWrite{UserManagementStore: users, entered: make(chan struct{}), proceed: make(chan struct{})}
	svc := domainauth.NewService(users, paused, f.transactor, nil, f.sessions, nil, activity.NewService(sqlstores.NewSQLActivityStore(f.conn)), authz.NewPermissionResolver(f.conn))
	result := make(chan error, 1)
	go func() {
		_, err := svc.ResetUserPassword(authn.SetInfo(t.Context(), &f.operatorInfo), &authv1.ResetUserPasswordRequest{UserId: f.customerExternalID})
		result <- err
	}()
	<-paused.entered
	func() {
		defer close(paused.proceed)
		tx, err := f.conn.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		_, err = tx.ExecContext(t.Context(), `SET LOCAL lock_timeout = '200ms'`)
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), `UPDATE user_organization SET role_id=NULL WHERE user_id=$1 AND organization_id=$2`, f.customerID, f.org)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "55P03", pgErr.Code, "role removal cannot cross the locked admission/password-write boundary")
		// A client deadline returns before pgx finishes cancelling on the server.
		// Finish the updater transaction before releasing the password writer.
		require.NoError(t, tx.Rollback())
	}()
	require.NoError(t, <-result)
	role, err := users.GetUserRoleName(t.Context(), f.customerID, f.org)
	require.NoError(t, err)
	require.NotEmpty(t, role)
}

func TestHostUserDirectoryExcludesCustomerOnlyIdentities(t *testing.T) {
	f := customerManagementSetup(t)
	q := sqlc.New(f.conn)
	role, err := q.GetUserRoleInOrganization(t.Context(), sqlc.GetUserRoleInOrganizationParams{UserID: f.operatorInfo.UserID, OrganizationID: f.org})
	require.NoError(t, err)
	var permissionID int64
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT id FROM permission WHERE key=$1`, authz.PermUserRead).Scan(&permissionID))
	require.NoError(t, q.AssignPermissionToRole(t.Context(), sqlc.AssignPermissionToRoleParams{RoleID: role.ID, PermissionID: permissionID}))
	effective, err := authz.NewPermissionResolver(f.conn).LoadEffective(t.Context(), f.operatorInfo.UserID, f.org)
	require.NoError(t, err)
	require.True(t, effective.Has(authz.PermUserRead, authz.ResourceContext{}))

	listedIDs := func() []string {
		response, err := f.svc.ListUsers(authn.SetInfo(t.Context(), &f.operatorInfo))
		require.NoError(t, err)
		ids := make([]string, len(response.Users))
		for i, user := range response.Users {
			ids[i] = user.UserId
		}
		return ids
	}
	ids := listedIDs()
	require.Contains(t, ids, f.operatorInfo.ExternalUserID, "live host role holders remain visible")
	require.NotContains(t, ids, f.customerExternalID, "host user:read is not access to private customer identities")
	roleID := f.addOrganizationRole(t)
	require.Contains(t, listedIDs(), f.customerExternalID, "an explicit live host role admits the identity to the host directory")
	_, err = f.conn.ExecContext(t.Context(), `UPDATE role SET deleted_at=now() WHERE id=$1`, roleID)
	require.NoError(t, err)
	require.NotContains(t, listedIDs(), f.customerExternalID, "a deleted host role does not admit the identity")
}
