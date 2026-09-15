package migrations_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
)

func TestRolloutOfflineReservationsMigrationDownUpAndBackfill(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	f := newReleaseChannelFixture(t, db)
	downSQL, err := migrations.Migrations.ReadFile("000149_rollout_offline_reservations.down.sql")
	require.NoError(t, err)
	upSQL, err := migrations.Migrations.ReadFile("000149_rollout_offline_reservations.up.sql")
	require.NoError(t, err)
	f.exec(string(downSQL))
	var table sql.NullString
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT to_regclass('firmware_rollout_reservation')::text`).Scan(&table))
	require.False(t, table.Valid, "down removes the reservation table")

	channel := f.channel("reservation migration")
	a, b, unsent := f.device("a", 0), f.device("b", 0), f.device("unsent", 0)
	old := f.rollout(channel, "Bitmain", "S19")
	const batch = "00000000-0000-0000-0000-000000000001"
	f.exec(`INSERT INTO firmware_rollout_device (rollout_id, device_id, last_dispatched_at, last_dispatched_batch_uuid, excluded_at)
		VALUES ($1, $2, now(), $3, now())`, old, a.id, batch)
	f.exec(`UPDATE firmware_rollout SET status = 'canceled' WHERE id = $1`, old)
	current := f.rollout(channel, "Bitmain", "S19")
	// A retained command can appear in more than one rollout's target history.
	// It needs one reservation per channel/device/batch, including old targets
	// that were excluded or whose rollout was canceled.
	f.exec(`INSERT INTO firmware_rollout_device (rollout_id, device_id, last_dispatched_at, last_dispatched_batch_uuid)
		VALUES ($1, $2, now(), $4), ($1, $3, now(), $4)`, current, a.id, b.id, batch)
	f.exec(`INSERT INTO firmware_rollout_device (rollout_id, device_id) VALUES ($1, $2)`, current, unsent.id)
	oldRevision, currentRevision := f.revision(old), f.revision(current)

	for round := range 2 {
		f.exec(string(upSQL))
		require.Equal(t, int64(2), f.scanID(`SELECT count(*) FROM firmware_rollout_reservation`), "round %d: duplicate history is deduplicated and unsent targets reserve nothing", round)
		for _, device := range []fixtureDevice{a, b} {
			var observedOffline bool
			require.NoError(t, db.QueryRowContext(t.Context(), `SELECT observed_offline FROM firmware_rollout_reservation
				WHERE channel_id = $1 AND device_id = $2 AND batch_uuid = $3`, channel, device.id, batch).Scan(&observedOffline))
			require.False(t, observedOffline, "migration cannot infer a previous offline/online cycle")
		}
		require.Equal(t, oldRevision, f.revision(old), "backfill does not rewrite canceled rollout history")
		require.Equal(t, currentRevision, f.revision(current))
		if round == 0 {
			f.exec(`UPDATE firmware_rollout_reservation SET observed_offline = true`)
			f.exec(string(downSQL))
		}
	}
}
