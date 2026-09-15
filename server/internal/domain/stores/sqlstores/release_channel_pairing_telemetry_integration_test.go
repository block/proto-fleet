package sqlstores_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestReleaseChannelQueries_TelemetryBelongsToCurrentPairing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	for _, sampleOffset := range []time.Duration{0, time.Microsecond} {
		t.Run(sampleOffset.String(), func(t *testing.T) {
			f := newReleaseChannelQueryFixture(t)
			channel := f.channel("pairing-telemetry")
			old := f.device("repaired-miner", "Bitmain", "S19", "v1")
			f.status(old, "ACTIVE")
			require.NoError(t, f.q.InsertReleaseChannelMinerTargets(t.Context(), sqlc.InsertReleaseChannelMinerTargetsParams{
				ChannelID: channel, DeviceIdentifiers: []string{old.identifier},
			}))
			var now time.Time
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT clock_timestamp()`).Scan(&now))
			f.exec(`UPDATE device SET created_at = $2 WHERE id = $1`, old.id, now.Add(-10*time.Minute))
			f.exec(`INSERT INTO device_metrics (time, device_identifier, hash_rate_hs, power_w, efficiency_jh, temp_c)
				VALUES ($1, $2, 100, 200, 2, 60)`, now.Add(-5*time.Minute), old.identifier)
			oldRollout := f.rollout(channel, "Bitmain", "S19")
			require.NoError(t, f.q.SnapshotFirmwareRolloutDevices(t.Context(), sqlc.SnapshotFirmwareRolloutDevicesParams{
				RolloutID: oldRollout, DeviceIds: []int64{old.id},
			}))
			readTarget := func(rollout, deviceID int64) sqlc.ListFirmwareRolloutDevicesRow {
				t.Helper()
				rows, err := f.q.ListFirmwareRolloutDevices(t.Context(), rollout)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, deviceID, rows[0].DeviceID)
				return rows[0]
			}
			oldBaseline := readTarget(oldRollout, old.id)
			oldMetrics := [4]sql.NullFloat64{{Float64: 100, Valid: true}, {Float64: 200, Valid: true}, {Float64: 2, Valid: true}, {Float64: 60, Valid: true}}
			require.Equal(t, oldMetrics, pairingBaselineTelemetry(oldBaseline))
			_, err := f.q.FinishFirmwareRollout(t.Context(), sqlc.FinishFirmwareRolloutParams{RolloutID: oldRollout, Status: "completed"})
			require.NoError(t, err)
			old.softDelete()
			repaired := f.device(old.identifier, "Bitmain", "S19", "v1")
			f.status(repaired, "ACTIVE")
			require.NotEqual(t, old.id, repaired.id)
			var pairedAt time.Time
			require.NoError(t, f.db.QueryRowContext(t.Context(), `SELECT created_at FROM device WHERE id = $1`, repaired.id).Scan(&pairedAt))
			require.True(t, pairedAt.After(now.Add(-5*time.Minute)))
			rollout := f.rollout(channel, "Bitmain", "S19")
			mismatchParams := sqlc.ListReleaseChannelMismatchedMembersParams{
				OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19",
				FirmwareVersion: "v2", FirmwareChecksum: "sum", AssignmentGeneration: 1,
			}
			readMismatched := func() sqlc.ListReleaseChannelMismatchedMembersRow {
				t.Helper()
				rows, err := f.q.ListReleaseChannelMismatchedMembers(t.Context(), mismatchParams)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, repaired.id, rows[0].DeviceID)
				return rows[0]
			}

			// The old generation's sample is still under fifteen minutes old,
			// but cannot influence the new pairing's order, baseline or health.
			assert.False(t, readMismatched().EfficiencyJh.Valid, "a previous pairing cannot determine rollout ordering")
			require.NoError(t, f.q.SnapshotFirmwareRolloutDevices(t.Context(), sqlc.SnapshotFirmwareRolloutDevicesParams{
				RolloutID: rollout, DeviceIds: []int64{repaired.id},
			}))
			beforeSample := readTarget(rollout, repaired.id)
			assert.Equal(t, [4]sql.NullFloat64{}, pairingBaselineTelemetry(beforeSample), "a new pairing cannot inherit an earlier baseline")
			assert.Equal(t, [4]sql.NullFloat64{}, pairingCurrentTelemetry(beforeSample), "a new pairing has no current telemetry until it reports")
			assert.Equal(t, "ACTIVE", beforeSample.Status, "status remains tied to the new device row")
			assert.Equal(t, [4]sql.NullFloat64{}, pairingCurrentTelemetry(readTarget(oldRollout, old.id)), "a deleted pairing has no current telemetry")

			// The creation boundary is inclusive. The live target now sees
			// its sample while its earlier, empty baseline remains immutable.
			f.exec(`INSERT INTO device_metrics (time, device_identifier, hash_rate_hs, power_w, efficiency_jh, temp_c)
				VALUES ($1, $2, 300, 900, 3, 70)`, pairedAt.Add(sampleOffset), repaired.identifier)
			newMetrics := [4]sql.NullFloat64{{Float64: 300, Valid: true}, {Float64: 900, Valid: true}, {Float64: 3, Valid: true}, {Float64: 70, Valid: true}}
			assert.Equal(t, newMetrics[2], readMismatched().EfficiencyJh)
			afterSample := readTarget(rollout, repaired.id)
			assert.Equal(t, newMetrics, pairingCurrentTelemetry(afterSample))
			assert.Equal(t, pairingBaselineTelemetry(beforeSample), pairingBaselineTelemetry(afterSample))
			assert.Equal(t, beforeSample.BaselineAt, afterSample.BaselineAt)
			oldHistory := readTarget(oldRollout, old.id)
			assert.Equal(t, [4]sql.NullFloat64{}, pairingCurrentTelemetry(oldHistory), "new pairing telemetry cannot leak into retained history")
			assert.Equal(t, oldMetrics, pairingBaselineTelemetry(oldHistory))
			assert.Equal(t, oldBaseline.BaselineAt, oldHistory.BaselineAt)
			assert.False(t, oldHistory.IsChannelMember)

			// A later rollout captures only the current pairing's sample.
			_, err = f.q.FinishFirmwareRollout(t.Context(), sqlc.FinishFirmwareRolloutParams{RolloutID: rollout, Status: "completed"})
			require.NoError(t, err)
			nextRollout := f.rollout(channel, "Bitmain", "S19")
			require.NoError(t, f.q.SnapshotFirmwareRolloutDevices(t.Context(), sqlc.SnapshotFirmwareRolloutDevicesParams{
				RolloutID: nextRollout, DeviceIds: []int64{repaired.id},
			}))
			assert.Equal(t, newMetrics, pairingBaselineTelemetry(readTarget(nextRollout, repaired.id)))
		})
	}
}

func TestReleaseChannelQueries_PairingTelemetryStillRequiresFreshness(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("pairing-telemetry-freshness")
	device := f.device("stale-current-pairing", "Bitmain", "S19", "v1")
	f.exec(`UPDATE device SET created_at = clock_timestamp() - INTERVAL '20 minutes' WHERE id = $1`, device.id)
	f.exec(`INSERT INTO device_metrics (time, device_identifier, hash_rate_hs, power_w, efficiency_jh, temp_c)
		VALUES (clock_timestamp() - INTERVAL '16 minutes', $1, 100, 200, 2, 60)`, device.identifier)
	require.NoError(t, f.q.InsertReleaseChannelMinerTargets(t.Context(), sqlc.InsertReleaseChannelMinerTargetsParams{
		ChannelID: channel, DeviceIdentifiers: []string{device.identifier},
	}))
	members, err := f.q.ListReleaseChannelMismatchedMembers(t.Context(), sqlc.ListReleaseChannelMismatchedMembersParams{
		OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19",
		FirmwareVersion: "v2", FirmwareChecksum: "sum", AssignmentGeneration: 1,
	})
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.False(t, members[0].EfficiencyJh.Valid, "a current-pairing sample can still be too old")
	rollout := f.rollout(channel, "Bitmain", "S19")
	require.NoError(t, f.q.SnapshotFirmwareRolloutDevices(t.Context(), sqlc.SnapshotFirmwareRolloutDevicesParams{
		RolloutID: rollout, DeviceIds: []int64{device.id},
	}))
	targets, err := f.q.ListFirmwareRolloutDevices(t.Context(), rollout)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, [4]sql.NullFloat64{}, pairingBaselineTelemetry(targets[0]))
	assert.Equal(t, [4]sql.NullFloat64{}, pairingCurrentTelemetry(targets[0]))
}

func pairingCurrentTelemetry(target sqlc.ListFirmwareRolloutDevicesRow) [4]sql.NullFloat64 {
	return [4]sql.NullFloat64{target.HashRateHs, target.PowerW, target.EfficiencyJh, target.TempC}
}

func pairingBaselineTelemetry(target sqlc.ListFirmwareRolloutDevicesRow) [4]sql.NullFloat64 {
	return [4]sql.NullFloat64{target.BaselineHashRateHs, target.BaselinePowerW, target.BaselineEfficiencyJh, target.BaselineTempC}
}
