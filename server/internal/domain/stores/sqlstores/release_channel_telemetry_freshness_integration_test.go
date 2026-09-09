package sqlstores_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReleaseChannelQueries_TelemetryFreshnessWithinTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("telemetry-freshness")
	require.NoError(t, f.q.InsertReleaseChannelTargets(t.Context(), sqlc.InsertReleaseChannelTargetsParams{
		ChannelID: channel, TargetTypes: []string{"site"}, TargetIds: []int64{f.site},
	}))
	stale := f.device("stale", "Bitmain", "S19", "v1")
	fresh := f.device("fresh", "Bitmain", "S19", "v1")
	f.status(stale, "ACTIVE")
	f.status(fresh, "ACTIVE")
	rollout := f.rollout(channel, "Bitmain", "S19")
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	// A sample on the transaction's inclusive cutoff becomes stale before
	// the next statement. This reproduces an aged transaction without a
	// fifteen-minute wait or depending on the application's clock.
	_, err = tx.ExecContext(t.Context(), `
		INSERT INTO device_metrics (time, device_identifier, hash_rate_hs, power_w, efficiency_jh, temp_c)
		VALUES (now() - INTERVAL '15 minutes', $1, 100, 200, 2, 60),
		       (statement_timestamp() - INTERVAL '1 minute', $2, 150, 300, 2, 65),
		       (statement_timestamp(), $2, 200, 600, 3, 70)`, stale.identifier, fresh.identifier)
	require.NoError(t, err)
	var transactionTime, beforeRead time.Time
	require.NoError(t, tx.QueryRowContext(t.Context(), `SELECT now(), statement_timestamp()`).Scan(&transactionTime, &beforeRead))
	require.True(t, beforeRead.After(transactionTime), "sample on the transaction cutoff is stale by the read statement")
	q := sqlc.New(tx)

	t.Run("ordering excludes stale efficiency", func(t *testing.T) {
		members, err := q.ListReleaseChannelMismatchedMembers(t.Context(), sqlc.ListReleaseChannelMismatchedMembersParams{
			OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19",
			FirmwareVersion: "v2", FirmwareChecksum: "sum", RolloutID: rollout, AssignmentGeneration: 1,
		})
		require.NoError(t, err)
		require.Len(t, members, 2)
		require.Equal(t, "fresh", members[0].DeviceIdentifier)
		assert.Equal(t, sql.NullFloat64{Float64: 3, Valid: true}, members[0].EfficiencyJh)
		require.Equal(t, "stale", members[1].DeviceIdentifier)
		assert.False(t, members[1].EfficiencyJh.Valid, "stale efficiency cannot determine rollout order")
	})

	require.NoError(t, q.SnapshotFirmwareRolloutDevices(t.Context(), sqlc.SnapshotFirmwareRolloutDevicesParams{
		RolloutID: rollout, DeviceIds: []int64{stale.id, fresh.id}, BatchIndex: sql.NullInt32{Int32: 0, Valid: true},
	}))
	devices, err := q.ListFirmwareRolloutDevices(t.Context(), rollout)
	require.NoError(t, err)
	require.Len(t, devices, 2)
	require.Equal(t, stale.id, devices[0].DeviceID)
	require.Equal(t, fresh.id, devices[1].DeviceID)

	t.Run("baseline excludes stale telemetry", func(t *testing.T) {
		assert.False(t, devices[0].BaselineHashRateHs.Valid)
		assert.False(t, devices[0].BaselinePowerW.Valid)
		assert.False(t, devices[0].BaselineEfficiencyJh.Valid)
		assert.False(t, devices[0].BaselineTempC.Valid)
		assert.Equal(t, sql.NullString{String: "ACTIVE", Valid: true}, devices[0].BaselineStatus)
		require.True(t, devices[0].BaselineAt.Valid)
		assert.True(t, devices[0].BaselineAt.Time.After(beforeRead))
		assert.Equal(t, sql.NullFloat64{Float64: 200, Valid: true}, devices[1].BaselineHashRateHs)
		assert.Equal(t, sql.NullFloat64{Float64: 600, Valid: true}, devices[1].BaselinePowerW)
		assert.Equal(t, sql.NullFloat64{Float64: 3, Valid: true}, devices[1].BaselineEfficiencyJh)
		assert.Equal(t, sql.NullFloat64{Float64: 70, Valid: true}, devices[1].BaselineTempC)
	})

	t.Run("current health excludes stale telemetry", func(t *testing.T) {
		assert.False(t, devices[0].HashRateHs.Valid)
		assert.False(t, devices[0].PowerW.Valid)
		assert.False(t, devices[0].EfficiencyJh.Valid)
		assert.False(t, devices[0].TempC.Valid)
		assert.Equal(t, "ACTIVE", devices[0].Status)
		assert.Equal(t, sql.NullFloat64{Float64: 200, Valid: true}, devices[1].HashRateHs)
		assert.Equal(t, sql.NullFloat64{Float64: 600, Valid: true}, devices[1].PowerW)
		assert.Equal(t, sql.NullFloat64{Float64: 3, Valid: true}, devices[1].EfficiencyJh)
		assert.Equal(t, sql.NullFloat64{Float64: 70, Valid: true}, devices[1].TempC)
	})
}
