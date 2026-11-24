package migration

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

type adoptionFixture struct {
	conn          *sql.DB
	cipher        *encrypt.Service
	org, device   int64
	installations []int64
}

func legacyBoardFixture(t *testing.T) adoptionFixture {
	t.Helper()
	ctx := t.Context()
	f := adoptionFixture{conn: dbtest.GetTestDB(t)}
	var err error
	f.cipher, err = encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	var root, customer, discovery, tank int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Adoption') RETURNING id`, uuid.NewString()).Scan(&f.org))
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, f.org).Scan(&root))
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hosted_account(account_id,organization_id,parent_id,depth,name,capabilities) VALUES($1,$2,$3,1,'Customer',15) RETURNING id`, uuid.NewString(), f.org, root).Scan(&customer))
	identifier := uuid.NewString()
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.1','80','http','proto',2) RETURNING id`, f.org, identifier).Scan(&discovery))
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,$2,'02:00:00:00:00:01',$3) RETURNING id`, f.org, identifier, discovery).Scan(&f.device))
	_, err = sqlc.New(f.conn).AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: identifier, ObservedAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, sqlc.New(f.conn).EnsureMiningGroupState(ctx, sqlc.EnsureMiningGroupStateParams{OrganizationID: f.org, DeviceID: f.device}))
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO container_tank(device_id,position,first_seen_at,last_seen_at) VALUES($1,1,now(),now()) RETURNING id`, f.device).Scan(&tank))
	for i := range 4 {
		var module, board, installation int64
		require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO container_module(tank_id,position,reported_id,first_seen_at,last_seen_at) VALUES($1,$2,$3,now(),now()) RETURNING id`, tank, i+1, fmt.Sprintf("module-%d", i)).Scan(&module))
		require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard(org_id,first_seen_at,last_seen_at) VALUES($1,now(),now()) RETURNING id`, f.org).Scan(&board))
		mac := fmt.Sprintf("02:00:00:00:01:%02d", i)
		metadata, err := json.Marshal(map[string]string{"macAddress": mac})
		require.NoError(t, err)
		require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO hashboard_installation(hashboard_id,module_id,position,installed_at,last_seen_at,metadata) VALUES($1,$2,1,now(),now(),$3) RETURNING id`, board, module, metadata).Scan(&installation))
		f.installations = append(f.installations, installation)
		_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_slot(module_id,hashboard_id,position,occupancy,observed_at) VALUES($1,$2,1,'occupied',now())`, module, board)
		require.NoError(t, err)
		poolID, revisionID := uuid.NewString(), uuid.NewString()
		password := "imported-private-password"
		slots, err := poolconfigurations.EncryptSlots(f.cipher, []poolconfigurations.SlotOptions{{URL: "stratum+tcp://primary.example", Username: "payout", Password: &password}, {URL: "stratum+tcp://backup.example", Username: "backup"}}, nil)
		require.NoError(t, err)
		_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, poolID, f.org, root)
		require.NoError(t, err)
		_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_pool_revision(revision_id,organization_id,account_id,pool_id,number,name,slots) VALUES($1,$2,$3,$4,1,'Legacy stack',$5)`, revisionID, f.org, root, poolID, slots)
		require.NoError(t, err)
		_, err = f.conn.ExecContext(ctx, `INSERT INTO hashboard_pool_intent(organization_id,hashboard_id,device_id,installation_id,mac_address,account_id,pool_revision_id,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, f.org, board, f.device, installation, mac, root, revisionID, uuid.New())
		require.NoError(t, err)
		if i == 2 {
			_, err = f.conn.ExecContext(ctx, `INSERT INTO hosted_account_asset(organization_id,kind,asset_id,device_id,hashboard_id,client_id) VALUES($1,7,$2,$3,$2,$4)`, f.org, board, f.device, customer)
			require.NoError(t, err)
		}
		if i == 3 {
			_, err = f.conn.ExecContext(ctx, `UPDATE hashboard_installation SET removed_at=now() WHERE id=$1`, installation)
			require.NoError(t, err)
		}
	}
	return f
}

func TestImportAdoptsOnlyCurrentSameOwnerBoardIntentWithoutDispatch(t *testing.T) {
	f := legacyBoardFixture(t)
	run := func() poolconfigurations.ImportReport {
		t.Helper()
		report, err := db.WithTransaction(t.Context(), f.conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
			return Import(t.Context(), q, f.cipher, f.org)
		})
		require.NoError(t, err)
		return report
	}
	first := run()
	require.Equal(t, 1, first.CreatedConfigurations, "identical legacy stacks merge within their owner")
	require.Len(t, first.AdoptedAssignments, 2)
	require.Len(t, first.Unresolved, 2, "reallocated and removed boards need explicit review")
	requireFreezeRejected(t, f.conn, f.org, "legacy_board_intent")
	require.Equal(t, first.AdoptedAssignments[0].ConfigurationID, first.AdoptedAssignments[1].ConfigurationID)
	second := run()
	require.Zero(t, second.CreatedConfigurations)
	require.Equal(t, first.AdoptedAssignments, second.AdoptedAssignments)
	var assignments, operations int
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_configuration_assignment WHERE adopted_at IS NOT NULL AND operation_id IS NULL`).Scan(&assignments))
	require.Equal(t, 2, assignments)
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_configuration_operation`).Scan(&operations))
	require.Zero(t, operations)
	work, err := sqlc.New(f.conn).ListPendingPoolConfigurationWork(t.Context())
	require.NoError(t, err)
	require.Empty(t, work)
	encoded, err := json.Marshal(first)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "imported-private-password")
	require.NotContains(t, string(encoded), "ciphertext")
}

func TestImportReportsUnfinishedMiningGroupWorkWithoutBlockingLibrary(t *testing.T) {
	f := legacyBoardFixture(t)
	_, err := f.conn.ExecContext(t.Context(), `UPDATE container_mining_group_state SET status='blocked' WHERE device_id=$1`, f.device)
	require.NoError(t, err)
	report, err := db.WithTransaction(t.Context(), f.conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
		return Import(t.Context(), q, f.cipher, f.org)
	})
	require.NoError(t, err)
	require.Empty(t, report.AdoptedAssignments)
	require.Len(t, report.Unresolved, 4)
	require.Contains(t, report.Unresolved[0].Reason, "unfinished mining-group operation")
	var configurations int
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_configuration`).Scan(&configurations))
	require.Equal(t, 1, configurations, "library import is independent of hardware adoption")
}

func enqueueAdoptionCommand(t *testing.T, conn *sql.DB, deviceID int64, command, status string) {
	t.Helper()
	_, err := conn.ExecContext(t.Context(), `INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES($1,$2,$3,$4,0)`, uuid.NewString(), deviceID, command, status)
	require.NoError(t, err)
}

func TestImportAdoptionChecksQueuedCommands(t *testing.T) {
	for _, tc := range []struct {
		name, command, status string
		blocked               bool
	}{
		{"pending pool change", "UpdateMiningPools", "PENDING", true},
		{"processing pool change", "UpdateMiningPools", "PROCESSING", true},
		{"unknown pending command", "FuturePoolCommand", "PENDING", true},
		{"completed pool change", "UpdateMiningPools", "SUCCESS", false},
		{"failed pool change", "UpdateMiningPools", "FAILED", false},
		{"unrelated pending command", "BlinkLED", "PENDING", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := legacyBoardFixture(t)
			enqueueAdoptionCommand(t, f.conn, f.device, tc.command, tc.status)
			report, err := db.WithTransaction(t.Context(), f.conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
				return Import(t.Context(), q, f.cipher, f.org)
			})
			require.NoError(t, err)
			require.Equal(t, 1, report.CreatedConfigurations, "queued work does not block library import")
			if tc.blocked {
				require.Empty(t, report.AdoptedAssignments)
				require.Len(t, report.Unresolved, 4)
				for _, issue := range report.Unresolved {
					require.Contains(t, issue.Reason, "unfinished")
				}
			} else {
				require.Len(t, report.AdoptedAssignments, 2)
			}
			var status string
			require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT status FROM queue_message WHERE device_id=$1`, f.device).Scan(&status))
			require.Equal(t, tc.status, status, "adoption must not cancel or dispatch queued work")
			blocked, err := blockedPoolDevices(t.Context(), sqlc.New(f.conn), f.org+1)
			require.NoError(t, err)
			require.Empty(t, blocked, "queue checks remain scoped to the importing organization")
		})
	}
}

func TestImportAdoptionLocksQueueUntilCommit(t *testing.T) {
	conn := dbtest.GetTestDB(t)
	tx, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, sqlc.New(tx).LockPoolAssignmentAdoptionSources(t.Context()))
	other, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer other.Rollback()
	_, err = other.ExecContext(t.Context(), `LOCK TABLE queue_message IN ROW EXCLUSIVE MODE NOWAIT`)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr, "enqueue and status changes cannot race the final adoption check")
	require.Equal(t, "55P03", pgErr.Code)
	require.NoError(t, tx.Commit())
	require.NoError(t, other.Rollback())
	// A new writer can proceed after the import transaction releases its lock.
	writer, err := conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer writer.Rollback()
	_, err = writer.ExecContext(t.Context(), `LOCK TABLE queue_message IN ROW EXCLUSIVE MODE NOWAIT`)
	require.NoError(t, err)
}
