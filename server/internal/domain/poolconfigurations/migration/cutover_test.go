package migration

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/testutil/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func newCutoverFixture(t *testing.T) (*sql.DB, *encrypt.Service, int64) {
	t.Helper()
	conn := dbtest.GetTestDB(t)
	cipher, err := encrypt.NewService(&encrypt.Config{ServiceMasterKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	require.NoError(t, err)
	var org int64
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO organization(org_id,name) VALUES($1,'Cutover') RETURNING id`, uuid.NewString()).Scan(&org))
	return conn, cipher, org
}

func freezeCutoverFixture(t *testing.T, conn *sql.DB, org int64) {
	t.Helper()
	require.NoError(t, db.WithTransactionNoResult(t.Context(), conn, func(q sqlc.Querier) error {
		return FreezeCutover(t.Context(), q, org)
	}))
}

func newCutoverDevice(t *testing.T, conn *sql.DB, org int64) int64 {
	t.Helper()
	var discovery, device int64
	identifier := uuid.NewString()
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.1','80','http','proto',1) RETURNING id`, org, identifier).Scan(&discovery))
	require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,$2,'02:00:00:00:00:01',$3) RETURNING id`, org, identifier, discovery).Scan(&device))
	return device
}

func TestCutoverRequiresFrozenWritersAndCompleteLibraryImport(t *testing.T) {
	conn, cipher, org := newCutoverFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	device := newCutoverDevice(t, conn, org)
	enqueueAdoptionCommand(t, conn, device, "UpdateMiningPools", "SUCCESS")
	_, err := conn.ExecContext(ctx, `INSERT INTO pool(org_id,pool_name,url,username,password_enc) VALUES($1,'Legacy pool','stratum+tcp://pool.example','worker','')`, org)
	require.NoError(t, err)
	before, err := InspectCutover(ctx, q, org)
	require.NoError(t, err)
	require.False(t, before.Frozen)
	require.False(t, before.Complete)
	require.Len(t, before.Gaps, 1)
	require.Equal(t, "legacy_library", before.Gaps[0].Source.Kind)
	complete := func() (CutoverReport, error) {
		return db.WithTransaction(ctx, conn, func(q sqlc.Querier) (CutoverReport, error) {
			return CompleteCutover(ctx, q, org)
		})
	}
	_, err = complete()
	require.ErrorContains(t, err, "freeze")
	frozen, err := q.PoolConfigurationWritersFrozen(ctx, org)
	require.NoError(t, err)
	require.False(t, frozen, "a failed completion must not silently freeze an organization")
	requireFreezeRejected(t, conn, org, "legacy_library")
	assertLegacyInsertRejected := func() {
		t.Helper()
		_, err := conn.ExecContext(ctx, `INSERT INTO pool(org_id,pool_name,url,username,password_enc) VALUES($1,'Late writer','stratum+tcp://late.example','worker','')`, org)
		require.ErrorContains(t, err, "legacy pool writes are disabled")
		var root int64
		require.NoError(t, conn.QueryRowContext(ctx, `SELECT id FROM hosted_account WHERE organization_id=$1 AND depth=0`, org).Scan(&root))
		_, err = conn.ExecContext(ctx, `INSERT INTO hosted_account_pool(pool_id,organization_id,account_id) VALUES($1,$2,$3)`, uuid.NewString(), org, root)
		require.ErrorContains(t, err, "legacy pool writes are disabled")
		_, err = conn.ExecContext(ctx, `INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES($1,$2,'UpdateMiningPools','PENDING',0)`, uuid.NewString(), device)
		require.ErrorContains(t, err, "legacy pool commands are disabled")
		_, err = conn.ExecContext(ctx, `UPDATE queue_message SET status='PENDING' WHERE device_id=$1 AND command_type='UpdateMiningPools'`, device)
		require.ErrorContains(t, err, "legacy pool commands are disabled", "retrying an old message must not bypass the admission freeze")
	}
	imported, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
		return Libraries(ctx, q, cipher, org)
	})
	require.NoError(t, err)
	require.Equal(t, 1, imported.CreatedConfigurations)
	require.Empty(t, imported.Unresolved)
	ready, err := InspectCutover(ctx, q, org)
	require.NoError(t, err)
	require.False(t, ready.Frozen)
	require.Empty(t, ready.Gaps)
	freezeCutoverFixture(t, conn, org)
	assertLegacyInsertRejected()
	completed, err := complete()
	require.NoError(t, err)
	require.True(t, completed.Complete)
	assertLegacyInsertRejected()
	var completedAt time.Time
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT completed_at FROM pool_configuration_cutover WHERE organization_id=$1`, org).Scan(&completedAt))
	again, err := complete()
	require.NoError(t, err)
	require.True(t, again.Complete)
	require.Empty(t, again.Gaps)
	var rerunAt time.Time
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT completed_at FROM pool_configuration_cutover WHERE organization_id=$1`, org).Scan(&rerunAt))
	require.Equal(t, completedAt, rerunAt, "reruns preserve the original cutover record")
	var otherOrg int64
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO organization(org_id,name) VALUES($1,'Other operator') RETURNING id`, uuid.NewString()).Scan(&otherOrg))
	_, err = conn.ExecContext(ctx, `INSERT INTO pool(org_id,pool_name,url,username,password_enc) VALUES($1,'Unfrozen operator','stratum+tcp://other.example','worker','')`, otherOrg)
	require.NoError(t, err, "writer fencing must remain scoped to the cutover organization")
}

func TestCutoverReportsUnknownDeviceCredentialsAndKeepsGateClosed(t *testing.T) {
	conn, cipher, org := newCutoverFixture(t)
	ctx := t.Context()
	q := sqlc.New(conn)
	var discovery, device int64
	identifier := uuid.NewString()
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO discovered_device(org_id,device_identifier,ip_address,port,url_scheme,driver_name,device_class) VALUES($1,$2,'192.0.2.1','80','http','proto',1) RETURNING id`, org, identifier).Scan(&discovery))
	require.NoError(t, conn.QueryRowContext(ctx, `INSERT INTO device(org_id,device_identifier,mac_address,discovered_device_id) VALUES($1,$2,'02:00:00:00:00:01',$3) RETURNING id`, org, identifier, discovery).Scan(&device))
	_, err := conn.ExecContext(ctx, `INSERT INTO device_pairing(device_id,pairing_status) VALUES($1,'PAIRED')`, device)
	require.NoError(t, err)
	requireFreezeRejected(t, conn, org, "unmanaged_device")
	reads := 0
	snapshots, err := CollectDevices(ctx, q, cipher, org, snapshotReader(func(source sqlc.ListPoolDeviceImportSourcesRow) ([]interfaces.MinerStoredPool, error) {
		reads++
		require.Equal(t, device, source.ID)
		return []interfaces.MinerStoredPool{{URL: "stratum+tcp://pool.example", Username: "worker", Password: nil}}, nil
	}))
	require.NoError(t, err)
	require.Equal(t, 1, reads)
	report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
		return Import(ctx, q, cipher, org, snapshots...)
	})
	require.NoError(t, err)
	require.Empty(t, report.AdoptedAssignments)
	require.Len(t, report.Unresolved, 1)
	require.Contains(t, report.Unresolved[0].Reason, "password is unknown")
	inspection, err := InspectCutover(ctx, q, org)
	require.NoError(t, err)
	require.False(t, inspection.Frozen)
	require.False(t, inspection.Complete)
	require.Len(t, inspection.Gaps, 1)
	require.Equal(t, "unmanaged_device", inspection.Gaps[0].Source.Kind)
	requireFreezeRejected(t, conn, org, "unmanaged_device")
	complete, err := q.HasPoolConfigurationCutover(ctx, org)
	require.NoError(t, err)
	require.False(t, complete)
	encoded, err := json.Marshal(inspection)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "ciphertext")
	var assignments, attempts, queued int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool_configuration_assignment WHERE device_id=$1`, device).Scan(&assignments))
	require.Zero(t, assignments, "missing readback credentials must not become a reusable configuration")
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, device).Scan(&attempts))
	require.Zero(t, attempts)
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message WHERE device_id=$1`, device).Scan(&queued))
	require.Zero(t, queued)
}

func TestCutoverRejectsUnsettledPoolWork(t *testing.T) {
	for _, scenario := range []struct {
		name, source, status, command, kind string
		blocked                             bool
	}{
		{"pending queue", "queue", "PENDING", "UpdateMiningPools", "mutation", true},
		{"processing queue", "queue", "PROCESSING", "UpdateMiningPools", "mutation", true},
		{"unknown queue command", "queue", "PENDING", "FuturePoolCommand", "mutation", true},
		{"completed queue", "queue", "SUCCESS", "UpdateMiningPools", "mutation", false},
		{"unrelated queue", "queue", "PENDING", "BlinkLED", "mutation", false},
		{"running attempt", "attempt", "running", "UpdateMiningPools", "mutation", true},
		{"uncertain attempt", "attempt", "unknown", "UpdateMiningPools", "mutation", true},
		{"uncertain group attempt", "attempt", "unknown", "MiningGroups", "mutation", true},
		{"unrelated attempt", "attempt", "running", "BlinkLED", "mutation", false},
		{"read-only observation", "attempt", "running", "UpdateMiningPools", "observation", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			conn, _, org := newCutoverFixture(t)
			ctx := t.Context()
			q := sqlc.New(conn)
			device := newCutoverDevice(t, conn, org)
			if scenario.source == "queue" {
				enqueueAdoptionCommand(t, conn, device, scenario.command, scenario.status)
			} else {
				require.NoError(t, q.EnsureDeviceWriteFences(ctx, []int64{device}))
				_, err := conn.ExecContext(ctx, `INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,owner_instance_id,identity_snapshot,status,finished_at,command_type,operation_kind)
 VALUES($1,$2,$3,0,$4,'{}',$5,CASE WHEN $5='running' THEN NULL ELSE clock_timestamp() END,$6,$7)`, uuid.New(), device, org, uuid.New(), scenario.status, scenario.command, scenario.kind)
				require.NoError(t, err)
			}
			freezeErr := db.WithTransactionNoResult(ctx, conn, func(q sqlc.Querier) error {
				return FreezeCutover(ctx, q, org)
			})
			if scenario.blocked {
				require.ErrorContains(t, freezeErr, "unresolved")
				frozen, err := q.LegacyPoolWritersDisabled(ctx, org)
				require.NoError(t, err)
				require.False(t, frozen, "rejected freeze leaves drain and recovery available")
				// Exercise completion's independent defense against unfinished
				// work even if a marker was inserted outside the maintenance API.
				require.NoError(t, q.FreezePoolConfigurationWriters(ctx, org))
			} else {
				require.NoError(t, freezeErr)
			}
			inspection, err := InspectCutover(ctx, q, org)
			require.NoError(t, err)
			if scenario.blocked {
				require.Len(t, inspection.Gaps, 1)
				require.Equal(t, "unfinished_work", inspection.Gaps[0].Source.Kind)
			} else {
				require.Empty(t, inspection.Gaps, "independent or settled work cannot block cutover")
			}
			report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (CutoverReport, error) {
				return CompleteCutover(ctx, q, org)
			})
			if scenario.blocked {
				require.ErrorContains(t, err, "unresolved")
				complete, err := q.HasPoolConfigurationCutover(ctx, org)
				require.NoError(t, err)
				require.False(t, complete)
			} else {
				require.NoError(t, err)
				require.True(t, report.Complete)
			}
			var status string
			if scenario.source == "queue" {
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT status FROM queue_message WHERE device_id=$1`, device).Scan(&status))
			} else {
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT status FROM device_write_attempt WHERE device_id=$1`, device).Scan(&status))
			}
			require.Equal(t, scenario.status, status, "the gate must not cancel queued work or invent attempt resolution")
		})
	}
}

func TestCutoverFreezeRejectsLegacyWritersAlreadyWaitingOnFreeze(t *testing.T) {
	for _, source := range []string{"library", "queue"} {
		t.Run(source, func(t *testing.T) {
			conn, _, org := newCutoverFixture(t)
			ctx := t.Context()
			device := newCutoverDevice(t, conn, org)
			// This writer transaction predates the freeze, so its INSERT must
			// recheck the committed marker after waiting for the cutover barrier.
			writer, err := conn.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer writer.Rollback()
			var writerPID int
			require.NoError(t, writer.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))
			freeze, err := conn.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer freeze.Rollback()
			q := sqlc.New(freeze)
			require.NoError(t, FreezeCutover(ctx, q, org))
			result := make(chan error, 1)
			go func() {
				var err error
				if source == "library" {
					_, err = writer.ExecContext(ctx, `INSERT INTO pool(org_id,pool_name,url,username,password_enc) VALUES($1,'Concurrent legacy writer','stratum+tcp://pool.example','worker','')`, org)
				} else {
					_, err = writer.ExecContext(ctx, `INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES($1,$2,'UpdateMiningPools','PENDING',0)`, uuid.NewString(), device)
				}
				result <- err
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err := conn.QueryRowContext(ctx, `SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1`, writerPID).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 5*time.Millisecond, "the legacy writer must wait until the freeze transaction commits")
			require.NoError(t, freeze.Commit())
			select {
			case err := <-result:
				var pgErr *pgconn.PgError
				require.ErrorAs(t, err, &pgErr)
				require.Equal(t, "23514", pgErr.Code)
				require.Contains(t, pgErr.Message, "legacy pool")
			case <-time.After(5 * time.Second):
				t.Fatal("legacy writer did not finish after freeze committed")
			}
			var records int
			if source == "library" {
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM pool WHERE org_id=$1`, org).Scan(&records))
			} else {
				require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message WHERE device_id=$1`, device).Scan(&records))
			}
			require.Zero(t, records, "a pre-freeze transaction cannot commit new legacy state after the barrier")
		})
	}
}

func TestCutoverFreezeWaitsForPreviouslyAdmittedLegacyCommand(t *testing.T) {
	conn, _, org := newCutoverFixture(t)
	ctx := t.Context()
	device := newCutoverDevice(t, conn, org)
	writer, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer writer.Rollback()
	_, err = writer.ExecContext(ctx, `INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES($1,$2,'UpdateMiningPools','PENDING',0)`, uuid.NewString(), device)
	require.NoError(t, err)
	freeze, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer freeze.Rollback()
	_, err = freeze.ExecContext(ctx, `SET LOCAL lock_timeout='100ms'`)
	require.NoError(t, err)
	err = sqlc.New(freeze).FreezePoolConfigurationWriters(ctx, org)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "55P03", pgErr.Code, "freeze must wait for writers already admitted through the old command path")
	require.NoError(t, freeze.Rollback())
	// The maintenance entry point also waits for that admission, then rejects
	// the newly visible work instead of committing a marker that blocks recovery.
	safeFreeze, err := conn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer safeFreeze.Rollback()
	var freezePID int
	require.NoError(t, safeFreeze.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&freezePID))
	result := make(chan error, 1)
	go func() { result <- FreezeCutover(ctx, sqlc.New(safeFreeze), org) }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := conn.QueryRowContext(ctx, `SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1`, freezePID).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 5*time.Millisecond)
	require.NoError(t, writer.Commit())
	select {
	case err := <-result:
		require.ErrorContains(t, err, "unresolved", "the prior writer's queued command must prevent freezing")
	case <-time.After(6 * time.Second):
		t.Fatal("freeze did not finish after the admitted writer committed")
	}
	require.NoError(t, safeFreeze.Rollback())
	inspection, err := InspectCutover(ctx, sqlc.New(conn), org)
	require.NoError(t, err)
	require.False(t, inspection.Complete)
	require.False(t, inspection.Frozen)
	require.Len(t, inspection.Gaps, 1)
	require.Equal(t, "unfinished_work", inspection.Gaps[0].Source.Kind)
}

func TestImportReportsAmbiguousRetainedGroupsWithoutAbortingLibraryProgress(t *testing.T) {
	f := legacyBoardFixture(t)
	ctx := t.Context()
	var revision string
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT pool_revision_id FROM hashboard_pool_intent WHERE installation_id=$1`, f.installations[0]).Scan(&revision))
	_, err := f.conn.ExecContext(ctx, `UPDATE hashboard_pool_intent SET pool_revision_id=$1 WHERE installation_id IN ($2,$3)`, revision, f.installations[0], f.installations[1])
	require.NoError(t, err)
	bindings, err := json.Marshal(map[string]any{
		"retained-a": map[string]any{"config_id": revision, "configured": true},
		"retained-b": map[string]any{"config_id": revision, "configured": true},
	})
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET bindings=$2,observed='{}' WHERE device_id=$1`, f.device, bindings)
	require.NoError(t, err)
	run := func() poolconfigurations.ImportReport {
		t.Helper()
		report, err := db.WithTransaction(ctx, f.conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
			return Import(ctx, q, f.cipher, f.org)
		})
		require.NoError(t, err, "ambiguous membership must be reported without rolling back unrelated import progress")
		return report
	}
	for iteration := range 2 {
		report := run()
		if iteration == 0 {
			require.Equal(t, 1, report.CreatedConfigurations)
		} else {
			require.Zero(t, report.CreatedConfigurations)
		}
		require.Empty(t, report.AdoptedAssignments)
		ambiguous := 0
		for _, issue := range report.Unresolved {
			if issue.Source.Kind == "hashboard_intent" && strings.Contains(issue.Reason, "ambiguous") {
				ambiguous++
			}
		}
		require.Equal(t, 2, ambiguous, "equal legacy revisions alone cannot choose between retained controller groups")
		intents, err := sqlc.New(f.conn).ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: f.org, DeviceID: f.device})
		require.NoError(t, err)
		require.Empty(t, intents)
	}
	var macs [2]string
	for index := range macs {
		require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT mac_address FROM hashboard_pool_intent WHERE installation_id=$1`, f.installations[index]).Scan(&macs[index]))
	}
	observation, err := json.Marshal(map[string]any{"enabled": true, "groups": []map[string]any{{"id": "retained-a", "macs": macs[:1]}, {"id": "retained-b", "macs": macs[1:]}}})
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `UPDATE container_mining_group_state SET observed=$2 WHERE device_id=$1`, f.device, observation)
	require.NoError(t, err)
	repaired := run()
	require.Len(t, repaired.AdoptedAssignments, 2, "a later authoritative membership observation can finish partially completed import")
	intents, err := sqlc.New(f.conn).ListPoolGroupIntents(ctx, sqlc.ListPoolGroupIntentsParams{OrganizationID: f.org, DeviceID: f.device})
	require.NoError(t, err)
	require.Len(t, intents, 2)
	require.NotEqual(t, intents[0].GroupSnapshotID.String, intents[1].GroupSnapshotID.String)
	var attempts, queued int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM device_write_attempt WHERE device_id=$1`, f.device).Scan(&attempts))
	require.Zero(t, attempts)
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM queue_message WHERE device_id=$1`, f.device).Scan(&queued))
	require.Zero(t, queued)
}
