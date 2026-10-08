package hostedaccounts

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
)

type accountListDB struct {
	*sql.DB
	calls int
}

func (d *accountListDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	d.calls++
	rows, err := d.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("account list query: %w", err)
	}
	return rows, nil
}

func (d *accountListDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	d.calls++
	return d.DB.QueryRowContext(ctx, query, args...)
}

type accountListFixture struct {
	conn            *accountListDB
	svc             *Service
	org, host, user int64
	root            string
}

func setupAccountList(t *testing.T) accountListFixture {
	t.Helper()
	conn := &accountListDB{DB: dbtest.GetTestDB(t)}
	f := accountListFixture{conn: conn, svc: &Service{conn: conn.DB, queries: sqlc.New(conn)}}
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO organization (org_id,name) VALUES ($1,'Account list') RETURNING id`, uuid.NewString()).Scan(&f.org))
	require.NoError(t, authz.Reconcile(t.Context(), conn.DB))
	addUser := func() int64 {
		var id int64
		require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO "user" (user_id,username,password_hash) VALUES ($1,$2,'test') RETURNING id`, uuid.NewString(), uuid.NewString()).Scan(&id))
		_, err := conn.ExecContext(t.Context(), `INSERT INTO user_organization (user_id,organization_id,role_id) SELECT $1,$2,id FROM role WHERE organization_id=$2 AND builtin_key='FIELD_TECH'`, id, f.org)
		require.NoError(t, err)
		return id
	}
	f.host, f.user = addUser(), addUser()
	var role int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='SUPER_ADMIN'`, f.org).Scan(&role))
	_, err := sqlc.New(conn).AssignRole(t.Context(), sqlc.AssignRoleParams{UserID: f.host, OrganizationID: f.org, RoleID: role, ScopeType: "org"})
	require.NoError(t, err)
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT account_id FROM hosted_account WHERE organization_id=$1 AND depth=0`, f.org).Scan(&f.root))
	return f
}

func (f accountListFixture) create(t *testing.T, parentID, name string, actor, administrator int64) Account {
	t.Helper()
	a, err := f.svc.CreateChild(t.Context(), actor, f.org, parentID, name, administrator, AllCapabilities)
	require.NoError(t, err)
	return a
}

// Compare the batch projection against the mutation-path resolver, including
// public parent identity. Policy-only children must never acquire private access.
func assertAccountListAuthority(t *testing.T, f accountListFixture, actor int64, accounts []Account, childrenOf string) {
	t.Helper()
	var listed []AccountSummary
	var err error
	if childrenOf == "" {
		listed, err = f.svc.ListAccounts(t.Context(), actor, f.org)
	} else {
		listed, err = f.svc.ListChildren(t.Context(), actor, f.org, childrenOf)
	}
	require.NoError(t, err)
	require.True(t, slices.IsSortedFunc(listed, func(a, b AccountSummary) int {
		if order := cmp.Compare(a.Depth, b.Depth); order != 0 {
			return order
		}
		if order := cmp.Compare(a.Name, b.Name); order != 0 {
			return order
		}
		return cmp.Compare(a.AccountID, b.AccountID)
	}))
	byID := make(map[string]AccountSummary, len(listed))
	for _, row := range listed {
		byID[row.AccountID] = row
	}
	for _, a := range accounts {
		path, err := loadPath(t.Context(), f.svc.queries, f.org, a.AccountID)
		require.NoError(t, err)
		metadata := summary(path[len(path)-1], path)
		if childrenOf != "" && metadata.ParentAccountID != childrenOf {
			continue
		}
		access, err := f.svc.Resolve(t.Context(), actor, f.org, a.AccountID)
		row, exists := byID[a.AccountID]
		if err != nil {
			require.True(t, fleeterror.IsForbiddenError(err), "%v", err)
			if childrenOf == "" {
				require.False(t, exists, "inaccessible account %s", a.Name)
				continue
			}
			require.True(t, exists)
			require.Zero(t, row.EffectiveCapabilities)
			require.Empty(t, row.Role)
		} else {
			require.True(t, exists, "authorized account %s", a.Name)
			require.Equal(t, access.Capabilities, row.EffectiveCapabilities)
			require.Equal(t, access.Role, row.Role)
		}
		require.Equal(t, metadata.ParentAccountID, row.ParentAccountID)
		require.Equal(t, metadata.Capabilities, row.Capabilities)
		require.Equal(t, metadata.Suspended, row.Suspended)
		require.Equal(t, metadata.Revision, row.Revision)
	}
}

func TestAccountListBatchMatchesLivePrivateAuthority(t *testing.T) {
	f := setupAccountList(t)
	parent := f.create(t, f.root, "Parent", f.host, f.user)
	accounts := []Account{parent}
	for _, name := range []string{"Direct operator", "Mixed", "Ancestor only", "Revoked grant", "Revoked member", "Suspended", "Private"} {
		accounts = append(accounts, f.create(t, parent.AccountID, name, f.user, f.host))
	}
	q := sqlc.New(f.conn)
	membership := func(a Account, role Role, revoked bool) {
		require.NoError(t, q.SetHostedAccountMembership(t.Context(), sqlc.SetHostedAccountMembershipParams{OrganizationID: f.org, AccountID: a.ID, UserID: f.user, Role: string(role)}))
		if revoked {
			require.NoError(t, q.RevokeHostedAccountMembership(t.Context(), sqlc.RevokeHostedAccountMembershipParams{OrganizationID: f.org, AccountID: a.ID, UserID: f.user}))
		}
	}
	membership(accounts[1], RolePoolOperator, false)
	membership(accounts[2], RoleViewer, false)
	membership(accounts[5], RoleAdministrator, true)
	membership(accounts[6], RoleAdministrator, false)
	for _, a := range accounts[2:5] {
		require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, a.AccountID, parent.AccountID, View|ManagePools))
	}
	require.NoError(t, f.svc.RevokeAncestorGrant(t.Context(), f.host, f.org, accounts[4].AccountID, parent.AccountID))
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.user, f.org, accounts[6].AccountID, AllCapabilities, true))
	assertAccountListAuthority(t, f, f.user, accounts, "")
	assertAccountListAuthority(t, f, f.user, accounts, parent.AccountID)
	// Restricting an ancestor bounds every direct and explicit grant source.
	require.NoError(t, f.svc.ConfigureChild(t.Context(), f.host, f.org, parent.AccountID, View|ManageMembers|Delegate, false))
	assertAccountListAuthority(t, f, f.user, accounts, "")
	assertAccountListAuthority(t, f, f.user, accounts, parent.AccountID)
	// Host account management exposes only delegation until an explicit grant.
	hostList, err := f.svc.ListAccounts(t.Context(), f.host, f.org)
	require.NoError(t, err)
	require.Equal(t, f.root, hostList[0].AccountID)
	require.Equal(t, Delegate, hostList[0].EffectiveCapabilities)
	require.Empty(t, hostList[0].Role)
	assertAccountListAuthority(t, f, f.host, accounts, "")
	assertAccountListAuthority(t, f, f.host, accounts, f.root)
	require.NoError(t, f.svc.SetAncestorGrant(t.Context(), f.host, f.org, parent.AccountID, f.root, View))
	assertAccountListAuthority(t, f, f.host, accounts, "")
	_, err = f.conn.ExecContext(t.Context(), `UPDATE user_organization_role SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`, f.host, f.org)
	require.NoError(t, err)
	assertAccountListAuthority(t, f, f.host, accounts, "")
	// A grant cannot outlive the source member's role capability mask.
	require.NoError(t, q.SetHostedAccountMembership(t.Context(), sqlc.SetHostedAccountMembershipParams{OrganizationID: f.org, AccountID: parent.ID, UserID: f.user, Role: string(RoleViewer)}))
	assertAccountListAuthority(t, f, f.user, accounts, "")
	// Revoking the ancestor membership removes grant-derived private authority.
	require.NoError(t, q.RevokeHostedAccountMembership(t.Context(), sqlc.RevokeHostedAccountMembershipParams{OrganizationID: f.org, AccountID: parent.ID, UserID: f.user}))
	assertAccountListAuthority(t, f, f.user, accounts, "")
	require.NoError(t, q.SetHostedAccountMembership(t.Context(), sqlc.SetHostedAccountMembershipParams{OrganizationID: f.org, AccountID: parent.ID, UserID: f.user, Role: string(RoleAdministrator)}))
	require.NoError(t, q.ConfigureHostedAccount(t.Context(), sqlc.ConfigureHostedAccountParams{Capabilities: int64(AllCapabilities), Suspended: true, OrganizationID: f.org, ID: parent.ID}))
	assertAccountListAuthority(t, f, f.user, accounts, "")
	_, err = f.svc.ListChildren(t.Context(), f.user, f.org, parent.AccountID)
	require.Error(t, err, "a suspended parent cannot expose its child policy list")
}

func TestAccountListBatchRejectsInactiveIdentityAndAffiliation(t *testing.T) {
	for _, statement := range []string{
		`UPDATE "user" SET deleted_at=now() WHERE id=$1 AND EXISTS (SELECT 1 FROM organization WHERE id=$2)`,
		`UPDATE user_organization SET deleted_at=now() WHERE user_id=$1 AND organization_id=$2`,
		`UPDATE organization SET deleted_at=now() WHERE id=$2 AND EXISTS (SELECT 1 FROM "user" WHERE id=$1)`,
	} {
		t.Run(statement, func(t *testing.T) {
			f := setupAccountList(t)
			parent := f.create(t, f.root, "Parent", f.host, f.user)
			_ = f.create(t, parent.AccountID, "Child", f.user, f.user)
			_, err := f.conn.ExecContext(t.Context(), statement, f.user, f.org)
			require.NoError(t, err)
			list, err := f.svc.ListAccounts(t.Context(), f.user, f.org)
			require.NoError(t, err)
			require.Empty(t, list)
			_, err = f.svc.ListChildren(t.Context(), f.user, f.org, parent.AccountID)
			require.Error(t, err)
		})
	}
}

func TestAccountListsUseConstantDatabaseRoundTrips(t *testing.T) {
	f := setupAccountList(t)
	parent := f.create(t, f.root, "Parent", f.host, f.user)
	var childCalls int
	for _, size := range []int{1, 40} {
		for i := range size {
			_ = f.create(t, parent.AccountID, fmt.Sprintf("Child %d-%d", size, i), f.user, f.user)
		}
		f.conn.calls = 0
		list, err := f.svc.ListAccounts(t.Context(), f.user, f.org)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(list), size+1)
		require.Equal(t, 1, f.conn.calls, "account list must execute one SQL statement regardless of account count")
		f.conn.calls = 0
		children, err := f.svc.ListChildren(t.Context(), f.user, f.org, parent.AccountID)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(children), size)
		if size == 1 {
			childCalls = f.conn.calls
			require.Equal(t, 3, childCalls, "path, live parent authority, and batch children")
		} else {
			require.Equal(t, childCalls, f.conn.calls, "child count must not add SQL round trips")
		}
		f.conn.calls = 0
		_, err = f.svc.ListChildren(t.Context(), f.host, f.org, f.root)
		require.NoError(t, err)
		require.Equal(t, 4, f.conn.calls, "host delegation authorization and the batch list use a fixed number of statements")
	}
}
