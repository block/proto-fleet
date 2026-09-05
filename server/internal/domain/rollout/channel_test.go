package rollout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopesResolvePlacementAndRejectOverlap(t *testing.T) {
	f := newFixture(t, 5)
	ctx := t.Context()
	f.addMiner(t, "other-0", "Other")
	site := f.addSite(t, "Site A")
	building := f.addBuilding(t, site, "Building 1")
	rack := f.addRack(t, site, building, "Rack 1")
	group := f.addGroup(t, "Group X")
	f.placeInSet(t, rack, "rack", "miner-0")
	f.placeInSet(t, rack, "rack", "miner-1")
	f.placeInSet(t, group, "group", "miner-1")
	f.placeInSet(t, group, "group", "miner-2")
	f.placeAtSite(t, site, "miner-3")
	f.placeAtSite(t, site, "other-0")

	// Preview: a rack scope covers its two miners; a building scope covers
	// the rack's miners through rack placement; a site scope covers
	// devices placed at the site directly.
	preview, err := f.svc.PreviewScope(ctx, f.orgID, Scope{RackIDs: []int64{rack}}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(2), preview.MinerCount)
	assert.Equal(t, []ModelCount{{Model: "Rig", MinerCount: 2}}, preview.Models)
	preview, err = f.svc.PreviewScope(ctx, f.orgID, Scope{BuildingIDs: []int64{building}}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(2), preview.MinerCount)
	preview, err = f.svc.PreviewScope(ctx, f.orgID, Scope{SiteIDs: []int64{site}}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(2), preview.MinerCount)
	assert.Equal(t, []ModelCount{{Model: "Other", MinerCount: 1}, {Model: "Rig", MinerCount: 1}}, preview.Models)

	rackChannel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Rack channel", Scope: Scope{RackIDs: []int64{rack}}})
	require.NoError(t, err)
	assert.Equal(t, int32(2), rackChannel.MinerCount)
	assert.Equal(t, []int64{rack}, rackChannel.Scope.RackIDs)

	// The group shares miner-1 with the rack: rejected, naming the channel.
	_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Group channel", Scope: Scope{GroupIDs: []int64{group}}})
	require.ErrorContains(t, err, "overlaps release channel Rack channel (1 miners)")
	preview, err = f.svc.PreviewScope(ctx, f.orgID, Scope{GroupIDs: []int64{group}}, 0)
	require.NoError(t, err)
	assert.Equal(t, []ScopeConflict{{ChannelID: rackChannel.ID, ChannelName: "Rack channel", MinerCount: 1}}, preview.Conflicts)

	// Editing the rack channel itself does not conflict with itself.
	_, err = f.svc.UpdateChannel(ctx, f.orgID, rackChannel.ID, ChannelSpec{
		Name: "Rack channel", Scope: Scope{RackIDs: []int64{rack}, DeviceIdentifiers: []string{"miner-4"}},
	})
	require.NoError(t, err)
	_, err = f.svc.UpdateChannel(ctx, f.orgID, rackChannel.ID, ChannelSpec{
		Name: "Rack channel", Scope: Scope{DeviceIdentifiers: []string{"ghost-9"}},
	})
	assert.ErrorContains(t, err, `unknown miner "ghost-9"`)
	edited, err := f.svc.GetChannel(ctx, f.orgID, rackChannel.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"miner-4"}, edited.Scope.DeviceIdentifiers, "identifiers round-trip through storage by id")

	// The site channel covers the miners placed at the site directly
	// (miner-3 and other-0); no overlap with the rack channel yet.
	siteChannel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Site channel", Scope: Scope{SiteIDs: []int64{site}}})
	require.NoError(t, err)
	assert.Equal(t, int32(2), siteChannel.MinerCount)

	// Membership is dynamic: a miner moved into the rack joins the rack
	// channel without anyone editing it. Because miner-3 also sits at the
	// site, this is a runtime overlap: the more specific rack selector wins
	// and the miner is flagged.
	f.placeInSet(t, rack, "rack", "miner-3")
	channels, err := f.svc.ListChannels(ctx, f.orgID)
	require.NoError(t, err)
	byName := map[string]Channel{}
	for _, c := range channels {
		byName[c.Name] = c
	}
	assert.Equal(t, int32(4), byName["Rack channel"].MinerCount, "rack beats site")
	assert.Equal(t, int32(1), byName["Site channel"].MinerCount, "only other-0 remains at the site")
	require.Len(t, byName["Rack channel"].ModelGroups, 1)
	rig := byName["Rack channel"].ModelGroups[0]
	assert.Equal(t, "Rig", rig.Model)
	assert.Equal(t, int32(4), rig.MinerCount)
	assert.Equal(t, int32(0), rig.OnTargetCount, "nothing assigned yet")
	assert.Equal(t, []string{"1.0.0"}, rig.ReportedVersions)

	// Members are listed separately, by identifier, in pages; the model
	// filter narrows them and the conflict flag rides along.
	page, cursor, err := f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID, "", 3, "")
	require.NoError(t, err)
	require.Len(t, page, 3)
	require.NotEmpty(t, cursor)
	assert.Equal(t, []string{"miner-0", "miner-1", "miner-3"}, []string{page[0].DeviceIdentifier, page[1].DeviceIdentifier, page[2].DeviceIdentifier})
	assert.True(t, page[2].Conflicted, "miner-3 is also at the site")
	assert.False(t, page[0].Conflicted)
	rest, cursor, err := f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID, "", 3, cursor)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	assert.Equal(t, "miner-4", rest[0].DeviceIdentifier)
	assert.Empty(t, cursor)
	none, _, err := f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID, "Other", 0, "")
	require.NoError(t, err)
	assert.Empty(t, none)
	_, _, err = f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID, "", 0, "garbage")
	assert.ErrorContains(t, err, "invalid cursor")
	_, _, err = f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID+1000, "", 0, "")
	assert.ErrorContains(t, err, "not found")

	// A miner that leaves the rack leaves the channel.
	f.removeFromSet(t, rack, "miner-0")
	ch, err := f.svc.GetChannel(ctx, f.orgID, rackChannel.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(3), ch.MinerCount)

	_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Rack channel", Scope: Scope{}})
	assert.ErrorContains(t, err, "already exists")
	require.NoError(t, f.svc.DeleteChannel(ctx, f.orgID, siteChannel.ID))
	assert.ErrorContains(t, f.svc.DeleteChannel(ctx, f.orgID, siteChannel.ID), "not found")
}

func TestBehaviorValidation(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	create := func(b Behavior) error {
		_, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "v", Behavior: b})
		return err
	}
	assert.ErrorContains(t, create(Behavior{Method: MethodPilotThenContinue}), "pilot batch size")
	assert.ErrorContains(t, create(Behavior{Method: MethodBatched}), "batch size")
	assert.ErrorContains(t, create(Behavior{Method: "canary", BatchSize: 1}), "unknown rollout method")
	assert.ErrorContains(t, create(Behavior{Method: MethodBatched, BatchSize: 1, Order: "alphabetical"}), "unknown rollout order")
	assert.ErrorContains(t, create(Behavior{Method: MethodPilotThenContinue, PilotSize: 1, Thresholds: Thresholds{MaxHashrateDropPercent: ptr(150.0)}}), "between 0 and 100")
	assert.ErrorContains(t, create(Behavior{Method: MethodAllAtOnce, MaxConcurrentOffline: -1}), "must not be negative")

	_, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "  "})
	assert.ErrorContains(t, err, "name is required")

	// Irrelevant knobs are normalized away per method.
	ch, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "normalized", Behavior: Behavior{
		Method: MethodPilotThenContinue, PilotSize: 10, BatchSize: 3, WaitBetweenBatchesSeconds: 30,
		AutoContinue: true, StabilizationSeconds: 15, Thresholds: Thresholds{MaxNewErrors: ptr(int32(2))},
	}})
	require.NoError(t, err)
	assert.Equal(t, Behavior{
		Method: MethodPilotThenContinue, Order: OrderLeastEfficientFirst, PilotSize: 10, ReviewAfterEachBatch: true,
		AutoContinue: true, StabilizationSeconds: 15, Thresholds: Thresholds{MaxNewErrors: ptr(int32(2))},
	}, ch.Behavior)
}
