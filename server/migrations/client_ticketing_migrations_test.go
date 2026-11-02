package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func applyClientTicketMigration(t *testing.T, conn *sql.DB, name string) {
	t.Helper()
	source, err := migrations.Current.ReadFile("current/" + name + ".sql")
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), string(source))
	require.NoError(t, err, name)
}

func TestClientTicketingMigrationDisablesExistingAccountsAndRoundTrips(t *testing.T) {
	conn := testutil.GetTestDB(t)
	rollbackEmptyClientTicketing(t, conn)
	org := insertMaintenanceTestOrg(t, conn, "ticketing-upgrade")
	var root, existing int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Existing customer',15) RETURNING id`, uuid.NewString(), org, root).Scan(&existing))
	_, err := conn.ExecContext(t.Context(), `INSERT INTO repair_ticket(org_id,ticket_number,category,component) VALUES($1,'TK-LEGACY',2,'Legacy component')`, org)
	require.NoError(t, err)

	applyClientTicketMigration(t, conn, "001048_internal_client_ticketing.up")
	var enabled bool
	for _, account := range []int64{root, existing} {
		require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT ticketing_enabled FROM hosted_account WHERE id=$1`, account).Scan(&enabled))
		require.Equal(t, account == root, enabled, "existing clients require opt-in while the host root remains open")
	}
	for _, explicitOptOut := range []bool{false, true} {
		query := `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'New customer',15) RETURNING ticketing_enabled`
		if explicitOptOut {
			query = `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities,ticketing_enabled) VALUES($1,$2,$3,1,'Opted out customer',15,false) RETURNING ticketing_enabled`
		}
		require.NoError(t, conn.QueryRowContext(t.Context(), query, uuid.NewString(), org, root).Scan(&enabled))
		require.Equal(t, !explicitOptOut, enabled, "new accounts default on and honor explicit opt-out")
	}
	newOrg := insertMaintenanceTestOrg(t, conn, "ticketing-new-org")
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT ticketing_enabled FROM hosted_account WHERE organization_id=$1 AND depth=0`, newOrg).Scan(&enabled))
	require.True(t, enabled, "new host roots must use the new-account default")

	var shared bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT shared_with_clients FROM repair_ticket WHERE org_id=$1 AND ticket_number='TK-LEGACY'`, org).Scan(&shared))
	require.False(t, shared, "upgrading must keep legacy tickets internal")
	for _, function := range []string{"snapshot_repair_ticket_target(bigint,bigint,smallint,bigint,bigint)", "open_repair_ticket_audience()", "capture_ticket_resolution_note()", "client_repair_ticket_projection(bigint,bigint,bigint)"} {
		var exists bool
		require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regprocedure($1) IS NOT NULL`, function).Scan(&exists))
		require.False(t, exists, "ticket business behavior belongs in Go: %s", function)
	}

	rollbackEmptyClientTicketing(t, conn)
	var component string
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT component FROM repair_ticket WHERE org_id=$1 AND ticket_number='TK-LEGACY'`, org).Scan(&component))
	require.Equal(t, "Legacy component", component, "rollback must preserve pre-ticketing maintenance records")
	var hasNewColumn bool
	require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='hosted_account' AND column_name='ticketing_enabled')`).Scan(&hasNewColumn))
	require.False(t, hasNewColumn)
	applyClientTicketMigration(t, conn, "001048_internal_client_ticketing.up")
}

func TestClientTicketingRollbackRefusesDurableDataBeforeChangingSchema(t *testing.T) {
	for _, tc := range []struct {
		name, evidence string
	}{
		{"new category", `UPDATE repair_ticket SET category=3 WHERE id=$2`},
		{"description", `UPDATE repair_ticket SET description='Retained client description' WHERE id=$2`},
		{"creator account", `UPDATE repair_ticket SET created_by_account_id=$3 WHERE id=$2`},
		{"client sharing", `UPDATE repair_ticket SET shared_with_clients=true WHERE id=$2`},
		{"resolution note", `UPDATE repair_ticket SET resolution_note='Retained closure evidence' WHERE id=$2`},
		{"resolution account", `UPDATE repair_ticket SET resolution_note_account_id=$3 WHERE id=$2`},
		{"audience", `INSERT INTO repair_ticket_audience(org_id,ticket_id,account_id) VALUES($1,$2,$3)`},
		{"target", `INSERT INTO repair_ticket_target(org_id,ticket_id,kind,asset_id,miner_id,label) VALUES($1,$2,1,$5,$5,'Opening miner snapshot')`},
		{"client comment", `INSERT INTO repair_ticket_comment(org_id,ticket_id,user_id,user_name,text,internal) VALUES($1,$2,$4,'Customer','Retained client reply',false)`},
		{"module model", `UPDATE container_module SET model='Retained module model' WHERE id=$6`},
		{"repeated part history", `INSERT INTO repair_ticket_part(org_id,ticket_id,inventory_part_id,part_name,quantity,consumed_at) VALUES($1,$2,$7,'Consumed part',1,now()),($1,$2,$7,'Replacement part',1,NULL)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, config)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			conn := database.DB
			var account, ticket, tank, module int64
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, user.OrganizationID).Scan(&account))
			require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO repair_ticket(org_id,ticket_number,category,component) VALUES($1,'TK-ROLLBACK',2,'Legacy component') RETURNING id`, user.OrganizationID).Scan(&ticket))
			_, err = conn.ExecContext(t.Context(), `INSERT INTO container_inventory(device_id,org_id,observed_at) VALUES($1,$2,now())`, device.DatabaseID, user.OrganizationID)
			require.NoError(t, err)
			require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, device.DatabaseID).Scan(&tank))
			require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO container_module(tank_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, tank).Scan(&module))
			part := insertMaintenanceTestPart(t, conn, user.OrganizationID, "Retained part")
			// A typed one-row input keeps each evidence statement independent of
			// unused positional parameters while using real schema constraints.
			//nolint:gosec // Evidence statements are fixed test fixtures, never external input.
			_, err = conn.ExecContext(t.Context(), `WITH fixture AS (SELECT $1::bigint AS org_id,$2::bigint AS ticket_id,$3::bigint AS account_id,$4::bigint AS user_id,$5::bigint AS device_id,$6::bigint AS module_id,$7::bigint AS part_id) `+tc.evidence,
				user.OrganizationID, ticket, account, user.DatabaseID, device.DatabaseID, module, part)
			require.NoError(t, err)
			snapshot := func() string {
				t.Helper()
				var state string
				require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT jsonb_build_object(
					'tickets',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM repair_ticket r),
					'comments',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM repair_ticket_comment c),
					'audiences',(SELECT jsonb_agg(to_jsonb(a) ORDER BY org_id,ticket_id,account_id) FROM repair_ticket_audience a),
					'targets',(SELECT jsonb_agg(to_jsonb(t) ORDER BY org_id,ticket_id,kind,asset_id) FROM repair_ticket_target t),
					'modules',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM container_module m),
					'parts',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM repair_ticket_part p))::text`).Scan(&state))
				return state
			}
			before := snapshot()
			down, err := migrations.Current.ReadFile("current/001048_internal_client_ticketing.down.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(t.Context(), string(down))
			require.ErrorContains(t, err, "client ticketing has durable data; preserve it and fix forward")
			require.Equal(t, before, snapshot(), "refused rollback must preserve every ticket, comment, target, audience, model, and part record")
			var viewExists bool
			require.NoError(t, conn.QueryRowContext(t.Context(), `SELECT to_regclass('repair_ticket_asset_option') IS NOT NULL`).Scan(&viewExists))
			require.True(t, viewExists, "refusal must precede dropping client ticketing schema")
		})
	}
}
