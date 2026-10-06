package command_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type firmwareProvenanceFixture struct {
	conn     *sql.DB
	q        *sqlc.Queries
	deviceID int64
	orgID    int64
	userID   int64
	batch    string
}

func newFirmwareProvenanceFixture(t *testing.T, commandType, batchOrg string) firmwareProvenanceFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	conn, database, user := setupMarkStatusTest(t)
	device := database.CreateDevice(user.OrganizationID, "proto")
	f := firmwareProvenanceFixture{conn: conn, q: sqlc.New(conn), deviceID: device.DatabaseID, orgID: user.OrganizationID, userID: user.DatabaseID, batch: "firmware-terminal-provenance"}
	org := sql.NullInt64{Int64: user.OrganizationID, Valid: true}
	if batchOrg == "legacy" {
		org = sql.NullInt64{}
	} else if batchOrg == "other" {
		require.NoError(t, conn.QueryRowContext(t.Context(), `INSERT INTO organization (org_id, name)
			VALUES ('other-terminal-org', 'Other terminal org') RETURNING id`).Scan(&org.Int64))
	}
	_, err := f.q.CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{
		Uuid: f.batch, Type: commandType, CreatedBy: user.DatabaseID, CreatedAt: time.Now(),
		Status: sqlc.BatchStatusEnumPROCESSING, DevicesCount: 1, OrganizationID: org,
		// A manual/legacy payload has no immutable checksum. Its terminal
		// result must still invalidate managed identity from a prior update.
		Payload: pqtype.NullRawMessage{RawMessage: []byte(`{"firmware_file_id":"old-upload-id"}`), Valid: true},
	})
	require.NoError(t, err)
	return f
}

func (f firmwareProvenanceFixture) recordTerminal(ctx context.Context, q sqlc.Querier, status sqlc.DeviceCommandStatusEnum) error {
	return q.UpsertCommandOnDeviceLog(ctx, sqlc.UpsertCommandOnDeviceLogParams{
		Uuid: f.batch, DeviceID: f.deviceID, Status: status, UpdatedAt: time.Now(),
	})
}

func (f firmwareProvenanceFixture) seedProvenance(t *testing.T) time.Time {
	t.Helper()
	var channelID, rolloutID int64
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `INSERT INTO release_channel (org_id, name, created_by)
		VALUES ($1, 'Prior firmware owner', $2) RETURNING id`, f.orgID, f.userID).Scan(&channelID))
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `INSERT INTO firmware_rollout
		(org_id, channel_id, manufacturer, model, firmware_checksum, firmware_version, assignment_generation)
		VALUES ($1, $2, 'TestCorp', 'TestMiner', 'prior-artifact', '2.0.0', 1) RETURNING id`, f.orgID, channelID).Scan(&rolloutID))
	var observed time.Time
	// A future timestamp also exercises monotonicity when wall clocks move
	// backwards: invalidation must advance the existing CAS witness.
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `INSERT INTO device_firmware_deployment
		(device_id, firmware_checksum, firmware_version, rollout_id, deployed_at)
		VALUES ($1, 'prior-artifact', '2.0.0', $2, now() + INTERVAL '1 day') RETURNING deployed_at`, f.deviceID, rolloutID).Scan(&observed))
	return observed
}

func (f firmwareProvenanceFixture) requireInvalidated(t *testing.T) time.Time {
	t.Helper()
	var checksum, version, witness string
	var rolloutID sql.NullInt64
	var stamp time.Time
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT firmware_checksum, firmware_version, rollout_id, deployed_at, last_command_batch_uuid
		FROM device_firmware_deployment WHERE device_id = $1`, f.deviceID).Scan(&checksum, &version, &rolloutID, &stamp, &witness))
	require.Empty(t, checksum)
	require.Empty(t, version)
	require.Equal(t, f.batch, witness)
	require.False(t, rolloutID.Valid)
	return stamp
}

func TestTerminalFirmwareCommandInvalidatesManagedIdentity(t *testing.T) {
	for _, outcome := range []sqlc.DeviceCommandStatusEnum{sqlc.DeviceCommandStatusEnumSUCCESS, sqlc.DeviceCommandStatusEnumFAILED} {
		for _, org := range []string{"current", "legacy"} {
			t.Run(string(outcome)+"/"+org, func(t *testing.T) {
				f := newFirmwareProvenanceFixture(t, "FirmwareUpdate", org)
				prior := f.seedProvenance(t)
				require.NoError(t, f.recordTerminal(t.Context(), f.q, outcome))
				first := f.requireInvalidated(t)
				require.True(t, first.After(prior))
				require.NoError(t, f.recordTerminal(t.Context(), f.q, outcome))
				require.True(t, f.requireInvalidated(t).After(first), "repeated terminal writes cannot reuse an earlier CAS witness")
			})
		}
	}
}

func TestUnrelatedCommandResultPreservesManagedIdentity(t *testing.T) {
	for _, tc := range []struct{ name, commandType, org string }{
		{"non-firmware", "Reboot", "current"},
		{"other organization", "FirmwareUpdate", "other"},
		{"missing batch", "FirmwareUpdate", "current"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFirmwareProvenanceFixture(t, tc.commandType, tc.org)
			prior := f.seedProvenance(t)
			if tc.name == "missing batch" {
				f.batch = "missing-batch"
			}
			require.NoError(t, f.recordTerminal(t.Context(), f.q, sqlc.DeviceCommandStatusEnumSUCCESS))
			var checksum string
			var stamp time.Time
			require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT firmware_checksum, deployed_at FROM device_firmware_deployment WHERE device_id = $1`, f.deviceID).Scan(&checksum, &stamp))
			require.Equal(t, "prior-artifact", checksum)
			require.Equal(t, prior, stamp)
		})
	}
}

func TestTerminalFirmwareInvalidationDefeatsStaleProvenanceWrites(t *testing.T) {
	for _, present := range []bool{false, true} {
		for _, completionFirst := range []bool{false, true} {
			name := "absent"
			if present {
				name = "present"
			}
			if completionFirst {
				name += "/completion first"
			} else {
				name += "/adoption first"
			}
			t.Run(name, func(t *testing.T) {
				f := newFirmwareProvenanceFixture(t, "FirmwareUpdate", "current")
				var observed time.Time
				checksum := ""
				if present {
					observed = f.seedProvenance(t)
					checksum = "prior-artifact"
				}
				adopt := func() {
					t.Helper()
					require.NoError(t, f.q.RecordFirmwareDeployment(t.Context(), sqlc.RecordFirmwareDeploymentParams{
						DeviceIds: []int64{f.deviceID}, FirmwareChecksum: "stale-rollout-artifact", FirmwareVersion: "2.0.0",
						ExpectedDeploymentPresent: []bool{present}, ExpectedDeployedAts: []time.Time{observed}, ExpectedFirmwareChecksums: []string{checksum},
					}))
				}
				if completionFirst {
					require.NoError(t, f.recordTerminal(t.Context(), f.q, sqlc.DeviceCommandStatusEnumSUCCESS))
					adopt()
				} else {
					adopt()
					require.NoError(t, f.recordTerminal(t.Context(), f.q, sqlc.DeviceCommandStatusEnumSUCCESS))
				}
				f.requireInvalidated(t)
			})
		}
	}
}

func TestFirmwareTerminalAuditAndInvalidationRollBackTogether(t *testing.T) {
	f := newFirmwareProvenanceFixture(t, "FirmwareUpdate", "current")
	prior := f.seedProvenance(t)
	messageID := f.seedProcessingFirmware(t)
	_, err := f.conn.ExecContext(t.Context(), `CREATE FUNCTION reject_test_provenance() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'test invalidation failure'; END; $$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_test_provenance BEFORE INSERT OR UPDATE ON device_firmware_deployment
		FOR EACH ROW EXECUTE FUNCTION reject_test_provenance()`)
	require.NoError(t, err)
	complete := func() error {
		return db.WithTransactionNoResult(t.Context(), f.conn, func(q sqlc.Querier) error {
			if _, err := q.UpdateMessageStatus(t.Context(), sqlc.UpdateMessageStatusParams{ID: messageID, Status: sqlc.QueueStatusEnumSUCCESS}); err != nil {
				return fmt.Errorf("update completion queue status: %w", err)
			}
			return f.recordTerminal(t.Context(), q, sqlc.DeviceCommandStatusEnumSUCCESS)
		})
	}
	require.ErrorContains(t, complete(), "test invalidation failure")
	require.Equal(t, sqlc.QueueStatusEnumPROCESSING, queryQueueStatus(t, f.conn, messageID))
	require.False(t, queryDeviceLogExists(t, f.conn, f.batch, f.deviceID))
	var stillPrior time.Time
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT deployed_at FROM device_firmware_deployment WHERE device_id = $1`, f.deviceID).Scan(&stillPrior))
	require.Equal(t, prior, stillPrior)
	_, err = f.conn.ExecContext(t.Context(), `DROP TRIGGER reject_test_provenance ON device_firmware_deployment; DROP FUNCTION reject_test_provenance()`)
	require.NoError(t, err)
	require.NoError(t, complete())
	require.Equal(t, sqlc.QueueStatusEnumSUCCESS, queryQueueStatus(t, f.conn, messageID))
	require.True(t, queryDeviceLogExists(t, f.conn, f.batch, f.deviceID))
	f.requireInvalidated(t)
}

func (f firmwareProvenanceFixture) seedProcessingFirmware(t *testing.T) int64 {
	t.Helper()
	require.NoError(t, f.q.CreateQueueMessage(t.Context(), sqlc.CreateQueueMessageParams{
		CommandBatchLogUuid: f.batch, CommandType: "FirmwareUpdate", DeviceID: f.deviceID,
		Status:  sqlc.QueueStatusEnumPROCESSING,
		Payload: pqtype.NullRawMessage{RawMessage: []byte(`{"firmware_file_id":"old-upload-id"}`), Valid: true},
	}))
	var messageID int64
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT id FROM queue_message
		WHERE command_batch_log_uuid = $1 AND device_id = $2`, f.batch, f.deviceID).Scan(&messageID))
	return messageID
}

// Real header/deployment lock inversion: reconciliation owns the header while
// completion owns provenance and its revision trigger waits on that header.
// Either transaction may be the deadlock victim; both retry only SQL, and stale
// adoption must not undo the committed queue result or identity invalidation.
func TestConcurrentFirmwareCompletionAndAdoptionSurviveDeadlock(t *testing.T) {
	f := newFirmwareProvenanceFixture(t, "FirmwareUpdate", "current")
	prior := f.seedProvenance(t)
	messageID := f.seedProcessingFirmware(t)
	var rolloutID int64
	require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT rollout_id FROM device_firmware_deployment
		WHERE device_id = $1`, f.deviceID).Scan(&rolloutID))
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	locked := make(chan struct{})
	adopt := make(chan struct{})
	adoptionDone := make(chan error, 1)
	completionDone := make(chan error, 1)
	var adoptionAttempts, completionAttempts atomic.Int32
	go func() {
		adoptionDone <- db.WithTransactionTimeoutNoResult(ctx, f.conn, 12*time.Second, func(q sqlc.Querier) error {
			attempt := adoptionAttempts.Add(1)
			if _, err := q.GetFirmwareRolloutForUpdate(ctx, sqlc.GetFirmwareRolloutForUpdateParams{
				RolloutID: rolloutID, OrgID: f.orgID,
			}); err != nil {
				return fmt.Errorf("lock adoption owner: %w", err)
			}
			if attempt == 1 {
				close(locked)
				select {
				case <-adopt:
				case <-ctx.Done():
					return fmt.Errorf("wait for competing completion: %w", ctx.Err())
				}
			}
			return q.RecordFirmwareDeployment(ctx, sqlc.RecordFirmwareDeploymentParams{
				DeviceIds: []int64{f.deviceID}, FirmwareChecksum: "stale-rollout-artifact", FirmwareVersion: "2.0.0",
				RolloutID:                 sql.NullInt64{Int64: rolloutID, Valid: true},
				ExpectedDeploymentPresent: []bool{true}, ExpectedDeployedAts: []time.Time{prior},
				ExpectedFirmwareChecksums: []string{"prior-artifact"},
			})
		})
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("adoption did not acquire the rollout header")
	}
	go func() {
		completionDone <- db.WithTransactionTimeoutNoResult(ctx, f.conn, 12*time.Second, func(q sqlc.Querier) error {
			completionAttempts.Add(1)
			if _, err := q.UpdateMessageStatus(ctx, sqlc.UpdateMessageStatusParams{ID: messageID, Status: sqlc.QueueStatusEnumSUCCESS}); err != nil {
				return fmt.Errorf("update competing completion queue: %w", err)
			}
			return f.recordTerminal(ctx, q, sqlc.DeviceCommandStatusEnumSUCCESS)
		})
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE datname = current_database() AND pid <> pg_backend_pid()
			AND query LIKE '%UpsertCommandOnDeviceLog%' AND wait_event_type = 'Lock'
			AND cardinality(pg_blocking_pids(pid)) > 0)`).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "completion must reach the provenance trigger while adoption holds the header")
	close(adopt)
	require.NoError(t, <-completionDone)
	require.NoError(t, <-adoptionDone)
	require.GreaterOrEqual(t, adoptionAttempts.Load()+completionAttempts.Load(), int32(3), "the actual deadlock must retry one whole transaction")
	require.Equal(t, sqlc.QueueStatusEnumSUCCESS, queryQueueStatus(t, f.conn, messageID))
	require.True(t, queryDeviceLogExists(t, f.conn, f.batch, f.deviceID))
	f.requireInvalidated(t)
}
