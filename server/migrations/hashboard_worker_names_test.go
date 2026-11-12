package migrations_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	inventorypb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleetinventory"
	"github.com/block/proto-fleet/server/internal/domain/poolapplications"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestHashboardWorkerNamesMigration(t *testing.T) {
	database := testutil.NewDatabaseService(t, nil)
	user := database.CreateSuperAdminUser()
	ctx := t.Context()
	up, err := migrations.Current.ReadFile("current/001076_internal_hashboard_worker_names.up.sql")
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001076_internal_hashboard_worker_names.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(ctx, string(down))
	require.NoError(t, err)

	var id int64
	require.NoError(t, database.DB.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, user.OrganizationID).Scan(&id))
	_, err = database.DB.ExecContext(ctx, string(up))
	require.NoError(t, err)
	var name sql.NullString
	var mode string
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT worker_name,worker_name_mode FROM hashboard WHERE id=$1`, id).Scan(&name, &mode))
	require.False(t, name.Valid, "existing boards retain the plugin default")
	require.Equal(t, "none", mode)

	for _, tc := range []struct{ name, mode string }{
		{"", "none"}, {"aa:bb:cc:dd:ee:ff", "mac_address"}, {"rack-one", "custom"},
	} {
		_, err = database.DB.ExecContext(ctx, `UPDATE hashboard SET worker_name=$2,worker_name_mode=$3 WHERE id=$1`, id, tc.name, tc.mode)
		require.NoError(t, err)
		require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT worker_name,worker_name_mode FROM hashboard WHERE id=$1`, id).Scan(&name, &mode))
		require.Equal(t, sql.NullString{String: tc.name, Valid: true}, name)
		require.Equal(t, tc.mode, mode)
	}
	_, err = database.DB.ExecContext(ctx, `UPDATE hashboard SET worker_name_mode='invalid' WHERE id=$1`, id)
	require.ErrorContains(t, err, "hashboard_worker_name_mode_check")
	_, err = database.DB.ExecContext(ctx, `UPDATE hashboard SET worker_name_mode=NULL WHERE id=$1`, id)
	require.Error(t, err)

	_, err = database.DB.ExecContext(ctx, string(down))
	require.NoError(t, err)
	var columns, boards int
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='hashboard' AND column_name IN ('worker_name','worker_name_mode')`).Scan(&columns))
	require.Zero(t, columns, "rollback removes both columns")
	require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT count(*) FROM hashboard WHERE id=$1`, id).Scan(&boards))
	require.Equal(t, 1, boards, "rollback preserves the physical board")
	_, err = database.DB.ExecContext(ctx, string(up))
	require.NoError(t, err, "migration can be reapplied after rollback")
}

type workerMigrationCapabilities struct{}

func (workerMigrationCapabilities) GetRawCapabilitiesForDevice(context.Context, string, string, string) sdk.Capabilities {
	return sdk.Capabilities{sdk.CapabilityWorkerNameDefaultNone: true}
}

func TestHashboardWorkerNamesMigrationPreservesAssignedSuffixes(t *testing.T) {
	for _, tc := range []struct {
		name, targetKind, template, mac, expected string
		worker                                    sql.NullString
		retired, removed, unassigned, adopted     bool
		newerRevision                             bool
	}{
		{name: "board inherits controller MAC", targetKind: "hashboard_installation", template: "{miner_name}", mac: "AA:BB:CC:DD:EE:FF", expected: "AA:BB:CC:DD:EE:FF"},
		{name: "adopted board", targetKind: "hashboard_installation", template: "{miner_name}", mac: "aa:bb:cc:dd:ee:ff", expected: "aa:bb:cc:dd:ee:ff", adopted: true},
		{name: "assigned revision differs from latest", targetKind: "hashboard_installation", template: "{miner_name}", mac: "aa:bb:cc:dd:ee:ff", expected: "aa:bb:cc:dd:ee:ff", newerRevision: true},
		{name: "board inherits trimmed controller name", targetKind: "hashboard_installation", template: "{miner_name}", mac: "aa:bb:cc:dd:ee:ff", worker: sql.NullString{String: " controller-one ", Valid: true}, expected: "controller-one"},
		{name: "old empty name still used MAC", targetKind: "hashboard_installation", template: "rack-{miner_name}", mac: "aa:bb:cc:dd:ee:ff", worker: sql.NullString{Valid: true}, expected: "aa:bb:cc:dd:ee:ff"},
		{name: "controller identifier fallback", targetKind: "hashboard_installation", template: "{miner_name}", expected: "controller-id"},
		{name: "whole controller assignment", targetKind: "device", template: "{miner_name}", mac: "AA:BB:CC:DD:EE:FF", expected: "AA:BB:CC:DD:EE:FF"},
		{name: "whole controller trimmed name", targetKind: "device", template: "{miner_name}", worker: sql.NullString{String: " controller-one ", Valid: true}, expected: "controller-one"},
		{name: "no worker template", targetKind: "hashboard_installation", mac: "aa:bb:cc:dd:ee:ff"},
		{name: "literal worker template", targetKind: "hashboard_installation", template: "fixed-worker", mac: "aa:bb:cc:dd:ee:ff"},
		{name: "retired assignment", targetKind: "hashboard_installation", template: "{miner_name}", mac: "aa:bb:cc:dd:ee:ff", retired: true},
		{name: "removed installation", targetKind: "hashboard_installation", template: "{miner_name}", mac: "aa:bb:cc:dd:ee:ff", removed: true},
		{name: "unassigned board", targetKind: "hashboard_installation", template: "{miner_name}", mac: "aa:bb:cc:dd:ee:ff", unassigned: true},
		{name: "unassigned controller", targetKind: "device", template: "{miner_name}", mac: "aa:bb:cc:dd:ee:ff", unassigned: true},
		{name: "whole controller without worker template", targetKind: "device", mac: "aa:bb:cc:dd:ee:ff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := testutil.NewDatabaseService(t, nil)
			user := database.CreateSuperAdminUser()
			conn, ctx, org := database.DB, t.Context(), user.OrganizationID
			up, err := migrations.Current.ReadFile("current/001076_internal_hashboard_worker_names.up.sql")
			require.NoError(t, err)
			down, err := migrations.Current.ReadFile("current/001076_internal_hashboard_worker_names.down.sql")
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, string(down))
			require.NoError(t, err)

			var discovery, device, tank, module, board, installation int64
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,'controller-id','192.0.2.1','80','http','proto',2) RETURNING id`, org).Scan(&discovery))
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,discovered_device_id,mac_address,worker_name) VALUES($1,'controller-id',$2,$3,$4) RETURNING id`, org, discovery, tc.mac, tc.worker).Scan(&device))
			_, err = conn.ExecContext(ctx, `INSERT INTO device_write_fence(device_id,organization_id) VALUES($1,$2)`, device, org)
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, `INSERT INTO container_inventory(device_id,org_id,observed_at) VALUES($1,$2,now())`, device, org)
			require.NoError(t, err)
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, device).Scan(&tank))
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,1,'module-one',now(),now()) RETURNING id`, tank).Scan(&module))
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, org).Scan(&board))
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) VALUES($1,$2,1,now(),now(),'{"macAddress":"02:00:00:00:00:01"}') RETURNING id`, board, module).Scan(&installation))
			_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,1,'occupied',now())`, module, board)
			require.NoError(t, err)

			config, revision, operation := uuid.NewString(), uuid.NewString(), uuid.NewString()
			slots, err := json.Marshal([]map[string]any{{"id": uuid.NewString(), "url": "stratum+tcp://pool.example", "username": "account", "worker_template": tc.template, "password_ciphertext": "test-ciphertext", "has_password": false}})
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration(configuration_id,organization_id,name) VALUES($1,$2,'Existing pool')`, config, org)
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_revision(revision_id,organization_id,configuration_id,number,slots) VALUES($1,$2,$3,1,$4)`, revision, org, config, slots)
			require.NoError(t, err)
			_, err = conn.ExecContext(ctx, `UPDATE pool_configuration SET current_revision_id=$1 WHERE configuration_id=$2`, revision, config)
			require.NoError(t, err)
			if tc.newerRevision {
				latest := uuid.NewString()
				_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_revision(revision_id,organization_id,configuration_id,number,slots) VALUES($1,$2,$3,2,jsonb_set($4::jsonb,'{0,worker_template}','""'))`, latest, org, config, slots)
				require.NoError(t, err)
				_, err = conn.ExecContext(ctx, `UPDATE pool_configuration SET current_revision_id=$1 WHERE configuration_id=$2`, latest, config)
				require.NoError(t, err)
			}
			_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_operation(operation_id,organization_id,configuration_id,revision_id,actor_id) VALUES($1,$2,$3,$4,$5)`, operation, org, config, revision, user.DatabaseID)
			require.NoError(t, err)
			target := poolconfigurations.AssignmentTarget{Kind: tc.targetKind, ID: installation, DeviceID: device}
			if tc.targetKind == "device" {
				target.ID = device
			}
			if !tc.unassigned {
				_, err = conn.ExecContext(ctx, `INSERT INTO pool_configuration_assignment(organization_id,target_kind,target_id,device_id,configuration_id,revision_id,version,operation_id,retired_at,adopted_at) VALUES($1,$2,$3,$4,$5,$6,1,CASE WHEN $9 THEN NULL ELSE $7 END,CASE WHEN $8 THEN now() END,CASE WHEN $9 THEN now() END)`, org, tc.targetKind, target.ID, device, config, revision, operation, tc.retired, tc.adopted)
				require.NoError(t, err)
			}
			if tc.removed {
				_, err = conn.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=now() WHERE id=$1`, installation)
				require.NoError(t, err)
			}
			_, err = conn.ExecContext(ctx, string(up))
			require.NoError(t, err)
			var controllerName sql.NullString
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT worker_name FROM device WHERE id=$1`, device).Scan(&controllerName))
			expectedController := tc.worker
			if tc.targetKind == "device" && tc.expected != "" {
				expectedController = sql.NullString{String: tc.expected, Valid: true}
			}
			require.Equal(t, expectedController, controllerName)
			var name sql.NullString
			var mode string
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT worker_name,worker_name_mode FROM hashboard WHERE id=$1`, board).Scan(&name, &mode))
			if tc.expected == "" {
				require.False(t, name.Valid)
				require.Equal(t, "none", mode)
			} else {
				require.Equal(t, sql.NullString{String: tc.expected, Valid: true}, name)
				require.Equal(t, "custom", mode, "an inherited controller MAC is not the board's MAC strategy")
			}
			if tc.retired || tc.removed || tc.unassigned {
				return
			}

			q := sqlc.New(conn)
			runtime, err := q.GetPoolConfigurationRuntime(ctx, sqlc.GetPoolConfigurationRuntimeParams{OrgID: org, ID: device})
			require.NoError(t, err)
			reviewer := poolapplications.NewReviewer(nil, workerMigrationCapabilities{})
			workerID, workerName, err := reviewer.WorkerInputs(ctx, q, poolconfigurations.Scope{OrganizationID: org}, target, runtime)
			require.NoError(t, err)
			rendered, err := poolconfigurations.RenderWorker("account", tc.template, workerID, workerName)
			require.NoError(t, err)
			before, err := poolconfigurations.RenderWorker("account", tc.template, "controller-id", tc.expected)
			require.NoError(t, err)
			require.Equal(t, before, rendered, "readback and reapply must preserve the pre-upgrade pool username")
			inventory := fleetinventory.NewService(q)
			boards, err := inventory.Hashboards(ctx, org, &inventorypb.GetModuleHashboardsRequest{ModuleIds: []int64{module}}, []fleetinventory.Scope{{OrgWide: true}})
			require.NoError(t, err)
			require.Equal(t, tc.expected, boards.Modules[0].Slots[0].Board.WorkerName)
			require.Equal(t, mode, boards.Modules[0].Slots[0].Board.WorkerNameMode)
			listed, err := inventory.List(ctx, org, &inventorypb.ListInventoryRequest{View: inventorypb.InventoryView_INVENTORY_VIEW_MINING_ASSETS, Kinds: []inventorypb.EntityKind{inventorypb.EntityKind_ENTITY_KIND_MODULE}}, []fleetinventory.Scope{{OrgWide: true}})
			require.NoError(t, err)
			require.Len(t, listed.Items, 1)
			require.Equal(t, tc.expected, listed.Items[0].WorkerNameSummary)
		})
	}
}
