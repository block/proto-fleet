package rollout

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelUpdatesAllowRetainedAndReducedRuntimeConflicts(t *testing.T) {
	f := newFixture(t, 6)
	ctx := t.Context()
	groupA := f.addGroup(t, "Group A")
	groupB := f.addGroup(t, "Group B")
	groupC := f.addGroup(t, "Group C")
	f.placeInSet(t, groupA, "group", "miner-0")
	f.placeInSet(t, groupA, "group", "miner-1")
	f.placeInSet(t, groupB, "group", "miner-2")
	f.placeInSet(t, groupC, "group", "miner-3")
	channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "A", Scope: Scope{GroupIDs: []int64{groupA}}})
	require.NoError(t, err)
	_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "B", Scope: Scope{GroupIDs: []int64{groupB}}})
	require.NoError(t, err)
	_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "C", Scope: Scope{GroupIDs: []int64{groupC}}})
	require.NoError(t, err)
	// Membership changes after save introduce three conflict relations for A:
	// miner-0 with B and C, and miner-1 with B.
	f.placeInSet(t, groupB, "group", "miner-0")
	f.placeInSet(t, groupB, "group", "miner-1")
	f.placeInSet(t, groupC, "group", "miner-0")
	channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
		Name: "Renamed", Description: "Updated during a runtime overlap",
		Scope: Scope{GroupIDs: []int64{groupA}}, Behavior: Behavior{Method: MethodBatched, BatchSize: 2},
	})
	require.NoError(t, err, "existing runtime conflicts must not block name or behavior edits")
	assert.Equal(t, "Renamed", channel.Name)
	assert.Equal(t, MethodBatched, channel.Behavior.Method)
	assert.Equal(t, int32(2), channel.Behavior.BatchSize)
	assert.Zero(t, channel.MinerCount, "equal-specificity ties still exclude both miners")

	// Replacing a group with its current miners and one unclaimed miner keeps
	// the same conflict relations. The more specific selectors can resolve ties.
	channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
		Name: "Specific", Scope: Scope{DeviceIdentifiers: []string{"miner-0", "miner-1", "miner-5"}},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(3), channel.MinerCount)
	conflicts, _, err := f.svc.ListMembershipConflicts(ctx, f.orgID, channel.ID, 100, "")
	require.NoError(t, err)
	require.Len(t, conflicts, 2, "each conflicted miner has one relation for this channel")
	for _, conflict := range conflicts {
		assert.Equal(t, ResolutionWinner, conflict.Resolution)
		assert.Equal(t, int32(1), conflict.Specificity)
	}

	// Dropping miner-1 removes an existing relation while retaining miner-0's
	// two relations. Fewer conflicts remain acceptable even when not all disappear.
	channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
		Name: "Reduced", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), channel.MinerCount)
	_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "New overlap", Scope: channel.Scope})
	require.True(t, fleeterror.IsFailedPreconditionError(err), "creates still reject every overlapping relation: %v", err)
	channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{Name: "Cleared"})
	require.NoError(t, err)
	assert.True(t, channel.Scope.IsEmpty())
}

func TestChannelUpdatesRejectReplacementRuntimeConflicts(t *testing.T) {
	for _, test := range []struct {
		name       string
		identifier string
		channel    string
	}{
		{"same channel different miner", "miner-1", "B"},
		{"different channel and miner", "miner-2", "C"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, 3)
			ctx := t.Context()
			groupA := f.addGroup(t, "Group A")
			groupB := f.addGroup(t, "Group B")
			groupC := f.addGroup(t, "Group C")
			f.placeInSet(t, groupA, "group", "miner-0")
			f.placeInSet(t, groupB, "group", "miner-1")
			f.placeInSet(t, groupC, "group", "miner-2")
			channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "A", Scope: Scope{GroupIDs: []int64{groupA}}})
			require.NoError(t, err)
			_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "B", Scope: Scope{GroupIDs: []int64{groupB}}})
			require.NoError(t, err)
			_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "C", Scope: Scope{GroupIDs: []int64{groupC}}})
			require.NoError(t, err)
			f.placeInSet(t, groupB, "group", "miner-0")
			channel, err = f.svc.GetChannel(ctx, f.orgID, channel.ID)
			require.NoError(t, err)

			// Both scopes have one conflict, but the proposed relation is new.
			_, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
				Name: "Rejected", Description: "Must roll back", Behavior: Behavior{Method: MethodBatched, BatchSize: 2},
				Scope: Scope{DeviceIdentifiers: []string{test.identifier}},
			})
			require.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
			require.ErrorContains(t, err, test.channel+" (1 miners)")
			unchanged, err := f.svc.GetChannel(ctx, f.orgID, channel.ID)
			require.NoError(t, err)
			assert.Equal(t, *channel, *unchanged, "rejected scope changes must preserve all channel fields and selectors")
		})
	}
}
