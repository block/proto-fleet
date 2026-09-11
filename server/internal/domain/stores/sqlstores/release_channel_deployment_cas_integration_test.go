package sqlstores_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestReleaseChannelQueries_RecordFirmwareDeploymentCASMixedBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	olderRollout := f.rollout(f.channel("older"), "Bitmain", "S19")
	newerRollout := f.rollout(f.channel("newer"), "Bitmain", "S19")
	require.Less(t, olderRollout, newerRollout)
	stalePresent := f.device("stale-present", "Bitmain", "S19", "v2")
	staleAbsent := f.device("stale-absent", "Bitmain", "S19", "v2")
	freshPresent := f.device("fresh-present", "Bitmain", "S19", "v2")
	freshAbsent := f.device("fresh-absent", "Bitmain", "S19", "v2")
	deletedPresent := f.device("deleted-present", "Bitmain", "S19", "v2")

	require.NoError(t, f.q.AppendFirmwareRolloutDevices(t.Context(), sqlc.AppendFirmwareRolloutDevicesParams{
		RolloutID: olderRollout,
		DeviceIds: []int64{stalePresent.id, staleAbsent.id, freshPresent.id, freshAbsent.id, deletedPresent.id},
	}))
	absentObservation := readFirmwareDeploymentObservation(t, f.db, staleAbsent.id)
	newObservation := readFirmwareDeploymentObservation(t, f.db, freshAbsent.id)
	require.NoError(t, f.q.RecordFirmwareDeployment(t.Context(), firmwareDeploymentCASParams("initial", "v1", newerRollout,
		readFirmwareDeploymentObservation(t, f.db, stalePresent.id),
		readFirmwareDeploymentObservation(t, f.db, freshPresent.id),
		readFirmwareDeploymentObservation(t, f.db, deletedPresent.id),
	)))
	staleObservation := readFirmwareDeploymentObservation(t, f.db, stalePresent.id)
	freshObservation := readFirmwareDeploymentObservation(t, f.db, freshPresent.id)
	deletedObservation := readFirmwareDeploymentObservation(t, f.db, deletedPresent.id)

	// Another writer commits between the target read and provenance recording:
	// one observed row changes, and another appears after observed absence.
	require.NoError(t, f.q.RecordFirmwareDeployment(t.Context(), firmwareDeploymentCASParams("intervening", "v2", newerRollout,
		staleObservation, absentObservation,
	)))
	interveningPresent := readFirmwareDeploymentObservation(t, f.db, stalePresent.id)
	interveningAbsent := readFirmwareDeploymentObservation(t, f.db, staleAbsent.id)
	f.exec(`DELETE FROM device_firmware_deployment WHERE device_id = $1`, deletedPresent.id)

	// One batch may contain fresh and stale observations. Repeated identical
	// observations are harmless, and a lower rollout ID can record a later send.
	require.NoError(t, f.q.RecordFirmwareDeployment(t.Context(), firmwareDeploymentCASParams("corrective", "v3", olderRollout,
		staleObservation, absentObservation, freshObservation, freshObservation,
		newObservation, newObservation, deletedObservation,
	)))
	require.Equal(t, interveningPresent, readFirmwareDeploymentObservation(t, f.db, stalePresent.id))
	require.Equal(t, interveningAbsent, readFirmwareDeploymentObservation(t, f.db, staleAbsent.id))
	require.False(t, readFirmwareDeploymentObservation(t, f.db, deletedPresent.id).present, "an observed row removed afterward must not be resurrected")

	for _, deviceID := range []int64{freshPresent.id, freshAbsent.id} {
		current := readFirmwareDeploymentObservation(t, f.db, deviceID)
		require.True(t, current.present)
		require.Equal(t, "corrective", current.checksum)
		require.Equal(t, "v3", current.version)
		require.Equal(t, sql.NullInt64{Int64: olderRollout, Valid: true}, current.rolloutID)
		require.True(t, current.deployedAt.After(freshObservation.deployedAt))
	}

	// Losing observations must leave timestamps and rollout revisions untouched.
	olderRevision, newerRevision := f.revision(olderRollout), f.revision(newerRollout)
	require.NoError(t, f.q.RecordFirmwareDeployment(t.Context(), firmwareDeploymentCASParams("stale", "v0", olderRollout,
		staleObservation, absentObservation, deletedObservation,
	)))
	require.Equal(t, olderRevision, f.revision(olderRollout))
	require.Equal(t, newerRevision, f.revision(newerRollout))

	rows, err := f.q.ListFirmwareRolloutDevices(t.Context(), olderRollout)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	for _, row := range rows {
		observed := readFirmwareDeploymentObservation(t, f.db, row.DeviceID)
		require.Equal(t, observed.present, row.LastDeployedAt.Valid)
		require.Equal(t, observed.checksum, row.LastDeployedFirmwareChecksum)
		if observed.present {
			require.True(t, observed.deployedAt.Equal(row.LastDeployedAt.Time))
		}
	}
}

func TestReleaseChannelQueries_RecordFirmwareDeploymentCASMonotonicTimestamp(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	rollout := f.rollout(f.channel("monotonic"), "Bitmain", "S19")
	device := f.device("monotonic", "Bitmain", "S19", "v1")
	// Put the stored timestamp ahead of the clock to exercise the minimum
	// increment deterministically, without relying on sleeps or clock precision.
	f.exec(`INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version, rollout_id, deployed_at)
		VALUES ($1, 'a', 'v1', $2, clock_timestamp() + INTERVAL '1 hour')`, device.id, rollout)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	q := sqlc.New(tx)
	initial := readFirmwareDeploymentObservation(t, tx, device.id)
	require.NoError(t, q.RecordFirmwareDeployment(t.Context(), firmwareDeploymentCASParams("b", "v2", rollout, initial)))
	second := readFirmwareDeploymentObservation(t, tx, device.id)
	require.True(t, initial.deployedAt.Add(time.Microsecond).Equal(second.deployedAt))
	require.Equal(t, "b", second.checksum)

	// Return to the original checksum in the same transaction. Its timestamp
	// must still change so the earlier observation cannot pass an ABA comparison.
	require.NoError(t, q.RecordFirmwareDeployment(t.Context(), firmwareDeploymentCASParams("a", "v1", rollout, second)))
	third := readFirmwareDeploymentObservation(t, tx, device.id)
	require.True(t, second.deployedAt.Add(time.Microsecond).Equal(third.deployedAt))
	require.NoError(t, q.RecordFirmwareDeployment(t.Context(), firmwareDeploymentCASParams("stale", "v0", rollout, initial)))
	require.Equal(t, third, readFirmwareDeploymentObservation(t, tx, device.id))

	// Matching time alone is insufficient if the observed checksum differs.
	wrongChecksum := third
	wrongChecksum.checksum = "unobserved"
	require.NoError(t, q.RecordFirmwareDeployment(t.Context(), firmwareDeploymentCASParams("stale", "v0", rollout, wrongChecksum)))
	require.Equal(t, third, readFirmwareDeploymentObservation(t, tx, device.id))
	require.NoError(t, tx.Commit())
}

type firmwareDeploymentObservation struct {
	deviceID   int64
	present    bool
	deployedAt time.Time
	checksum   string
	version    string
	rolloutID  sql.NullInt64
}

func readFirmwareDeploymentObservation(t *testing.T, db sqlc.DBTX, deviceID int64) firmwareDeploymentObservation {
	t.Helper()
	observed := firmwareDeploymentObservation{deviceID: deviceID}
	err := db.QueryRowContext(t.Context(), `SELECT deployed_at, firmware_checksum, firmware_version, rollout_id
		FROM device_firmware_deployment WHERE device_id = $1`, deviceID).Scan(
		&observed.deployedAt, &observed.checksum, &observed.version, &observed.rolloutID,
	)
	if err == sql.ErrNoRows {
		return observed
	}
	require.NoError(t, err)
	observed.present = true
	return observed
}

func firmwareDeploymentCASParams(checksum, version string, rolloutID int64, observations ...firmwareDeploymentObservation) sqlc.RecordFirmwareDeploymentParams {
	params := sqlc.RecordFirmwareDeploymentParams{
		FirmwareChecksum: checksum,
		FirmwareVersion:  version,
		RolloutID:        sql.NullInt64{Int64: rolloutID, Valid: true},
	}
	for _, observed := range observations {
		params.DeviceIds = append(params.DeviceIds, observed.deviceID)
		params.ExpectedDeploymentPresent = append(params.ExpectedDeploymentPresent, observed.present)
		params.ExpectedDeployedAts = append(params.ExpectedDeployedAts, observed.deployedAt)
		params.ExpectedFirmwareChecksums = append(params.ExpectedFirmwareChecksums, observed.checksum)
	}
	return params
}
