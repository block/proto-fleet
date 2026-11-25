package migration

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestWholeContainerImportUsesCompleteBoardOwnership(t *testing.T) {
	for _, scenario := range []string{"unallocated", "one customer", "mixed ownership", "incomplete membership", "ownership changed after collection", "mining groups mode"} {
		t.Run(scenario, func(t *testing.T) {
			database := testutil.NewDatabaseService(t, nil)
			conn, ctx := database.DB, t.Context()
			org := database.CreateSuperAdminUser().OrganizationID
			cipher, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
			require.NoError(t, err)
			container := database.CreateDevice(org, "proto")
			miner := database.CreateDevice(org, "proto")
			for _, device := range []struct {
				id    int64
				class int
			}{{container.DatabaseID, 2}, {miner.DatabaseID, 1}} {
				_, err = conn.ExecContext(ctx, `UPDATE discovered_device SET device_class=$2 WHERE id=(SELECT discovered_device_id FROM device WHERE id=$1)`, device.id, device.class)
				require.NoError(t, err)
				_, err = sqlc.New(conn).UpsertDevicePairing(ctx, sqlc.UpsertDevicePairingParams{DeviceID: device.id, PairingStatus: sqlc.PairingStatusEnumPAIRED})
				require.NoError(t, err)
			}
			var customer int64
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) SELECT $1,$2,id,1,'Customer',15 FROM hosted_account WHERE organization_id=$2 AND depth=0 RETURNING id`, uuid.NewString(), org).Scan(&customer))
			at := time.Now().UTC().Truncate(time.Microsecond)
			_, err = sqlc.New(conn).AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: container.ID, ObservedAt: at})
			require.NoError(t, err)
			var tank int64
			require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,$2,$2) RETURNING id`, container.DatabaseID, at).Scan(&tank))
			var lastModule, lastBoard int64
			for modulePosition := 1; modulePosition <= 2; modulePosition++ {
				require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,$2,$3,$4,$4) RETURNING id`, tank, modulePosition, uuid.NewString(), at).Scan(&lastModule))
				for position := 1; position <= 3; position++ {
					require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,$2,$2) RETURNING id`, org, at).Scan(&lastBoard))
					_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at) VALUES($1,$2,$3,$4,$4)`, lastBoard, lastModule, position, at)
					require.NoError(t, err)
					_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,$3,'occupied',$4)`, lastModule, lastBoard, position, at)
					require.NoError(t, err)
					_, err = conn.ExecContext(ctx, `INSERT INTO hashboard_observation(hashboard_id,device_id,last_seen_at) VALUES($1,$2,$3)`, lastBoard, container.DatabaseID, at)
					require.NoError(t, err)
				}
			}
			allocate := func(all bool) {
				t.Helper()
				_, err := conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id) SELECT $1,7,h.id,$2,h.id,$3 FROM hashboard h WHERE h.org_id=$1 AND ($4 OR h.id=$5)`, org, container.DatabaseID, customer, all, lastBoard)
				require.NoError(t, err)
			}
			owner := int64(0)
			switch scenario {
			case "one customer":
				allocate(true)
				owner = customer
			case "mixed ownership":
				allocate(false)
			case "incomplete membership":
				_, err = conn.ExecContext(ctx, `DELETE FROM hashboard_slot WHERE module_id=$1 AND position=3`, lastModule)
				require.NoError(t, err)
			}
			password := "saved-password"
			snapshots, err := CollectDevices(ctx, sqlc.New(conn), cipher, org, snapshotReader(func(source sqlc.ListPoolDeviceImportSourcesRow) ([]interfaces.MinerStoredPool, error) {
				if scenario == "mining groups mode" && source.ID == container.DatabaseID {
					return nil, errMiningGroups
				}
				return []interfaces.MinerStoredPool{{URL: "stratum+tcp://pool.example", Username: "worker", Password: &password}}, nil
			}))
			require.NoError(t, err)
			if scenario == "ownership changed after collection" {
				allocate(false)
			}
			valid := scenario == "unallocated" || scenario == "one customer"
			// Exercise the final validator directly, independently of the collector's
			// filtering, and roll back the probe before running the maintenance import.
			checkAdoption := func(account int64, wantValid bool) {
				t.Helper()
				tx, err := conn.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer tx.Rollback()
				q := sqlc.New(tx)
				repo := poolconfigurations.NewRepository(cipher)
				scope := poolconfigurations.Scope{OrganizationID: org, AccountID: account}
				config, err := repo.Create(ctx, q, scope, poolconfigurations.RevisionOptions{Name: "Ownership probe", Slots: []poolconfigurations.SlotOptions{{URL: "stratum+tcp://pool.example", Username: "worker"}}})
				require.NoError(t, err)
				_, err = q.AdoptPoolConfigurationAssignment(ctx, sqlc.AdoptPoolConfigurationAssignmentParams{OrganizationID: org, TargetKind: poolconfigurations.TargetDevice, TargetID: container.DatabaseID, DeviceID: container.DatabaseID, ConfigurationID: config.ID, RevisionID: config.RevisionID})
				if wantValid {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "pool adoption requires the current equipment owner")
				}
				require.NoError(t, tx.Rollback())
			}
			if scenario != "mining groups mode" {
				checkAdoption(owner, valid)
			}
			if valid {
				wrongOwner := customer
				if owner == customer {
					wrongOwner = 0
				}
				checkAdoption(wrongOwner, false)
			}
			report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
				return Import(ctx, q, cipher, org, snapshots...)
			})
			require.NoError(t, err, "a container must not abort the ordinary miner's import")
			wantAssignments := 1
			if valid {
				wantAssignments = 2
				require.Empty(t, report.Unresolved)
			} else if scenario != "mining groups mode" {
				require.Len(t, report.Unresolved, 1)
			}
			require.Len(t, report.AdoptedAssignments, wantAssignments)
			if valid {
				freezeCutoverFixture(t, conn, org)
			} else {
				requireFreezeRejected(t, conn, org, "unmanaged_device")
			}
			if valid {
				var actualOwner int64
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT COALESCE(c.account_id,0) FROM pool_configuration_assignment a JOIN pool_configuration c ON c.configuration_id=a.configuration_id WHERE a.device_id=$1`, container.DatabaseID).Scan(&actualOwner))
				require.Equal(t, owner, actualOwner)
			}
			var operations int
			require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_operation WHERE organization_id=$1`, org).Scan(&operations))
			require.Zero(t, operations, "adoption records existing settings without dispatching a pool update")
		})
	}
}
