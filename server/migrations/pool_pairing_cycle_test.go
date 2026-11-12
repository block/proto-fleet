package migrations_test

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/testutil"
	"github.com/block/proto-fleet/server/migrations"
	"github.com/stretchr/testify/require"
)

func TestPoolPairingCycleUpgradePreservesExistingDevices(t *testing.T) {
	cfg, err := testutil.GetTestConfig()
	require.NoError(t, err)
	database := testutil.NewDatabaseService(t, cfg)
	user := database.CreateSuperAdminUser()
	runMigration := func(direction string) {
		t.Helper()
		script, err := migrations.Current.ReadFile("current/001076_internal_default_pool_lifecycle." + direction + ".sql")
		require.NoError(t, err)
		_, err = database.DB.ExecContext(t.Context(), string(script))
		require.NoError(t, err)
	}
	runMigration("down")
	q := sqlc.New(database.DB)
	devices := map[sqlc.PairingStatusEnum]int64{}
	for _, status := range []sqlc.PairingStatusEnum{sqlc.PairingStatusEnumPAIRED, sqlc.PairingStatusEnumDEFAULTPASSWORD, sqlc.PairingStatusEnumAUTHENTICATIONNEEDED, sqlc.PairingStatusEnumPENDING, sqlc.PairingStatusEnumFAILED, sqlc.PairingStatusEnumUNPAIRED} {
		device := database.CreateDevice(user.OrganizationID, "proto")
		devices[status] = device.DatabaseID
		_, err := q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device.DatabaseID, PairingStatus: status})
		require.NoError(t, err)
	}
	countEvents := func(device int64) int {
		var count int
		require.NoError(t, database.DB.QueryRowContext(t.Context(), `SELECT count(*) FROM pool_lifecycle_transition WHERE device_id=$1`, device).Scan(&count))
		return count
	}
	runMigration("up")
	for status, device := range devices {
		t.Run(string(status), func(t *testing.T) {
			require.Zero(t, countEvents(device), "upgrade must not queue writes")
			// Include existing devices whose credentials fail after upgrade.
			_, err := q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device, PairingStatus: sqlc.PairingStatusEnumAUTHENTICATIONNEEDED})
			require.NoError(t, err)
			_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device, PairingStatus: sqlc.PairingStatusEnumPAIRED})
			require.NoError(t, err)
			want := 0
			if status == sqlc.PairingStatusEnumUNPAIRED {
				want++
			}
			require.Equal(t, want, countEvents(device), "only explicitly unpaired legacy devices should adopt the default")
		})
	}

	// Marker and enqueue must roll back together; the next successful attempt
	// must still get its first-pairing policy.
	device := database.CreateDevice(user.OrganizationID, "proto")
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device.DatabaseID, PairingStatus: sqlc.PairingStatusEnumAUTHENTICATIONNEEDED})
	require.NoError(t, err)
	tx, err := database.DB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = sqlc.New(tx).UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device.DatabaseID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.Zero(t, countEvents(device.DatabaseID))
	_, err = q.UpsertDevicePairing(t.Context(), sqlc.UpsertDevicePairingParams{DeviceID: device.DatabaseID, PairingStatus: sqlc.PairingStatusEnumPAIRED})
	require.NoError(t, err)
	require.Equal(t, 1, countEvents(device.DatabaseID))
}
