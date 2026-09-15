package sqlstores_test

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/require"
)

func TestReleaseChannelQueries_ScopeReportsEveryConflictingChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	f := newReleaseChannelQueryFixture(t)
	ctx := t.Context()
	groupA, groupB := f.group("Group A"), f.group("Group B")
	rack := f.rack("Rack A", f.building)
	moving := f.device("moving", "Bitmain", "S19", "v1")
	unclaimed := f.device("unclaimed", "Bitmain", "S21", "v2")
	f.addToSet(groupA, "group", moving)
	f.addToSet(rack, "rack", moving)
	channelA, channelB := f.channel("Channel A"), f.channel("Channel B")
	require.NoError(t, f.q.InsertReleaseChannelTargets(ctx, sqlc.InsertReleaseChannelTargetsParams{
		ChannelID: channelA, TargetTypes: []string{"group", "rack"}, TargetIds: []int64{groupA, rack},
	}))
	require.NoError(t, f.q.InsertReleaseChannelTargets(ctx, sqlc.InsertReleaseChannelTargetsParams{
		ChannelID: channelB, TargetTypes: []string{"group"}, TargetIds: []int64{groupB},
	}))
	params := sqlc.ResolveReleaseChannelScopeParams{
		OrgID: f.org, DeviceIdentifiers: []string{moving.identifier, unclaimed.identifier},
		GroupIds: []int64{groupA}, RackIds: []int64{rack},
	}
	row := func(channelID int64, name string) sqlc.ResolveReleaseChannelScopeRow {
		return sqlc.ResolveReleaseChannelScopeRow{
			DeviceID: moving.id, DeviceIdentifier: moving.identifier, Manufacturer: "Bitmain", Model: "S19", FirmwareVersion: "v1",
			OwnerChannelID: channelID, OwnerChannelName: name,
		}
	}
	unclaimedRow := sqlc.ResolveReleaseChannelScopeRow{
		DeviceID: unclaimed.id, DeviceIdentifier: unclaimed.identifier, Manufacturer: "Bitmain", Model: "S21", FirmwareVersion: "v2",
	}
	rows, err := f.q.ResolveReleaseChannelScope(ctx, params)
	require.NoError(t, err)
	require.Equal(t, []sqlc.ResolveReleaseChannelScopeRow{row(channelA, "Channel A"), unclaimedRow}, rows,
		"multiple candidate selectors and multiple hits within one channel must not duplicate a miner/channel row")

	// Placement changes after the scopes are saved create a runtime tie:
	// both channels must appear, even though neither wins membership.
	f.addToSet(groupB, "group", moving)
	rows, err = f.q.ResolveReleaseChannelScope(ctx, params)
	require.NoError(t, err)
	require.Equal(t, []sqlc.ResolveReleaseChannelScopeRow{row(channelA, "Channel A"), row(channelB, "Channel B"), unclaimedRow}, rows)

	// A more-specific selector makes B win membership; the losing channel
	// still conflicts, and B's two selector hits still produce only one row.
	require.NoError(t, f.q.InsertReleaseChannelMinerTargets(ctx, sqlc.InsertReleaseChannelMinerTargetsParams{
		ChannelID: channelB, DeviceIdentifiers: []string{moving.identifier},
	}))
	rows, err = f.q.ResolveReleaseChannelScope(ctx, params)
	require.NoError(t, err)
	require.Equal(t, []sqlc.ResolveReleaseChannelScopeRow{row(channelA, "Channel A"), row(channelB, "Channel B"), unclaimedRow}, rows)

	// Editing a channel excludes that channel only, without hiding the other
	// affected scope or dropping an unclaimed miner from the preview.
	params.ExcludeChannelID = channelA
	rows, err = f.q.ResolveReleaseChannelScope(ctx, params)
	require.NoError(t, err)
	require.Equal(t, []sqlc.ResolveReleaseChannelScopeRow{row(channelB, "Channel B"), unclaimedRow}, rows)
	params.ExcludeChannelID = channelB
	rows, err = f.q.ResolveReleaseChannelScope(ctx, params)
	require.NoError(t, err)
	require.Equal(t, []sqlc.ResolveReleaseChannelScopeRow{row(channelA, "Channel A"), unclaimedRow}, rows)
}
