package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestAccountAssetRollbackPreservesAllocationsAndRecallWitnesses(t *testing.T) {
	for _, recalled := range []bool{false, true} {
		name := "allocated"
		if recalled {
			name = "recalled"
		}
		t.Run(name, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			rollbackEmptyAllocationHistory(t, database.DB)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			ctx := t.Context()
			var root, account int64
			require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, user.OrganizationID).Scan(&root))
			require.NoError(t, database.DB.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Customer',31) RETURNING id`, uuid.NewString(), user.OrganizationID, root).Scan(&account))
			_, err = database.DB.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,client_id) VALUES($1,5,$2,$2,$2,$3)`, user.OrganizationID, device.DatabaseID, account)
			require.NoError(t, err)
			expectedRevision := int64(1)
			if recalled {
				_, err = database.DB.ExecContext(ctx, `UPDATE hosted_account_asset SET client_id=NULL,revision=revision+1 WHERE organization_id=$1`, user.OrganizationID)
				require.NoError(t, err)
				expectedRevision++
			}
			down, err := migrations.Current.ReadFile("current/001028_internal_hosted_account_assets.down.sql")
			require.NoError(t, err)
			// A prepared statement verifies the refusal and destructive DDL stay in one
			// atomic statement even when run without an outer migration transaction.
			rollback, err := database.DB.PrepareContext(ctx, string(down))
			require.NoError(t, err)
			defer rollback.Close()
			_, err = rollback.ExecContext(ctx)
			require.ErrorContains(t, err, "reporting allocation data")
			var revision int64
			require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT revision FROM hosted_account_asset WHERE organization_id=$1 AND kind=5 AND asset_id=$2`, user.OrganizationID, device.DatabaseID).Scan(&revision))
			require.Equal(t, expectedRevision, revision)
			var schemaIntact bool
			require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT to_regclass('hosted_account_asset') IS NOT NULL AND to_regprocedure('check_hosted_account_asset()') IS NOT NULL`).Scan(&schemaIntact))
			require.True(t, schemaIntact)
			// Explicit test cleanup permits empty rollback; neither rollback nor reapply
			// may remove or recreate the existing account hierarchy.
			_, err = database.DB.ExecContext(ctx, `DELETE FROM hosted_account_asset WHERE organization_id=$1`, user.OrganizationID)
			require.NoError(t, err)
			_, err = rollback.ExecContext(ctx)
			require.NoError(t, err)
			var schemaDropped bool
			require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT to_regclass('hosted_account_asset') IS NULL AND to_regprocedure('check_hosted_account_asset()') IS NULL`).Scan(&schemaDropped))
			require.True(t, schemaDropped)
			up, err := migrations.Current.ReadFile("current/001028_internal_hosted_account_assets.up.sql")
			require.NoError(t, err)
			_, err = database.DB.ExecContext(ctx, string(up))
			require.NoError(t, err)
			var preservedRoot, preservedCustomer int64
			require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, user.OrganizationID).Scan(&preservedRoot))
			require.Equal(t, root, preservedRoot)
			require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND parent_id=$2 AND depth=1`, user.OrganizationID, root).Scan(&preservedCustomer))
			require.Equal(t, account, preservedCustomer)
			var count int
			require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT count(*) FROM hosted_account_asset`).Scan(&count))
			require.Zero(t, count)
		})
	}
}

// The service checks the hierarchy before writing, but the trigger is the
// durable guard against direct writes that would corrupt reporting ownership.
func TestAccountAssetChainTriggerRejectsBrokenOwnership(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	org := user.OrganizationID
	miner := database.CreateDevice(org, "proto").DatabaseID
	controller := database.CreateDevice(org, "proto").DatabaseID
	other := database.CreateDevice(org, "proto").DatabaseID
	ctx := t.Context()
	var root, customer, otherCustomer, subclient, tank, module int64
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	account := func(parent int64, depth int, name string) (id int64) {
		require.NoError(t, database.DB.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,$4,$5,31) RETURNING id`, uuid.NewString(), org, parent, depth, name).Scan(&id))
		return id
	}
	customer = account(root, 1, "Customer")
	otherCustomer = account(root, 1, "Other customer")
	subclient = account(otherCustomer, 2, "Foreign subclient")
	_, err = database.DB.ExecContext(ctx, `INSERT INTO container_inventory(device_id,org_id,observed_at) VALUES($1,$2,now())`, controller, org)
	require.NoError(t, err)
	require.NoError(t, database.DB.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, controller).Scan(&tank))
	require.NoError(t, database.DB.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at,state,state_observed_at) VALUES($1,1,now(),now(),'active',now()) RETURNING id`, tank).Scan(&module))
	for _, broken := range []struct {
		name    string
		insert  string
		args    []any
		message string
	}{
		{"subclient as customer", `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,client_id) VALUES($1,5,$2,$2,$2,$3)`, []any{org, miner, subclient}, "allocation requires customer"},
		{"root as customer", `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,client_id) VALUES($1,5,$2,$2,$2,$3)`, []any{org, miner, root}, "allocation requires customer"},
		{"another customer's subclient", `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,client_id,subclient_id) VALUES($1,5,$2,$2,$2,$3,$4)`, []any{org, miner, customer, subclient}, "allocation requires direct subclient"},
		{"module under another controller", `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,module_id,client_id) VALUES($1,6,$2,$3,$2,$4)`, []any{org, module, other, customer}, "module controller mismatch"},
	} {
		t.Run(broken.name, func(t *testing.T) {
			_, err := database.DB.ExecContext(ctx, broken.insert, broken.args...)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23514", pgErr.Code)
			require.Contains(t, pgErr.Message, broken.message)
		})
	}
	// Updates pass through the same trigger, so a valid row cannot be moved under
	// a foreign subclient after insert either.
	_, err = database.DB.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,miner_id,client_id) VALUES($1,5,$2,$2,$2,$3)`, org, miner, customer)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, `UPDATE hosted_account_asset SET subclient_id=$3 WHERE organization_id=$1 AND asset_id=$2`, org, miner, subclient)
	require.ErrorContains(t, err, "allocation requires direct subclient")
}
