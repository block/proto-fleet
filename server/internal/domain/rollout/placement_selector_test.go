package rollout

import (
	"fmt"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type placementSelectorCase struct {
	kind   string
	table  string
	ids    func(Scope) []int64
	withID func(...int64) Scope
}

var placementSelectorCases = []placementSelectorCase{
	{"site", "site", func(s Scope) []int64 { return s.SiteIDs }, func(ids ...int64) Scope { return Scope{SiteIDs: ids} }},
	{"building", "building", func(s Scope) []int64 { return s.BuildingIDs }, func(ids ...int64) Scope { return Scope{BuildingIDs: ids} }},
	{"rack", "device_set", func(s Scope) []int64 { return s.RackIDs }, func(ids ...int64) Scope { return Scope{RackIDs: ids} }},
	{"group", "device_set", func(s Scope) []int64 { return s.GroupIDs }, func(ids ...int64) Scope { return Scope{GroupIDs: ids} }},
}

func emptyPlacementScope(t *testing.T, f *fixture, name string) Scope {
	t.Helper()
	siteID := f.addSite(t, name)
	buildingID := f.addBuilding(t, siteID, name)
	rackID := f.addRack(t, siteID, buildingID, name)
	groupID := f.addGroup(t, name)
	return Scope{SiteIDs: []int64{siteID}, BuildingIDs: []int64{buildingID}, RackIDs: []int64{rackID}, GroupIDs: []int64{groupID}}
}

func softDeletePlacement(t *testing.T, f *fixture, selector placementSelectorCase, id int64) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), fmt.Sprintf(`UPDATE %s SET deleted_at = now() WHERE id = $1`, selector.table), id)
	require.NoError(t, err)
}

func TestChannelPlacementSelectorsRejectUnresolvedAdditions(t *testing.T) {
	for _, selector := range placementSelectorCases {
		problems := []string{"missing", "foreign org", "deleted"}
		if selector.kind == "rack" || selector.kind == "group" {
			problems = append(problems, "wrong type")
		}
		for _, problem := range problems {
			for _, operation := range []string{"create", "update"} {
				t.Run(selector.kind+"/"+problem+"/"+operation, func(t *testing.T) {
					f := newFixture(t, 2)
					ctx := t.Context()
					local := emptyPlacementScope(t, f, "Local")
					original, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{
						Name: "Original", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}},
					})
					require.NoError(t, err)
					badID := int64(999999)
					switch problem {
					case "foreign org":
						foreign := *f
						require.NoError(t, f.conn.QueryRowContext(ctx, `
							INSERT INTO organization (org_id, name) VALUES ('other-org', 'Other') RETURNING id
						`).Scan(&foreign.orgID))
						badID = selector.ids(emptyPlacementScope(t, &foreign, "Foreign"))[0]
					case "deleted":
						badID = selector.ids(local)[0]
						softDeletePlacement(t, f, selector, badID)
					case "wrong type":
						badID = local.GroupIDs[0]
						if selector.kind == "group" {
							badID = local.RackIDs[0]
						}
					}
					scope := selector.withID(badID)
					scope.DeviceIdentifiers = []string{"miner-1"}
					spec := ChannelSpec{Name: "Changed", Description: "Must not persist", Scope: scope, Behavior: Behavior{Method: MethodBatched, BatchSize: 2}}
					if operation == "create" {
						_, err = f.svc.CreateChannel(ctx, f.orgID, 1, spec)
					} else {
						_, err = f.svc.UpdateChannel(ctx, f.orgID, original.ID, spec)
					}
					require.Error(t, err)
					assert.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
					assert.Contains(t, err.Error(), selector.kind)
					channels, err := f.svc.ListChannels(ctx, f.orgID)
					require.NoError(t, err)
					require.Len(t, channels, 1, "rejected creates must not persist a channel")
					assert.Equal(t, *original, channels[0], "rejected updates must preserve channel fields and targets")
				})
			}
		}
	}
}

func TestChannelPlacementSelectorsRetainDeletedButRejectReadded(t *testing.T) {
	for _, selector := range placementSelectorCases {
		t.Run(selector.kind, func(t *testing.T) {
			f := newFixture(t, 0)
			ctx := t.Context()
			first := selector.ids(emptyPlacementScope(t, f, "First"))[0]
			second := selector.ids(emptyPlacementScope(t, f, "Second"))[0]
			channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Empty", Scope: selector.withID(first, first)})
			require.NoError(t, err, "a valid empty container is a usable future scope")
			assert.Equal(t, int32(0), channel.MinerCount)
			assert.Equal(t, []int64{first}, selector.ids(channel.Scope), "exact duplicate selectors normalize once")
			channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{Name: "Expanded", Scope: selector.withID(first, second)})
			require.NoError(t, err, "updates may add a valid empty container")

			softDeletePlacement(t, f, selector, first)
			retained := selector.withID(first, second)
			channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{Name: "Renamed", Scope: retained})
			require.NoError(t, err, "a later deletion must not prevent unrelated channel edits")
			assert.ElementsMatch(t, []int64{first, second}, selector.ids(channel.Scope))
			_, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{Name: "Rejected", Scope: selector.withID(first, second, 999999)})
			require.True(t, fleeterror.IsInvalidArgumentError(err), "retained IDs do not excuse an invalid addition: %v", err)
			unchanged, err := f.svc.GetChannel(ctx, f.orgID, channel.ID)
			require.NoError(t, err)
			assert.Equal(t, *channel, *unchanged)

			channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{Name: "Removed", Scope: selector.withID(second)})
			require.NoError(t, err)
			assert.Equal(t, []int64{second}, selector.ids(channel.Scope))
			_, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{Name: "Readded", Scope: retained})
			require.True(t, fleeterror.IsInvalidArgumentError(err), "a removed deleted ID becomes an invalid new addition: %v", err)
			unchanged, err = f.svc.GetChannel(ctx, f.orgID, channel.ID)
			require.NoError(t, err)
			assert.Equal(t, *channel, *unchanged)

			channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{Name: "Cleared"})
			require.NoError(t, err)
			assert.True(t, channel.Scope.IsEmpty())
		})
	}
}

func TestChannelPlacementSelectorRetentionIsTypeSpecific(t *testing.T) {
	for _, selector := range placementSelectorCases[2:] {
		t.Run(selector.kind, func(t *testing.T) {
			f := newFixture(t, 0)
			ctx := t.Context()
			id := selector.ids(emptyPlacementScope(t, f, "Local"))[0]
			channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Original", Scope: selector.withID(id)})
			require.NoError(t, err)
			wrongType := Scope{RackIDs: []int64{id}}
			if selector.kind == "rack" {
				wrongType = Scope{GroupIDs: []int64{id}}
			}
			_, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{Name: "Wrong type", Scope: wrongType})
			require.True(t, fleeterror.IsInvalidArgumentError(err), "retention applies to the saved selector type as well as its ID: %v", err)
			unchanged, err := f.svc.GetChannel(ctx, f.orgID, channel.ID)
			require.NoError(t, err)
			assert.Equal(t, *channel, *unchanged)
		})
	}
}
