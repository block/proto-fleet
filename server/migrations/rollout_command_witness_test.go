package migrations_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

func TestRolloutCommandWitnessMigrationPreservesExistingProvenance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	f := newReleaseChannelFixture(t, db)
	downSQL, err := migrations.Migrations.ReadFile("000149_rollout_enforcement_state.down.sql")
	require.NoError(t, err)
	upSQL, err := migrations.Migrations.ReadFile("000149_rollout_enforcement_state.up.sql")
	require.NoError(t, err)
	f.exec(string(downSQL))
	channel := f.channel("command witness migration")
	device := f.device("witness-device", 0)
	rollout := f.rollout(channel, "Bitmain", "S19")
	f.exec(`INSERT INTO device_firmware_deployment
		(device_id, firmware_checksum, firmware_version, rollout_id)
		VALUES ($1, 'known-artifact', '1.0.0', $2)`, device.id, rollout)
	var before time.Time
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT deployed_at
		FROM device_firmware_deployment WHERE device_id = $1`, device.id).Scan(&before))
	revision := f.revision(rollout)
	for range 2 {
		var columns int
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'device_firmware_deployment'
			AND column_name = 'last_command_batch_uuid'`).Scan(&columns))
		require.Zero(t, columns, "downgrade removes only the new command witness")
		f.exec(string(upSQL))
		var checksum, version string
		var owner int64
		var after time.Time
		var witness sql.NullString
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT firmware_checksum, firmware_version,
			rollout_id, deployed_at, last_command_batch_uuid FROM device_firmware_deployment
			WHERE device_id = $1`, device.id).Scan(&checksum, &version, &owner, &after, &witness))
		require.Equal(t, "known-artifact", checksum)
		require.Equal(t, "1.0.0", version)
		require.Equal(t, rollout, owner)
		require.Equal(t, before, after)
		require.False(t, witness.Valid, "an upgrade cannot infer command completion order from old timestamps")
		require.Equal(t, revision, f.revision(rollout), "adding the witness does not rewrite rollout history")
		f.exec(string(downSQL))
	}
	f.exec(string(upSQL))
}
