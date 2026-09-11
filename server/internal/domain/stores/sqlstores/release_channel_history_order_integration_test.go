package sqlstores_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/dbtypes"
)

func TestReleaseChannelQueries_RetryHistoryFollowsCreationOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	ctx := t.Context()
	channel := f.channel("history-order")
	device := f.device("history-device", "Bitmain", "S19", "v1")
	require.NoError(t, f.q.InsertReleaseChannelMinerTargets(ctx, sqlc.InsertReleaseChannelMinerTargetsParams{
		ChannelID: channel, DeviceIdentifiers: []string{device.identifier},
	}))
	_, err := f.q.UpsertReleaseChannelFirmware(ctx, sqlc.UpsertReleaseChannelFirmwareParams{
		ChannelID: channel, Manufacturer: "Bitmain", Model: "S19", FirmwareChecksum: "sum", FirmwareVersion: "v2", AssignedBy: 1,
	})
	require.NoError(t, err)

	// A caller can begin a transaction before another caller creates and
	// finishes a rollout, then acquire the assignment lock and start its retry.
	tx, err := f.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	var txStart time.Time
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT now()`).Scan(&txStart))
	old := f.rollout(channel, "Bitmain", "S19")
	require.NoError(t, f.q.AppendFirmwareRolloutDevices(ctx, sqlc.AppendFirmwareRolloutDevicesParams{
		RolloutID: old, DeviceIds: []int64{device.id},
	}))
	require.NoError(t, f.q.HaltFirmwareRolloutDevices(ctx, sqlc.HaltFirmwareRolloutDevicesParams{
		RolloutID: old, DeviceIds: []int64{device.id}, HaltReason: "failed",
	}))
	_, err = f.q.FinishFirmwareRollout(ctx, sqlc.FinishFirmwareRolloutParams{RolloutID: old, Status: "completed_with_failures"})
	require.NoError(t, err)
	suppressionParams := sqlc.ListReleaseChannelSuppressedMembersParams{
		OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19", AssignmentGeneration: 1,
	}
	suppressed, err := f.q.ListReleaseChannelSuppressedMembers(ctx, suppressionParams)
	require.NoError(t, err)
	require.Len(t, suppressed, 1, "the failed rollout initially suppresses the miner")

	q := sqlc.New(tx)
	require.NoError(t, q.LockReleaseChannelScopes(ctx, f.org))
	retry, err := q.CreateFirmwareRollout(ctx, sqlc.CreateFirmwareRolloutParams{
		OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19", FirmwareChecksum: "sum", FirmwareVersion: "v2", AssignmentGeneration: 1,
		Stage: "rest", ActorType: "system", BehaviorSnapshot: dbtypes.RolloutBehaviorSnapshot{Method: "all_at_once", OrderBy: "least_efficient_first"},
	})
	require.NoError(t, err)
	require.NoError(t, q.AppendFirmwareRolloutDevices(ctx, sqlc.AppendFirmwareRolloutDevicesParams{
		RolloutID: retry.ID, DeviceIds: []int64{device.id},
	}))
	require.NoError(t, tx.Commit())
	assert.True(t, retry.CreatedAt.After(txStart), "history uses creation time rather than transaction start")
	latest, err := f.q.GetLatestFirmwareRolloutForPair(ctx, sqlc.GetLatestFirmwareRolloutForPairParams{
		ChannelID: channel, Manufacturer: "Bitmain", Model: "S19", AssignmentGeneration: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, retry.ID, latest.ID)
	suppressed, err = f.q.ListReleaseChannelSuppressedMembers(ctx, suppressionParams)
	require.NoError(t, err)
	assert.Empty(t, suppressed, "the newer unhalted target releases the prior suppression")

	f.exec(`UPDATE discovered_device SET firmware_version = 'v2' WHERE device_identifier = $1`, device.identifier)
	f.exec(`INSERT INTO device_firmware_deployment (device_id, firmware_checksum, firmware_version, rollout_id)
		VALUES ($1, 'sum', 'v2', $2)`, device.id, retry.ID)
	require.NoError(t, f.q.MarkFirmwareRolloutDevicesVerified(ctx, sqlc.MarkFirmwareRolloutDevicesVerifiedParams{
		RolloutID: retry.ID, DeviceIds: []int64{device.id},
	}))
	_, err = f.q.FinishFirmwareRollout(ctx, sqlc.FinishFirmwareRolloutParams{RolloutID: retry.ID, Status: "completed"})
	require.NoError(t, err)
	mismatchParams := sqlc.ListReleaseChannelMismatchedMembersParams{
		OrgID: f.org, ChannelID: channel, Manufacturer: "Bitmain", Model: "S19", AssignmentGeneration: 1, FirmwareChecksum: "sum", FirmwareVersion: "v2",
	}
	mismatched, err := f.q.ListReleaseChannelMismatchedMembers(ctx, mismatchParams)
	require.NoError(t, err)
	require.Empty(t, mismatched, "the retry first converges to the assigned version and checksum")
	// The miner reports v1 again after the successful retry. The older halt
	// must not prevent either drift query from scheduling its next rollout.
	f.exec(`UPDATE discovered_device SET firmware_version = 'v1' WHERE device_identifier = $1`, device.identifier)
	mismatched, err = f.q.ListReleaseChannelMismatchedMembers(ctx, mismatchParams)
	require.NoError(t, err)
	if assert.Len(t, mismatched, 1) {
		assert.Equal(t, device.id, mismatched[0].DeviceID)
	}
	needingRollout, err := f.q.ListReleaseChannelFirmwareNeedingRollout(ctx)
	require.NoError(t, err)
	if assert.Len(t, needingRollout, 1) {
		assert.Equal(t, channel, needingRollout[0].ChannelID)
	}
}
