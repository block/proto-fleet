package hostedaccounts_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/hostedaccounts"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestInvitationRegistrantRequiresLiveHostRoleForMaintenanceAssignment(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	account := f.client(t, "Customer", f.host, hostedaccounts.DefaultCapabilities)
	invites := invitations(f)
	issued, err := invites.Create(ctx, f.host, f.org, account.AccountID, hostedaccounts.RoleViewer, time.Hour)
	require.NoError(t, err)
	_, err = invites.Register(ctx, issued.Token, "maintenance-customer", "password123", "browser", "192.0.2.1")
	require.NoError(t, err)
	registered, err := sqlc.New(f.db).GetUserByUsername(ctx, "maintenance-customer")
	require.NoError(t, err)

	store := sqlstores.NewSQLMaintenanceReferenceStore(f.db)
	assignees, err := store.ListAssignees(ctx, f.org)
	require.NoError(t, err)
	for _, assignee := range assignees {
		require.NotEqual(t, registered.ID, assignee.UserID, "ineligible users must not be offered as assignees")
	}
	_, err = store.ResolveAssignee(ctx, f.org, registered.ID)
	require.True(t, fleeterror.IsNotFoundError(err), "assignment writes must reject an ineligible user: %v", err)

	// Giving the same registered identity a host role makes it eligible; a
	// customer account membership alone must never have that effect.
	var hostRole int64
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='FIELD_TECH'`, f.org).Scan(&hostRole))
	_, err = f.db.ExecContext(ctx, `UPDATE user_organization SET role_id=$1 WHERE organization_id=$2 AND user_id=$3`, hostRole, f.org, registered.ID)
	require.NoError(t, err)
	assignee, err := store.ResolveAssignee(ctx, f.org, registered.ID)
	require.NoError(t, err)
	require.Equal(t, "FIELD_TECH", assignee.RoleName)
	assignees, err = store.ListAssignees(ctx, f.org)
	require.NoError(t, err)
	require.Contains(t, assignees, *assignee)

	var otherOrg int64
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Other host') RETURNING id`, uuid.NewString()).Scan(&otherOrg))
	require.NoError(t, authz.Reconcile(ctx, f.db))
	var otherRole int64
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT id FROM role WHERE organization_id=$1 AND builtin_key='FIELD_TECH'`, otherOrg).Scan(&otherRole))

	for _, tc := range []struct {
		name     string
		mutation string
		argument int64
	}{
		{"role from another organization", `UPDATE user_organization SET role_id=$1 WHERE organization_id=$2 AND user_id=$3`, otherRole},
		{"deleted role", `UPDATE role SET deleted_at=CURRENT_TIMESTAMP WHERE id=$1`, hostRole},
		{"deleted organization membership", `UPDATE user_organization SET deleted_at=CURRENT_TIMESTAMP WHERE user_id=$1`, registered.ID},
		{"deactivated identity", `UPDATE "user" SET deleted_at=CURRENT_TIMESTAMP WHERE id=$1`, registered.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := f.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback() }()
			if tc.name == "role from another organization" {
				_, err = tx.ExecContext(ctx, tc.mutation, tc.argument, f.org, registered.ID)
			} else {
				_, err = tx.ExecContext(ctx, tc.mutation, tc.argument)
			}
			require.NoError(t, err)
			q := sqlc.New(tx)
			rows, err := q.ListMaintenanceAssignees(ctx, f.org)
			require.NoError(t, err)
			for _, row := range rows {
				require.NotEqual(t, registered.ID, row.UserID)
			}
			_, err = q.ResolveMaintenanceAssignee(ctx, sqlc.ResolveMaintenanceAssigneeParams{OrgID: f.org, UserID: registered.ID})
			require.ErrorIs(t, err, sql.ErrNoRows)
		})
	}
}
