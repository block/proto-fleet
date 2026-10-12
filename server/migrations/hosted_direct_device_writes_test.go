package migrations_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	dbinfra "github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestDirectWriteRollbackPreservesCompletedDirectEvidence(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	attempt, err := devicewrite.NewService(database.DB).BeginHost(t.Context(), user.OrganizationID, device.DatabaseID, 0)
	require.NoError(t, err)
	require.NoError(t, devicewrite.Dispatch(attempt.Context(t.Context()), func(context.Context) error { return nil }))
	require.NoError(t, attempt.Finish(t.Context(), nil))
	// Explicitly seed qualified completion history for the rollback fixture.
	// A nil adapter result above now leaves the real attempt unresolved.
	_, err = database.DB.ExecContext(t.Context(), "UPDATE device_write_attempt SET status='complete' WHERE device_id=$1", device.DatabaseID)
	require.NoError(t, err)
	down, err := migrations.Current.ReadFile("current/001011_internal_direct_device_write_attempts.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.ErrorContains(t, err, "rollback is unsafe")
	var count int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND queue_message_id IS NULL AND status='complete'", device.DatabaseID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestRetiredWriteRollbackPreservesSafetyEvidence(t *testing.T) {
	for _, evidence := range []string{"running attempt", "deletion permit"} {
		t.Run(evidence, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			service := devicewrite.NewService(database.DB)
			q := sqlc.New(database.DB)
			keys, err := q.GetDeviceWriteIdentityKeys(t.Context(), device.DatabaseID)
			require.NoError(t, err)
			if evidence == "running attempt" {
				_, err = service.BeginHost(t.Context(), user.OrganizationID, device.DatabaseID, 0)
				require.NoError(t, err)
			} else {
				epoch, err := service.Reserve(t.Context(), user.OrganizationID, device.DatabaseID)
				require.NoError(t, err)
				require.NoError(t, q.CreateDeletionUnpairPermit(t.Context(), sqlc.CreateDeletionUnpairPermitParams{DeviceID: device.DatabaseID, OrganizationID: user.OrganizationID, Epoch: epoch, BindingHash: "fixture-binding-digest", IdentityKeys: keys}))
			}
			down, err := migrations.Current.ReadFile("current/001012_internal_retired_device_write_permits.down.sql")
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), string(down))
			require.ErrorContains(t, err, "rollback is unsafe")
			retained, err := q.GetDeviceWriteIdentityKeys(t.Context(), device.DatabaseID)
			require.NoError(t, err)
			require.Equal(t, keys, retained)
			var count int
			if evidence == "running attempt" {
				require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_write_attempt WHERE device_id=$1 AND identity_keys=$2 AND status='running'", device.DatabaseID, keys).Scan(&count))
			} else {
				require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM device_deletion_unpair_permit WHERE device_id=$1", device.DatabaseID).Scan(&count))
			}
			require.Equal(t, 1, count)
		})
	}
}

func TestRetiredIdentityUpgradeRequiresUnstartedHostedFeature(t *testing.T) {
	for _, status := range []string{"", "running", "unknown", "complete", "resolved"} {
		name := status
		if name == "" {
			name = "empty history"
		}
		t.Run(name, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			down, err := migrations.Current.ReadFile("current/001012_internal_retired_device_write_permits.down.sql")
			require.NoError(t, err)
			_, err = database.DB.ExecContext(t.Context(), string(down))
			require.NoError(t, err)
			if status != "" {
				require.NoError(t, sqlc.New(database.DB).EnsureDeviceWriteFences(t.Context(), []int64{device.DatabaseID}))
				_, err = database.DB.ExecContext(t.Context(), `INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,owner_instance_id,identity_snapshot,status,finished_at)
 VALUES($1,$2,$3,0,$4,'{"serial_number":"captured-miner"}',$5,CASE WHEN $5='running' THEN NULL ELSE statement_timestamp() END)`, uuid.New(), device.DatabaseID, user.OrganizationID, uuid.New(), status)
				require.NoError(t, err)
			}
			up, err := migrations.Current.ReadFile("current/001012_internal_retired_device_write_permits.up.sql")
			require.NoError(t, err)
			tx, err := database.DB.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			_, err = tx.ExecContext(t.Context(), string(up))
			if status == "" {
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
				_, err = devicewrite.NewService(database.DB).BeginHost(t.Context(), user.OrganizationID, device.DatabaseID, 0)
				require.NoError(t, err, "the final schema must admit the first hosted write")
				return
			}
			require.ErrorContains(t, err, "intermediate hosted schema upgrades are unsupported")
			require.NoError(t, tx.Rollback())
			var retainedStatus, snapshot string
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status,identity_snapshot::text FROM device_write_attempt WHERE device_id=$1", device.DatabaseID).Scan(&retainedStatus, &snapshot))
			require.Equal(t, status, retainedStatus)
			require.JSONEq(t, `{"serial_number":"captured-miner"}`, snapshot)
			var columnCount int
			require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='device_write_attempt' AND column_name='identity_keys'").Scan(&columnCount))
			require.Zero(t, columnCount, "a refused upgrade must preserve the earlier schema and evidence")
		})
	}
}

func TestRetiredIdentityUpgradeWaitsForUncommittedHistory(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	down, err := migrations.Current.ReadFile("current/001012_internal_retired_device_write_permits.down.sql")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), string(down))
	require.NoError(t, err)
	require.NoError(t, sqlc.New(database.DB).EnsureDeviceWriteFences(t.Context(), []int64{device.DatabaseID}))

	writer, err := database.DB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer writer.Rollback()
	var writerPID int
	require.NoError(t, writer.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&writerPID))
	attemptID := uuid.New()
	_, err = writer.ExecContext(t.Context(), `INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,owner_instance_id,identity_snapshot,status,finished_at)
 VALUES($1,$2,$3,0,$4,'{"serial_number":"captured-miner"}','unknown',statement_timestamp())`, attemptID, device.DatabaseID, user.OrganizationID, uuid.New())
	require.NoError(t, err)

	upgrade, err := database.DB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer upgrade.Rollback()
	var upgradePID int
	require.NoError(t, upgrade.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&upgradePID))
	up, err := migrations.Current.ReadFile("current/001012_internal_retired_device_write_permits.up.sql")
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		_, err := upgrade.ExecContext(t.Context(), string(up))
		result <- err
	}()
	// Observe the actual lock wait, rather than assuming goroutine scheduling.
	require.Eventually(t, func() bool {
		var blocked bool
		err := writer.QueryRowContext(t.Context(), "SELECT $1::int = ANY(pg_blocking_pids($2::int))", writerPID, upgradePID).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond, "upgrade must wait for the uncommitted attempt")
	require.NoError(t, writer.Commit())
	select {
	case err := <-result:
		require.ErrorContains(t, err, "intermediate hosted schema upgrades are unsupported")
	case <-time.After(5 * time.Second):
		t.Fatal("upgrade did not finish after the writer committed")
	}
	require.NoError(t, upgrade.Rollback())
	var status, snapshot string
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT status,identity_snapshot::text FROM device_write_attempt WHERE attempt_id=$1", attemptID).Scan(&status, &snapshot))
	require.Equal(t, "unknown", status)
	require.JSONEq(t, `{"serial_number":"captured-miner"}`, snapshot)
	var columnCount int
	require.NoError(t, database.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='device_write_attempt' AND column_name='identity_keys'").Scan(&columnCount))
	require.Zero(t, columnCount, "the refused upgrade must leave the earlier schema intact")
}

func TestRetiredIdentityLookupUsesOutstandingIndex(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	device := database.CreateDevice(user.OrganizationID, "proto")
	q := sqlc.New(database.DB)
	require.NoError(t, q.EnsureDeviceWriteFences(t.Context(), []int64{device.DatabaseID}))
	_, err = database.DB.ExecContext(t.Context(), `INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,status,owner_instance_id,identity_snapshot,identity_keys,finished_at)
 SELECT md5('attempt-'||n)::uuid,$1,$2,0,CASE WHEN n%2=0 THEN 'unknown' ELSE 'complete' END,md5('owner-'||n)::uuid,'{}',ARRAY['org:'||$2::bigint::text||':serial:'||n::text],statement_timestamp() FROM generate_series(1,40000) n`, device.DatabaseID, user.OrganizationID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), "UPDATE device SET deleted_at=statement_timestamp() WHERE id=$1", device.DatabaseID)
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), "VACUUM (ANALYZE) device_write_attempt")
	require.NoError(t, err)
	_, err = database.DB.ExecContext(t.Context(), "ANALYZE device")
	require.NoError(t, err)
	var plan []byte
	require.NoError(t, database.DB.QueryRowContext(t.Context(), `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT EXISTS(SELECT 1 WHERE NOT EXISTS(
 SELECT 1 FROM device_write_attempt a JOIN device d ON d.id=a.device_id WHERE d.deleted_at IS NOT NULL AND a.status NOT IN ('complete','resolved') AND a.identity_keys && $1::text[]))`, []string{fmt.Sprintf("org:%d:serial:40000", user.OrganizationID)}).Scan(&plan))
	require.True(t, json.Valid(plan))
	require.Contains(t, string(plan), "idx_device_write_attempt_outstanding_identity_keys")
	t.Logf("retired identity lookup over 40000 attempts: %s", plan)
	drained, err := q.IsRetiredDeviceWriteDrained(t.Context(), []string{fmt.Sprintf("org:%d:serial:40000", user.OrganizationID)})
	require.NoError(t, err)
	require.False(t, drained)
	drained, err = q.IsRetiredDeviceWriteDrained(t.Context(), []string{fmt.Sprintf("org:%d:serial:39999", user.OrganizationID)})
	require.NoError(t, err)
	require.True(t, drained, "completed retained history must not block admission")
}

func TestHostedUpgradePreflightPreservesCheckpointAndHistory(t *testing.T) {
	for _, status := range []string{"unknown", "complete", "resolved"} {
		t.Run(status, func(t *testing.T) {
			cfg, err := testutil.GetTestConfig()
			require.NoError(t, err)
			database := testutil.NewDatabaseService(t, cfg)
			user := database.CreateSuperAdminUser()
			device := database.CreateDevice(user.OrganizationID, "proto")
			ctx := t.Context()
			for _, name := range []string{"001013_internal_fleet_node_heartbeat", "001012_internal_retired_device_write_permits", "001011_internal_direct_device_write_attempts"} {
				down, err := migrations.Current.ReadFile("current/" + name + ".down.sql")
				require.NoError(t, err)
				_, err = database.DB.ExecContext(ctx, string(down))
				require.NoError(t, err)
			}
			_, err = database.DB.ExecContext(ctx, `UPDATE schema_migrations SET version=1010,dirty=false`)
			require.NoError(t, err)
			require.NoError(t, sqlc.New(database.DB).EnsureDeviceWriteFences(ctx, []int64{device.DatabaseID}))
			var messageID int64
			require.NoError(t, database.DB.QueryRowContext(ctx, `INSERT INTO queue_message(command_batch_log_uuid,device_id,command_type,status,retry_count) VALUES('prior-hosted-write',$1,'Reboot','PENDING',0) RETURNING id`, device.DatabaseID).Scan(&messageID))
			attemptID := uuid.New()
			_, err = database.DB.ExecContext(ctx, `INSERT INTO device_write_attempt(attempt_id,device_id,organization_id,epoch,queue_message_id,owner_instance_id,identity_snapshot,status,finished_at) VALUES($1,$2,$3,0,$4,$5,'{"serial_number":"prior-hosted-miner"}',$6,statement_timestamp())`, attemptID, device.DatabaseID, user.OrganizationID, messageID, uuid.New(), status)
			require.NoError(t, err)
			if status == "resolved" {
				_, err = database.DB.ExecContext(ctx, `INSERT INTO device_write_recovery(recovery_id,attempt_id,fence_epoch,prior_status,operator_name,evidence,evidence_sha256) VALUES($1,$2,0,'unknown','host-operator','{"process_terminated":true}',repeat('a',64))`, uuid.New(), attemptID)
				require.NoError(t, err)
			}
			snapshotEvidence := func() string {
				t.Helper()
				var evidence string
				require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT jsonb_build_object(
 'attempts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY attempt_id) FROM device_write_attempt a),
 'recovery',(SELECT jsonb_agg(to_jsonb(r) ORDER BY recovery_id) FROM device_write_recovery r),
 'fences',(SELECT jsonb_agg(to_jsonb(f) ORDER BY device_id) FROM device_write_fence f),
 'queue',(SELECT jsonb_agg(to_jsonb(q) ORDER BY id) FROM queue_message q))::text`).Scan(&evidence))
				return evidence
			}
			before := snapshotEvidence()
			cli := struct {
				DB dbinfra.Config `envprefix:"DB_" embed:""`
			}{}
			parser, err := kong.New(&cli)
			require.NoError(t, err)
			_, err = parser.Parse(nil)
			require.NoError(t, err)
			require.NoError(t, database.DB.QueryRowContext(ctx, "SELECT current_database()").Scan(&cli.DB.Name))
			upgraded, err := dbinfra.ConnectAndMigrate(&cli.DB)
			if upgraded != nil {
				upgraded.Close()
			}
			require.ErrorContains(t, err, "intermediate hosted schema upgrades are unsupported")
			var version int
			var dirty, nullable bool
			require.NoError(t, database.DB.QueryRowContext(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty))
			require.Equal(t, 1010, version, "the preflight must refuse before migration 1011 commits")
			require.False(t, dirty)
			require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT is_nullable='YES' FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='device_write_attempt' AND column_name='queue_message_id'`).Scan(&nullable))
			require.False(t, nullable, "the refused upgrade must preserve the earlier schema")
			var retainedStatus, snapshot string
			require.NoError(t, database.DB.QueryRowContext(ctx, `SELECT status,identity_snapshot::text FROM device_write_attempt WHERE attempt_id=$1`, attemptID).Scan(&retainedStatus, &snapshot))
			require.Equal(t, status, retainedStatus)
			require.JSONEq(t, `{"serial_number":"prior-hosted-miner"}`, snapshot)
			require.JSONEq(t, before, snapshotEvidence(), "refusal must preserve attempt, audit, fence, and queue evidence")
		})
	}
}
