package authz_test

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUpdateCustomRoleReportsReloadErrorBeforeAdministratorContinuity(t *testing.T) {
	conn := testutil.GetTestDB(t)
	ctx := t.Context()
	orgID, callerID := setupOrgWithSuperAdmin(t, conn)
	svc := authz.NewService(conn, nil)
	role, err := svc.CreateCustomRole(ctx, callerID, orgID, "Reload target", "", []string{authz.PermFleetRead})
	require.NoError(t, err)

	// Both reload and continuity would fail. A transaction-local role tombstone
	// forces a real ErrNoRows at reload without mocking the database.
	_, err = conn.ExecContext(ctx, `
		INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities)
		SELECT $1,organization_id,id,1,'Unadministered fixture',31 FROM hosted_account
		WHERE organization_id=$2 AND depth=0`, uuid.NewString(), orgID)
	require.NoError(t, err)
	unadministered, err := sqlc.New(conn).CountHostedAccountsWithoutAdministrator(ctx, sqlc.CountHostedAccountsWithoutAdministratorParams{OrganizationID: orgID})
	require.NoError(t, err)
	require.Equal(t, int64(1), unadministered)
	_, err = conn.ExecContext(ctx, `
		CREATE FUNCTION tombstone_role_on_rename() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN NEW.deleted_at := CURRENT_TIMESTAMP; RETURN NEW; END $$;
		CREATE TRIGGER tombstone_role_on_rename BEFORE UPDATE OF name ON role
		FOR EACH ROW EXECUTE FUNCTION tombstone_role_on_rename()`)
	require.NoError(t, err)

	_, err = svc.UpdateCustomRole(ctx, callerID, orgID, role.ID, "Renamed target", "", []string{authz.PermFleetRead})
	require.True(t, fleeterror.IsInvalidArgumentError(err), "reload failure must not be masked by continuity failure: %v", err)
	require.ErrorContains(t, err, "invalid role_id")
	persisted, err := sqlc.New(conn).GetRoleByID(ctx, role.ID)
	require.NoError(t, err, "failed update rolls back its tombstone and rename")
	require.Equal(t, "Reload target", persisted.Name)
}
