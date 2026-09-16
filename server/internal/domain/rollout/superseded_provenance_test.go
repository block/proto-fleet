package rollout

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/sqlc-dev/pqtype"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
)

func createProvenanceCommand(t *testing.T, f *fixture, payload map[string]string, legacyOrg bool) string {
	t.Helper()
	uuid := id.GenerateID()
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	_, err = f.svc.store.GetQueries(t.Context()).CreateCommandBatchLog(t.Context(), sqlc.CreateCommandBatchLogParams{
		Uuid: uuid, Type: "FirmwareUpdate", CreatedBy: testActor.ID,
		CreatedAt: time.Now(), Status: sqlc.BatchStatusEnumPROCESSING, DevicesCount: 1,
		Payload:        pqtype.NullRawMessage{RawMessage: encoded, Valid: true},
		OrganizationID: sql.NullInt64{Int64: f.orgID, Valid: !legacyOrg},
	})
	require.NoError(t, err)
	return uuid
}

func completeProvenanceCommand(t *testing.T, f *fixture, uuid string, status sqlc.DeviceCommandStatusEnum) {
	t.Helper()
	require.NoError(t, f.svc.store.GetQueries(t.Context()).UpsertCommandOnDeviceLog(t.Context(), sqlc.UpsertCommandOnDeviceLogParams{
		Uuid: uuid, DeviceID: f.deviceIDs["miner-0"], Status: status, UpdatedAt: time.Now(),
	}))
}

func latestProvenanceDispatch(t *testing.T, f *fixture, rolloutID int64) sqlc.ListFirmwareRolloutDevicesRow {
	t.Helper()
	rows, err := f.svc.store.GetQueries(t.Context()).ListFirmwareRolloutDevices(t.Context(), rolloutID)
	require.NoError(t, err)
	for _, row := range rows {
		if row.DeviceIdentifier == "miner-0" {
			return row
		}
	}
	t.Fatal("missing rollout target miner-0")
	return sqlc.ListFirmwareRolloutDevicesRow{}
}

func TestLaterFirmwareResultSupersedesRolloutProvenance(t *testing.T) {
	for _, tc := range []struct {
		name      string
		payload   map[string]string
		status    sqlc.DeviceCommandStatusEnum
		legacyOrg bool
	}{
		{"different checksum success", map[string]string{"firmware_checksum": checksum1}, sqlc.DeviceCommandStatusEnumSUCCESS, false},
		{"different checksum failed", map[string]string{"firmware_checksum": checksum1}, sqlc.DeviceCommandStatusEnumFAILED, false},
		{"same checksum failed", map[string]string{"firmware_checksum": checksum2}, sqlc.DeviceCommandStatusEnumFAILED, false},
		{"legacy matching file ID", map[string]string{"firmware_file_id": "fw-2"}, sqlc.DeviceCommandStatusEnumSUCCESS, false},
		{"legacy missing organization", map[string]string{"firmware_file_id": "fw-1"}, sqlc.DeviceCommandStatusEnumSUCCESS, true},
	} {
		for _, recorded := range []bool{false, true} {
			name := tc.name + "/before first observation"
			if recorded {
				name = tc.name + "/after verification"
			}
			t.Run(name, func(t *testing.T) {
				f := newFixture(t, 2)
				useAuditedModelDispatcher(t, f)
				f.channel(t, pilotOf1, f.allMiners()...)
				// Its batch was created before the rollout's batch, but its
				// result will finish afterward. Completion orders the evidence.
				foreign := createProvenanceCommand(t, f, tc.payload, tc.legacyOrg)
				started := f.apply(t, "fw-2")
				f.svc.EnforceTick(t.Context())
				own := latestProvenanceDispatch(t, f, started.ID)
				completeProvenanceCommand(t, f, own.LastDispatchedBatchUuid.String, sqlc.DeviceCommandStatusEnumSUCCESS)
				f.finishUpdate(t, "miner-0", "2.0.0")
				if recorded {
					f.svc.EnforceTick(t.Context())
					require.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))
					require.EqualValues(t, 1, f.rigGroup(t).OnTargetCount)
				}

				completeProvenanceCommand(t, f, foreign, tc.status)
				require.Zero(t, f.rigGroup(t).OnTargetCount, "a completed overriding command immediately invalidates channel counts")
				require.False(t, latestProvenanceDispatch(t, f, started.ID).LastDispatchSucceeded)
				f.svc.EnforceTick(t.Context())
				current := f.rollout(t, started.ID)
				require.NotEqual(t, PhaseDone, phaseOf(current, "miner-0"), "matching version cannot reuse a superseded success")
				require.Empty(t, deviceOf(t, current, "miner-0").LastDeployedFirmwareChecksum)
				require.Equal(t, PhaseQueued, phaseOf(current, "miner-1"))

				// A fresh successful corrective attempt supplies current
				// evidence and can replace the invalidation marker.
				f.backdateSends(t)
				f.svc.EnforceTick(t.Context())
				correction := latestProvenanceDispatch(t, f, started.ID)
				require.NotEqual(t, own.LastDispatchedBatchUuid.String, correction.LastDispatchedBatchUuid.String)
				completeProvenanceCommand(t, f, correction.LastDispatchedBatchUuid.String, sqlc.DeviceCommandStatusEnumSUCCESS)
				f.svc.EnforceTick(t.Context())
				require.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))
				require.EqualValues(t, 1, f.rigGroup(t).OnTargetCount)
				var witness string
				require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT last_command_batch_uuid
					FROM device_firmware_deployment WHERE device_id = $1`, f.deviceIDs["miner-0"]).Scan(&witness))
				require.Equal(t, correction.LastDispatchedBatchUuid.String, witness, "adoption retains the latest command witness")
			})
		}
	}
}

func TestFirmwareCompletionInvalidatesStaleProvenanceObservation(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "absent"
		if existing {
			name = "existing"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 1)
			dispatcher := useAuditedModelDispatcher(t, f)
			if existing {
				_, err := f.conn.ExecContext(t.Context(), `INSERT INTO device_firmware_deployment
					(device_id, firmware_checksum, firmware_version, deployed_at)
					VALUES ($1, $2, '2.0.0', now() - INTERVAL '1 hour')`, f.deviceIDs["miner-0"], checksum1)
				require.NoError(t, err)
			}
			f.channel(t, pilotOf1, "miner-0")
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			// Success leaves a marker even for the first managed update. A
			// later completion must invalidate this exact observed marker.
			dispatcher.finish("miner-0", "SUCCESS")
			f.finishUpdate(t, "miner-0", "2.0.0")
			q := f.svc.store.GetQueries(t.Context())
			row, err := q.GetFirmwareRolloutWithChannel(t.Context(), sqlc.GetFirmwareRolloutWithChannelParams{RolloutID: started.ID, OrgID: f.orgID})
			require.NoError(t, err)
			stale, err := f.svc.listTargets(t.Context(), row.FirmwareRollout)
			require.NoError(t, err)
			require.True(t, stale[0].LastDispatchSucceeded)
			require.True(t, stale[0].LastDeployedAt.Valid)
			require.Empty(t, stale[0].LastDeployedFirmwareChecksum)
			foreign := createProvenanceCommand(t, f, map[string]string{"firmware_checksum": checksum1}, false)
			completeProvenanceCommand(t, f, foreign, sqlc.DeviceCommandStatusEnumSUCCESS)

			refreshed, err := f.svc.recordProvenance(t.Context(), row.FirmwareRollout, stale)
			require.NoError(t, err)
			require.Empty(t, refreshed[0].LastDeployedFirmwareChecksum, "a stale marker update must not overwrite command invalidation")
			require.False(t, refreshed[0].LastDispatchSucceeded)
			require.True(t, refreshed[0].LastDeployedAt.Valid, "the invalidation marker remains a durable CAS witness")
		})
	}
}

func TestFirmwareResultOrderingUsesConservativeCompletionEvidence(t *testing.T) {
	for _, tc := range []struct {
		name        string
		checksum    string
		fromOwnTime bool
		timeOffset  time.Duration
		mayAdopt    bool
	}{
		{"equal timestamps different artifact", checksum1, true, 0, false},
		{"later completion with backdated timestamp", checksum1, true, -time.Hour, false},
		{"later identical immutable artifact", checksum2, false, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 1)
			useAuditedModelDispatcher(t, f)
			f.channel(t, allAtOnce, "miner-0")
			started := f.apply(t, "fw-2")
			f.svc.EnforceTick(t.Context())
			own := latestProvenanceDispatch(t, f, started.ID)
			completeProvenanceCommand(t, f, own.LastDispatchedBatchUuid.String, sqlc.DeviceCommandStatusEnumSUCCESS)
			f.finishUpdate(t, "miner-0", "2.0.0")
			later := createProvenanceCommand(t, f, map[string]string{"firmware_checksum": tc.checksum}, false)
			completedAt := time.Now()
			if tc.fromOwnTime {
				require.NoError(t, f.conn.QueryRowContext(t.Context(), `SELECT result.updated_at
					FROM command_on_device_log result JOIN command_batch_log batch ON batch.id = result.command_batch_log_id
					WHERE batch.uuid = $1 AND result.device_id = $2`, own.LastDispatchedBatchUuid.String, f.deviceIDs["miner-0"]).Scan(&completedAt))
				completedAt = completedAt.Add(tc.timeOffset)
			}
			require.NoError(t, f.svc.store.GetQueries(t.Context()).UpsertCommandOnDeviceLog(t.Context(), sqlc.UpsertCommandOnDeviceLogParams{
				Uuid: later, DeviceID: f.deviceIDs["miner-0"], Status: sqlc.DeviceCommandStatusEnumSUCCESS, UpdatedAt: completedAt,
			}))
			require.Equal(t, tc.mayAdopt, latestProvenanceDispatch(t, f, started.ID).LastDispatchSucceeded)
			f.svc.EnforceTick(t.Context())
			require.Equal(t, tc.mayAdopt, phaseOf(f.rollout(t, started.ID), "miner-0") == PhaseDone)
		})
	}
}
