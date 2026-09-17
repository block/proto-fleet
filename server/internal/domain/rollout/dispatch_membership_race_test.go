package rollout

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatchExcludesTargetRemovedAfterPreparation(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	f.channel(t, allAtOnce, "miner-0")
	started := f.apply(t, "fw-2")
	stale, targets := preparedDispatch(t, ctx, f, started.ID)
	require.True(t, targets[0].IsChannelMember)
	require.False(t, targets[0].excluded())

	// A channel edit commits after membership preparation, before dispatch
	// takes its channel lock and reloads the miner's live compatibility.
	_, err := f.svc.UpdateChannel(ctx, f.orgID, f.channelID, ChannelSpec{
		Name: "Test channel", Behavior: allAtOnce,
	})
	require.NoError(t, err)
	require.NoError(t, f.svc.enforceRollout(ctx, stale, "Test channel", targets))
	require.Empty(t, f.dispatcher.sentIdentifiers())
	departed := f.rollout(t, started.ID)
	assert.Equal(t, PhaseExcluded, phaseOf(departed, "miner-0"), "leaving channel scope is not a hardware compatibility failure")
	rows, err := f.svc.store.GetQueries(ctx).ListFirmwareRolloutDevices(ctx, started.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].ExcludedAt.Valid)
	assert.False(t, rows[0].HaltedAt.Valid)
	assert.Empty(t, rows[0].HaltReason)
	assert.Zero(t, rows[0].Attempts)
	var suppressed int
	require.NoError(t, f.conn.QueryRowContext(ctx, `SELECT count(*) FROM firmware_rollout_suppressed_device
		WHERE channel_id = $1 AND device_id = $2 AND assignment_generation = $3`,
		f.channelID, f.deviceIDs["miner-0"], started.AssignmentGeneration).Scan(&suppressed))
	assert.Zero(t, suppressed, "scope edits must not suppress a future return for this generation")

	// The member can return without a retry action or new assignment and
	// continue the same rollout with its original target history.
	_, err = f.svc.UpdateChannel(ctx, f.orgID, f.channelID, ChannelSpec{
		Name: "Test channel", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}}, Behavior: allAtOnce,
	})
	require.NoError(t, err)
	f.svc.EnforceTick(ctx)
	assert.Equal(t, []string{"miner-0"}, f.dispatcher.sentIdentifiers())
	current := f.latestRollout(t)
	assert.Equal(t, started.ID, current.ID)
	assert.Equal(t, PhaseInProgress, phaseOf(current, "miner-0"))
	assignment, err := f.svc.store.GetQueries(ctx).GetReleaseChannelFirmware(ctx, sqlc.GetReleaseChannelFirmwareParams{
		ChannelID: f.channelID, Manufacturer: "Proto", Model: "Rig",
	})
	require.NoError(t, err)
	assert.Equal(t, started.AssignmentGeneration, assignment.AssignmentGeneration)
}

func TestDispatchLeavesReturningTargetForConvergence(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	f.channel(t, allAtOnce, f.allMiners()...)
	started := f.apply(t, "fw-2")
	f.svc.EnforceTick(ctx)
	f.finishUpdate(t, "miner-0", "2.0.0")
	f.svc.EnforceTick(ctx)
	require.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))
	_, err := f.svc.UpdateChannel(ctx, f.orgID, f.channelID, ChannelSpec{
		Name: "Test channel", Scope: Scope{DeviceIdentifiers: []string{"miner-1"}}, Behavior: allAtOnce,
	})
	require.NoError(t, err)
	f.backdateSends(t)
	stale, err := f.svc.store.GetQueries(ctx).GetFirmwareRollout(ctx, sqlc.GetFirmwareRolloutParams{RolloutID: started.ID, OrgID: f.orgID})
	require.NoError(t, err)
	targets, err := f.svc.prepareRollout(ctx, stale)
	require.NoError(t, err)
	require.Equal(t, PhaseExcluded, phaseOf(f.rollout(t, started.ID), "miner-0"))

	// This already-converged target returns after preparation. Dispatch must
	// not reset its verification and send an unnecessary firmware command.
	_, err = f.svc.UpdateChannel(ctx, f.orgID, f.channelID, ChannelSpec{
		Name: "Test channel", Scope: Scope{DeviceIdentifiers: f.allMiners()}, Behavior: allAtOnce,
	})
	require.NoError(t, err)
	sendsBefore := len(f.dispatcher.sentIdentifiers())
	require.NoError(t, f.svc.enforceRollout(ctx, stale, "Test channel", targets))
	assert.Equal(t, []string{"miner-1"}, f.dispatcher.sentIdentifiers()[sendsBefore:])
	f.svc.EnforceTick(ctx)
	assert.Equal(t, PhaseDone, phaseOf(f.rollout(t, started.ID), "miner-0"))
	assert.Equal(t, []string{"miner-1"}, f.dispatcher.sentIdentifiers()[sendsBefore:], "normal preparation verifies the returner without a new dispatch")
}
