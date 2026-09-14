package sqlstores_test

import (
	"database/sql"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

// Discovery identity can change after an update. Membership keeps the existing
// paired device in the rollout, while dispatch eligibility follows the current
// reported pair. A new pairing must not inherit the old target's history.
func TestReleaseChannelQueries_ModelTransitionRetainsPairedTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("model-transition")
	device := f.device("renamed-miner", "Bitmain", "S19", "v1")
	require.NoError(t, f.q.InsertReleaseChannelMinerTargets(t.Context(), sqlc.InsertReleaseChannelMinerTargetsParams{
		ChannelID: channel, DeviceIdentifiers: []string{device.identifier},
	}))
	rollout := f.rollout(channel, "Bitmain", "S19")
	require.NoError(t, f.q.SnapshotFirmwareRolloutDevices(t.Context(), sqlc.SnapshotFirmwareRolloutDevicesParams{
		RolloutID: rollout, DeviceIds: []int64{device.id},
	}))
	readTarget := func() sqlc.ListFirmwareRolloutDevicesRow {
		t.Helper()
		rows, err := f.q.ListFirmwareRolloutDevices(t.Context(), rollout)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, device.id, rows[0].DeviceID)
		return rows[0]
	}
	initial := readTarget()
	require.True(t, initial.IsChannelMember)
	require.Equal(t, sql.NullBool{Bool: true, Valid: true}, initial.InScope)
	require.True(t, initial.BaselineAt.Valid)

	for _, pair := range []struct{ manufacturer, model string }{
		{"Bitmain", "Antminer S19"},
		{"Braiins", "S19"},
		{"", ""},
	} {
		f.exec(`UPDATE discovered_device SET manufacturer = $1, model = $2, firmware_version = 'v2'
			WHERE id = (SELECT discovered_device_id FROM device WHERE id = $3)`, pair.manufacturer, pair.model, device.id)
		renamed := readTarget()
		require.True(t, renamed.IsChannelMember, "observed identity does not remove an existing channel member")
		require.Equal(t, sql.NullBool{Bool: false, Valid: true}, renamed.InScope, "incompatible identity is ineligible for another dispatch")
		require.Equal(t, "v2", renamed.FirmwareVersion, "live update evidence remains available")
		require.Equal(t, initial.BaselineAt, renamed.BaselineAt)
		require.False(t, renamed.VerifiedAt.Valid, "a rename alone is not proof of convergence")
		require.False(t, renamed.ExcludedAt.Valid)
	}

	// Pair normalization still permits a compatible redispatch.
	f.exec(`UPDATE discovered_device SET manufacturer = ' bitMAIN ', model = 's19 '
		WHERE id = (SELECT discovered_device_id FROM device WHERE id = $1)`, device.id)
	require.True(t, readTarget().InScope.Bool)

	// Losing the channel's selector remains a genuine scope departure.
	f.exec(`DELETE FROM release_channel_target WHERE channel_id = $1`, channel)
	require.False(t, readTarget().IsChannelMember)
	require.NoError(t, f.q.InsertReleaseChannelMinerTargets(t.Context(), sqlc.InsertReleaseChannelMinerTargetsParams{
		ChannelID: channel, DeviceIdentifiers: []string{device.identifier},
	}))
	require.True(t, readTarget().IsChannelMember)

	device.softDelete()
	repaired := f.device(device.identifier, "Bitmain", "S19", "v2")
	require.NotEqual(t, device.id, repaired.id)
	old := readTarget()
	require.False(t, old.IsChannelMember, "matching identifiers cannot transfer a rollout to a new device row")
	require.Empty(t, old.FirmwareVersion, "a soft-deleted discovery row provides no current evidence")
}

func TestReleaseChannelQueries_DispatchEvidenceExcludesPreflightSkips(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("dispatch-evidence")
	sent := f.device("dispatched", "Bitmain", "S19", "v1")
	skipped := f.device("preflight-skipped", "Bitmain", "S19", "v1")
	rollout := f.rollout(channel, "Bitmain", "S19")
	ids := []int64{sent.id, skipped.id}
	require.NoError(t, f.q.AppendFirmwareRolloutDevices(t.Context(), sqlc.AppendFirmwareRolloutDevicesParams{
		RolloutID: rollout, DeviceIds: ids,
	}))
	readTargets := func() map[int64]sqlc.ListFirmwareRolloutDevicesRow {
		t.Helper()
		rows, err := f.q.ListFirmwareRolloutDevices(t.Context(), rollout)
		require.NoError(t, err)
		require.Len(t, rows, 2)
		byID := make(map[int64]sqlc.ListFirmwareRolloutDevicesRow, len(rows))
		for _, row := range rows {
			byID[row.DeviceID] = row
		}
		return byID
	}
	require.NoError(t, f.q.MarkFirmwareRolloutDevicesSent(t.Context(), sqlc.MarkFirmwareRolloutDevicesSentParams{
		RolloutID: rollout, DeviceIds: ids, DispatchedDeviceIds: []int64{sent.id}, BatchUuid: "00000000-0000-0000-0000-000000000011",
	}))
	first := readTargets()
	for _, row := range first {
		require.Equal(t, int32(1), row.Attempts)
		require.True(t, row.LastSentAt.Valid, "every attempt waits before another retry")
	}
	require.True(t, first[sent.id].LastDispatchedAt.Valid)
	require.Equal(t, sql.NullString{String: "00000000-0000-0000-0000-000000000011", Valid: true}, first[sent.id].LastDispatchedBatchUuid)
	require.Equal(t, first[sent.id].LastSentAt, first[sent.id].LastDispatchedAt)
	require.False(t, first[skipped.id].LastDispatchedAt.Valid, "a preflight skip does not authorize provenance")
	require.False(t, first[skipped.id].LastDispatchedBatchUuid.Valid)

	// A later preflight skip must not claim a newer dispatch than the one that
	// really occurred, including when another deployment has intervened.
	f.exec(`UPDATE firmware_rollout_device SET last_dispatched_at = last_dispatched_at - INTERVAL '1 hour'
		WHERE rollout_id = $1`, rollout)
	beforeRetry := readTargets()
	require.NoError(t, f.q.MarkFirmwareRolloutDevicesSent(t.Context(), sqlc.MarkFirmwareRolloutDevicesSentParams{
		RolloutID: rollout, DeviceIds: ids, BatchUuid: "00000000-0000-0000-0000-000000000012",
	}))
	afterRetry := readTargets()
	require.Equal(t, beforeRetry[sent.id].LastDispatchedAt, afterRetry[sent.id].LastDispatchedAt)
	require.Equal(t, beforeRetry[sent.id].LastDispatchedBatchUuid, afterRetry[sent.id].LastDispatchedBatchUuid)
	require.False(t, afterRetry[skipped.id].LastDispatchedAt.Valid)
	require.Equal(t, int32(2), afterRetry[skipped.id].Attempts)

	require.NoError(t, f.q.HaltFirmwareRolloutDevices(t.Context(), sqlc.HaltFirmwareRolloutDevicesParams{
		RolloutID: rollout, DeviceIds: ids, HaltReason: "failed",
	}))
	requeued, err := f.q.RequeueFirmwareRolloutDevices(t.Context(), sqlc.RequeueFirmwareRolloutDevicesParams{
		RolloutID: rollout, DeviceIds: ids,
	})
	require.NoError(t, err)
	require.ElementsMatch(t, ids, requeued)
	for _, row := range readTargets() {
		require.Zero(t, row.Attempts)
		require.False(t, row.LastSentAt.Valid)
		require.False(t, row.LastDispatchedAt.Valid, "a retry must establish new dispatch evidence")
		require.False(t, row.LastDispatchedBatchUuid.Valid)
	}
}

func TestReleaseChannelQueries_DispatchSuccessUsesExactDurableAudit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	channel := f.channel("durable-dispatch-success")
	device := f.device("dispatched", "Bitmain", "S19", "v2")
	other := f.device("another-device", "Bitmain", "S19", "v2")
	rollout := f.rollout(channel, "Bitmain", "S19")
	require.NoError(t, f.q.AppendFirmwareRolloutDevices(t.Context(), sqlc.AppendFirmwareRolloutDevicesParams{
		RolloutID: rollout, DeviceIds: []int64{device.id},
	}))
	const batchUUID = "00000000-0000-0000-0000-000000000013"
	require.NoError(t, f.q.MarkFirmwareRolloutDevicesSent(t.Context(), sqlc.MarkFirmwareRolloutDevicesSentParams{
		RolloutID: rollout, DeviceIds: []int64{device.id}, DispatchedDeviceIds: []int64{device.id}, BatchUuid: batchUUID,
	}))
	readSuccess := func() bool {
		t.Helper()
		rows, err := f.q.ListFirmwareRolloutDevices(t.Context(), rollout)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		return rows[0].LastDispatchSucceeded
	}
	require.False(t, readSuccess(), "dispatch timestamp alone is not command success")
	userID := f.scanID(`INSERT INTO "user" (user_id, username, password_hash)
		VALUES ('dispatch-evidence-user', 'dispatch-evidence-user', 'test') RETURNING id`)
	batchID := f.scanID(`INSERT INTO command_batch_log (uuid, type, created_by, status, devices_count, payload, organization_id)
		VALUES ($1, 'FirmwareUpdate', $2, 'PROCESSING', 1, '{"firmware_checksum":"sum"}', $3) RETURNING id`, batchUUID, userID, f.org)
	require.False(t, readSuccess(), "a queued command has no successful device result")
	f.exec(`INSERT INTO command_on_device_log (command_batch_log_id, device_id, status, org_id)
		VALUES ($1, $2, 'FAILED', $3)`, batchID, device.id, f.org)
	require.False(t, readSuccess(), "a failed install cannot authorize provenance from the existing reported version")
	f.exec(`UPDATE command_on_device_log SET status = 'SUCCESS' WHERE command_batch_log_id = $1`, batchID)
	require.True(t, readSuccess(), "durable success works without retaining queue rows")

	otherOrg := f.scanID(`INSERT INTO organization (org_id, name) VALUES ('other-dispatch-org', 'Other dispatch org') RETURNING id`)
	for _, mutation := range []struct {
		name    string
		change  func()
		restore func()
	}{
		{"batch UUID", func() {
			f.exec(`UPDATE command_batch_log SET uuid = '00000000-0000-0000-0000-000000000099' WHERE id = $1`, batchID)
		}, func() { f.exec(`UPDATE command_batch_log SET uuid = $1 WHERE id = $2`, batchUUID, batchID) }},
		{"command type", func() { f.exec(`UPDATE command_batch_log SET type = 'Reboot' WHERE id = $1`, batchID) }, func() { f.exec(`UPDATE command_batch_log SET type = 'FirmwareUpdate' WHERE id = $1`, batchID) }},
		{"artifact checksum", func() {
			f.exec(`UPDATE command_batch_log SET payload = '{"firmware_checksum":"other"}' WHERE id = $1`, batchID)
		}, func() {
			f.exec(`UPDATE command_batch_log SET payload = '{"firmware_checksum":"sum"}' WHERE id = $1`, batchID)
		}},
		{"batch org", func() { f.exec(`UPDATE command_batch_log SET organization_id = $1 WHERE id = $2`, otherOrg, batchID) }, func() { f.exec(`UPDATE command_batch_log SET organization_id = $1 WHERE id = $2`, f.org, batchID) }},
		{"result device", func() {
			f.exec(`UPDATE command_on_device_log SET device_id = $1 WHERE command_batch_log_id = $2`, other.id, batchID)
		}, func() {
			f.exec(`UPDATE command_on_device_log SET device_id = $1 WHERE command_batch_log_id = $2`, device.id, batchID)
		}},
		{"result org", func() {
			f.exec(`UPDATE command_on_device_log SET org_id = $1 WHERE command_batch_log_id = $2`, otherOrg, batchID)
		}, func() {
			f.exec(`UPDATE command_on_device_log SET org_id = $1 WHERE command_batch_log_id = $2`, f.org, batchID)
		}},
	} {
		mutation.change()
		require.False(t, readSuccess(), "unrelated %s cannot supply success evidence", mutation.name)
		mutation.restore()
		require.True(t, readSuccess())
	}
}
