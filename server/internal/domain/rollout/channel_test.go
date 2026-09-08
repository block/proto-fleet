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
	assert.Equal(t, []ModelCount{{Manufacturer: "proto", Model: "Rig", MinerCount: 2}}, preview.Models)
	assert.Equal(t, int32(1), preview.ModelCount)
	preview, err = f.svc.PreviewScope(ctx, f.orgID, Scope{BuildingIDs: []int64{building}}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(2), preview.MinerCount)
	preview, err = f.svc.PreviewScope(ctx, f.orgID, Scope{SiteIDs: []int64{site}}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(2), preview.MinerCount)
	assert.Equal(t, []ModelCount{{Manufacturer: "proto", Model: "Other", MinerCount: 1}, {Manufacturer: "proto", Model: "Rig", MinerCount: 1}}, preview.Models)

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
	assert.Equal(t, int32(1), preview.ConflictCount)

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
	assert.Equal(t, []string{"miner-4"}, edited.Scope.DeviceIdentifiers, "identifiers round-trip through storage")

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
	assert.Equal(t, int32(1), byName["Rack channel"].ModelGroupCount)
	groups, cursor, err := f.svc.ListChannelModelGroups(ctx, f.orgID, rackChannel.ID, 0, "")
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Empty(t, cursor)
	rig := groups[0]
	assert.Equal(t, "proto", rig.Manufacturer, "observed identity is returned verbatim")
	assert.Equal(t, "Rig", rig.Model)
	assert.Equal(t, int32(4), rig.MinerCount)
	assert.Equal(t, int32(0), rig.OnTargetCount, "nothing assigned yet")
	assert.Equal(t, []string{"1.0.0"}, rig.ReportedVersions)
	assert.Equal(t, int32(1), rig.ReportedVersionCount)
	assert.Empty(t, rig.FirmwareChecksum)
	assert.Zero(t, rig.AssignmentGeneration)

	// The runtime overlap is visible as relations: the rack wins, the site
	// loses; the org-wide and per-channel views agree.
	conflicts, cursor, err := f.svc.ListMembershipConflicts(ctx, f.orgID, 0, 0, "")
	require.NoError(t, err)
	assert.Empty(t, cursor)
	require.Len(t, conflicts, 2)
	assert.Equal(t, "miner-3", conflicts[0].DeviceIdentifier)
	assert.Equal(t, MembershipConflict{
		DeviceID: f.deviceIDs["miner-3"], DeviceIdentifier: "miner-3", Manufacturer: "proto", Model: "Rig",
		ChannelID: rackChannel.ID, ChannelName: "Rack channel", Specificity: 3, Resolution: ResolutionWinner,
	}, conflicts[0])
	assert.Equal(t, ResolutionLoser, conflicts[1].Resolution)
	assert.Equal(t, siteChannel.ID, conflicts[1].ChannelID)
	onlySite, _, err := f.svc.ListMembershipConflicts(ctx, f.orgID, siteChannel.ID, 0, "")
	require.NoError(t, err)
	require.Len(t, onlySite, 1)
	assert.Equal(t, ResolutionLoser, onlySite[0].Resolution)
	_, _, err = f.svc.ListMembershipConflicts(ctx, f.orgID, 0, 0, "garbage")
	assert.ErrorContains(t, err, "invalid cursor")

	// Members are listed separately, by identifier, in pages; the model
	// filter narrows them and the conflict flag rides along.
	page, cursor, err := f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID, "", "", 3, "")
	require.NoError(t, err)
	require.Len(t, page, 3)
	require.NotEmpty(t, cursor)
	assert.Equal(t, []string{"miner-0", "miner-1", "miner-3"}, []string{page[0].DeviceIdentifier, page[1].DeviceIdentifier, page[2].DeviceIdentifier})
	assert.True(t, page[2].Conflicted, "miner-3 is also at the site")
	assert.False(t, page[0].Conflicted)
	rest, cursor, err := f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID, "", "", 3, cursor)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	assert.Equal(t, "miner-4", rest[0].DeviceIdentifier)
	assert.Empty(t, cursor)
	none, _, err := f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID, "", "Other", 0, "")
	require.NoError(t, err)
	assert.Empty(t, none)
	byManufacturer, _, err := f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID, "proto", "Rig", 0, "")
	require.NoError(t, err)
	assert.Len(t, byManufacturer, 4)
	assert.Equal(t, "proto", byManufacturer[0].Manufacturer)
	verbatim, _, err := f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID, "Proto", "", 0, "")
	require.NoError(t, err)
	assert.Empty(t, verbatim, "observed-identity filters match verbatim, not folded")
	_, _, err = f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID, "", "", 0, "garbage")
	assert.ErrorContains(t, err, "invalid cursor")
	_, _, err = f.svc.ListChannelMiners(ctx, f.orgID, rackChannel.ID+1000, "", "", 0, "")
	assert.ErrorContains(t, err, "not found")
	_, _, err = f.svc.ListChannelModelGroups(ctx, f.orgID, rackChannel.ID+1000, 0, "")
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

func TestMinerScopeSurvivesDeletionAndRepairing(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	ch, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{
		Name: "Selected miner", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"miner-0"}, ch.Scope.DeviceIdentifiers)
	assert.Equal(t, int32(1), ch.MinerCount)

	// Removing the current device row leaves the saved selector visible,
	// while its miner is absent from the channel's live membership.
	previousID := f.deviceIDs["miner-0"]
	_, err = f.conn.ExecContext(ctx, `UPDATE device SET deleted_at = now() WHERE id = $1`, previousID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `
		UPDATE discovered_device SET deleted_at = now()
		WHERE id = (SELECT discovered_device_id FROM device WHERE id = $1)
	`, previousID)
	require.NoError(t, err)
	ch, err = f.svc.GetChannel(ctx, f.orgID, ch.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"miner-0"}, ch.Scope.DeviceIdentifiers)
	assert.Zero(t, ch.MinerCount)

	// Re-pairing creates a new device ID for the same identifier. The
	// channel adopts that row without a scope update.
	currentID := f.addMiner(t, "miner-0", "Rig")
	require.NotEqual(t, previousID, currentID)
	members, _, err := f.svc.ListChannelMiners(ctx, f.orgID, ch.ID, "", "", 0, "")
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, currentID, members[0].DeviceID)
	assert.Equal(t, "miner-0", members[0].DeviceIdentifier)
	preview, err := f.svc.PreviewScope(ctx, f.orgID, Scope{DeviceIdentifiers: []string{"miner-0"}}, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(1), preview.MinerCount)
	assert.Equal(t, []ScopeConflict{{ChannelID: ch.ID, ChannelName: ch.Name, MinerCount: 1}}, preview.Conflicts)

	// Replacing the scope also clears its identifier selectors.
	ch, err = f.svc.UpdateChannel(ctx, f.orgID, ch.ID, ChannelSpec{Name: ch.Name})
	require.NoError(t, err)
	assert.Empty(t, ch.Scope.DeviceIdentifiers)
	assert.Zero(t, ch.MinerCount)
}

func TestSpecificityTiesExcludeTheMiner(t *testing.T) {
	f := newFixture(t, 3)
	ctx := t.Context()
	groupA := f.addGroup(t, "Group A")
	groupB := f.addGroup(t, "Group B")
	f.placeInSet(t, groupA, "group", "miner-0")
	f.placeInSet(t, groupB, "group", "miner-1")
	a, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "A", Scope: Scope{GroupIDs: []int64{groupA}}})
	require.NoError(t, err)
	b, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "B", Scope: Scope{GroupIDs: []int64{groupB}}})
	require.NoError(t, err)

	// miner-2 joins both groups after the channels were saved: two group
	// selectors tie, so it belongs to neither until the conflict is removed.
	f.placeInSet(t, groupA, "group", "miner-2")
	f.placeInSet(t, groupB, "group", "miner-2")
	chA, err := f.svc.GetChannel(ctx, f.orgID, a.ID)
	require.NoError(t, err)
	chB, err := f.svc.GetChannel(ctx, f.orgID, b.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(1), chA.MinerCount)
	assert.Equal(t, int32(1), chB.MinerCount)
	conflicts, _, err := f.svc.ListMembershipConflicts(ctx, f.orgID, 0, 0, "")
	require.NoError(t, err)
	require.Len(t, conflicts, 2)
	for _, c := range conflicts {
		assert.Equal(t, "miner-2", c.DeviceIdentifier)
		assert.Equal(t, ResolutionExcludedTie, c.Resolution)
		assert.Equal(t, int32(2), c.Specificity)
	}

	// Paging walks the relations one at a time in (identifier, channel) order.
	first, cursor, err := f.svc.ListMembershipConflicts(ctx, f.orgID, 0, 1, "")
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.NotEmpty(t, cursor)
	second, cursor, err := f.svc.ListMembershipConflicts(ctx, f.orgID, 0, 1, cursor)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Empty(t, cursor)
	assert.Less(t, first[0].ChannelID, second[0].ChannelID)

	f.removeFromSet(t, groupB, "miner-2")
	chA, err = f.svc.GetChannel(ctx, f.orgID, a.ID)
	require.NoError(t, err)
	assert.Equal(t, int32(2), chA.MinerCount, "the tie is gone and the miner belongs to A")
}
