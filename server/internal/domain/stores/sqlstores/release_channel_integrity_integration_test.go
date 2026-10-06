package sqlstores_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseChannelQueries_RejectUnknownStateValues(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("integrity")
	rollout := f.rollout(channel, "Bitmain", "S19")
	device := f.device("integrity", "Bitmain", "S19", "v1")
	f.exec(`INSERT INTO firmware_rollout_device (rollout_id, device_id) VALUES ($1, $2)`, rollout, device.id)
	for _, test := range []struct {
		table, column, predicate, bad string
		id                            int64
	}{
		{"release_channel", "method", "id", "BATCHED", channel},
		{"release_channel", "order_by", "id", "randomly", channel},
		{"firmware_rollout", "status", "id", "ACTIVE", rollout},
		{"firmware_rollout", "stage", "id", "unknown_stage", rollout},
		{"firmware_rollout", "cancel_reason", "id", "cancelled", rollout},
		{"firmware_rollout", "started_by_type", "id", "admin", rollout},
		{"firmware_rollout", "last_action_by_type", "id", "API_KEY", rollout},
		{"firmware_rollout", "behavior_snapshot", "id", `null`, rollout},
		{"firmware_rollout", "behavior_snapshot", "id", `[]`, rollout},
		{"firmware_rollout", "behavior_snapshot", "id", `{}`, rollout},
		{"firmware_rollout", "behavior_snapshot", "id", `{"method":"all_at_once","order_by":null}`, rollout},
		{"firmware_rollout", "behavior_snapshot", "id", `{"method":"unknown","order_by":"random"}`, rollout},
		{"firmware_rollout", "behavior_snapshot", "id", `{"method":"all_at_once","order_by":"unknown"}`, rollout},
		{"firmware_rollout_device", "halt_reason", "rollout_id", "failure", rollout},
	} {
		t.Run(test.table+"/"+test.column, func(t *testing.T) {
			_, err := f.db.ExecContext(t.Context(), fmt.Sprintf(`UPDATE %s SET %s = $1 WHERE %s = $2`, test.table, test.column, test.predicate), test.bad, test.id)
			require.ErrorContains(t, err, "violates check constraint")
		})
	}
}

func TestReleaseChannelQueries_HistoryPreventsDeviceHardDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("history")
	rollout := f.rollout(channel, "Bitmain", "S19")
	device := f.device("history", "Bitmain", "S19", "v1")
	f.exec(`INSERT INTO firmware_rollout_device (rollout_id, device_id) VALUES ($1, $2)`, rollout, device.id)
	f.exec(`UPDATE firmware_rollout SET status = 'completed', finished_at = now() WHERE id = $1`, rollout)
	revision := f.revision(rollout)

	_, err := f.db.ExecContext(t.Context(), `DELETE FROM device WHERE id = $1`, device.id)
	require.ErrorContains(t, err, "violates foreign key constraint")
	require.Equal(t, int64(1), f.scanID(`SELECT count(*) FROM firmware_rollout_device WHERE rollout_id = $1`, rollout))
	require.Equal(t, revision, f.revision(rollout), "a rejected deletion changes neither history nor revision")

	// The normal removal path remains supported and retains the old device's
	// historical target even when the identifier is paired to a new row.
	device.softDelete()
	replacement := f.device("history", "Bitmain", "S19", "v2")
	require.NotEqual(t, device.id, replacement.id)
	require.Equal(t, int64(1), f.scanID(`SELECT count(*) FROM firmware_rollout_device WHERE rollout_id = $1 AND device_id = $2`, rollout, device.id))

	// Deleting the owning channel explicitly deletes its rollout history and
	// releases the historical reference, so the device can then be purged.
	f.exec(`DELETE FROM release_channel WHERE id = $1`, channel)
	f.exec(`DELETE FROM device WHERE id = $1`, device.id)
}

func TestReleaseChannelQueries_ChildDeletesAdvanceRevision(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("delete revision")
	rollout := f.rollout(channel, "Bitmain", "S19")
	device := f.device("delete revision", "Bitmain", "S19", "v1")
	f.exec(`INSERT INTO firmware_rollout_device (rollout_id, device_id) VALUES ($1, $2)`, rollout, device.id)
	f.exec(`INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version, rollout_id) VALUES ($1, 'sum', 'v2', $2)`, device.id, rollout)
	revision := f.revision(rollout)
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(t.Context(), `DELETE FROM firmware_rollout_device WHERE rollout_id = $1`, rollout)
	require.NoError(t, err)
	_, err = tx.ExecContext(t.Context(), `DELETE FROM device_firmware_deployment WHERE device_id = $1`, device.id)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.Equal(t, revision+1, f.revision(rollout), "both child deletions invalidate the header once in the transaction")
}

func TestReleaseChannelQueries_OrganizationDeleteIncludesHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("org deletion")
	rollout := f.rollout(channel, "Bitmain", "S19")
	device := f.device("org deletion", "Bitmain", "S19", "v1")
	f.exec(`INSERT INTO firmware_rollout_device (rollout_id, device_id) VALUES ($1, $2)`, rollout, device.id)
	// Existing organization FKs require explicit device/location cleanup. The
	// deferred history reference permits deleting devices first in the same
	// transaction that removes the organization and cascades rollout history.
	tx, err := f.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	for _, query := range []string{
		`DELETE FROM device WHERE org_id = $1`,
		`DELETE FROM discovered_device WHERE org_id = $1`,
		`DELETE FROM building WHERE org_id = $1`,
		`DELETE FROM site WHERE org_id = $1`,
		`DELETE FROM organization WHERE id = $1`,
	} {
		_, err = tx.ExecContext(t.Context(), query, f.org)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit())
	require.Equal(t, int64(0), f.scanID(`SELECT count(*) FROM firmware_rollout WHERE id = $1`, rollout))
	require.Equal(t, int64(0), f.scanID(`SELECT count(*) FROM firmware_rollout_device WHERE rollout_id = $1`, rollout))
}
