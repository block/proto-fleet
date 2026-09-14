package rollout

import (
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func softDeleteSelectedMiner(t *testing.T, f *fixture, deviceID int64) {
	t.Helper()
	_, err := f.conn.ExecContext(t.Context(), `UPDATE device SET deleted_at = now() WHERE id = $1`, deviceID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(t.Context(), `
		UPDATE discovered_device SET deleted_at = now()
		WHERE id = (SELECT discovered_device_id FROM device WHERE id = $1)
	`, deviceID)
	require.NoError(t, err)
}

func TestChannelMinerSelectorsRetainDeletedUntilRepairing(t *testing.T) {
	for _, identifier := range []string{"miner-0", " miner-0 ", " \t\u00a0"} {
		t.Run(identifier, func(t *testing.T) {
			f := newFixture(t, 0)
			ctx := t.Context()
			previousID := f.addMiner(t, identifier, "Rig")
			f.addMiner(t, "live-miner", "Rig")
			channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{
				Name: "Original", Scope: Scope{DeviceIdentifiers: []string{identifier, identifier}},
			})
			require.NoError(t, err)
			softDeleteSelectedMiner(t, f, previousID)

			channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
				Name: "Renamed", Description: "Changed while the miner is absent",
				Scope:    Scope{DeviceIdentifiers: []string{identifier, identifier}},
				Behavior: Behavior{Method: MethodBatched, BatchSize: 2},
			})
			require.NoError(t, err, "a saved identifier must not block name or behavior edits while its miner is deleted")
			assert.Equal(t, "Renamed", channel.Name)
			assert.Equal(t, MethodBatched, channel.Behavior.Method)
			assert.Equal(t, int32(2), channel.Behavior.BatchSize)
			assert.Equal(t, []string{identifier}, channel.Scope.DeviceIdentifiers, "retention preserves the exact opaque key")
			assert.Zero(t, channel.MinerCount)

			channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
				Name: "Expanded", Scope: Scope{DeviceIdentifiers: []string{identifier, "live-miner"}},
			})
			require.NoError(t, err, "retained missing and newly added live miners may share a scope")
			assert.Equal(t, int32(1), channel.MinerCount)
			_, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
				Name: "Rejected", Scope: Scope{DeviceIdentifiers: []string{identifier, "live-miner", identifier + " "}},
			})
			require.True(t, fleeterror.IsInvalidArgumentError(err), "a differently spelled key is a new unresolved addition: %v", err)
			unchanged, err := f.svc.GetChannel(ctx, f.orgID, channel.ID)
			require.NoError(t, err)
			assert.Equal(t, *channel, *unchanged)

			channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
				Name: "Removed", Scope: Scope{DeviceIdentifiers: []string{"live-miner"}},
			})
			require.NoError(t, err)
			_, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
				Name: "Readded too early", Scope: Scope{DeviceIdentifiers: []string{identifier, "live-miner"}},
			})
			require.True(t, fleeterror.IsInvalidArgumentError(err), "removal ends the saved identifier's retention: %v", err)
			unchanged, err = f.svc.GetChannel(ctx, f.orgID, channel.ID)
			require.NoError(t, err)
			assert.Equal(t, *channel, *unchanged)
			_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Absent miner", Scope: Scope{DeviceIdentifiers: []string{identifier}}})
			require.True(t, fleeterror.IsInvalidArgumentError(err), "new channels still require a current miner: %v", err)

			currentID := f.addMiner(t, identifier, "Rig")
			require.NotEqual(t, previousID, currentID)
			channel, err = f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
				Name: "Repaired", Scope: Scope{DeviceIdentifiers: []string{identifier, "live-miner"}},
			})
			require.NoError(t, err)
			assert.Equal(t, int32(2), channel.MinerCount)
			members, _, err := f.svc.ListChannelMiners(ctx, f.orgID, channel.ID, "", "", 0, "")
			require.NoError(t, err)
			require.Len(t, members, 2)
			memberIDs := map[string]int64{}
			for _, member := range members {
				memberIDs[member.DeviceIdentifier] = member.DeviceID
			}
			assert.Equal(t, map[string]int64{identifier: currentID, "live-miner": f.deviceIDs["live-miner"]}, memberIDs)
		})
	}
}

func TestChannelMinerSelectorRetentionRejectsForeignAndMissingAdditions(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Original", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}}})
	require.NoError(t, err)
	softDeleteSelectedMiner(t, f, f.deviceIDs["miner-0"])
	channel, err = f.svc.GetChannel(ctx, f.orgID, channel.ID)
	require.NoError(t, err)
	foreign := *f
	require.NoError(t, f.conn.QueryRowContext(ctx, `
		INSERT INTO organization (org_id, name) VALUES ('foreign-org', 'Foreign') RETURNING id
	`).Scan(&foreign.orgID))
	foreign.addMiner(t, "foreign-miner", "Rig")
	for _, identifier := range []string{"missing-miner", "foreign-miner"} {
		t.Run(identifier, func(t *testing.T) {
			_, err := f.svc.UpdateChannel(ctx, f.orgID, channel.ID, ChannelSpec{
				Name: "Rejected", Scope: Scope{DeviceIdentifiers: []string{"miner-0", identifier}},
			})
			require.ErrorContains(t, err, identifier, "the new addition is invalid, not the retained missing miner")
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			unchanged, err := f.svc.GetChannel(ctx, f.orgID, channel.ID)
			require.NoError(t, err)
			assert.Equal(t, *channel, *unchanged)
			_, err = f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Rejected", Scope: Scope{DeviceIdentifiers: []string{identifier}}})
			require.True(t, fleeterror.IsInvalidArgumentError(err), "%v", err)
			channels, _, err := f.svc.ListChannels(ctx, f.orgID, 0, "")
			require.NoError(t, err)
			assert.Len(t, channels, 1, "rejected creates must not persist a channel")
		})
	}
}

func TestChannelMinerSelectorRetentionIsChannelSpecific(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	owner, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Owner", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}}})
	require.NoError(t, err)
	other, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Other"})
	require.NoError(t, err)
	softDeleteSelectedMiner(t, f, f.deviceIDs["miner-0"])
	_, err = f.svc.UpdateChannel(ctx, f.orgID, other.ID, ChannelSpec{Name: "Rejected", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}}})
	require.True(t, fleeterror.IsInvalidArgumentError(err), "another channel's saved selector cannot authorize a missing addition: %v", err)
	unchanged, err := f.svc.GetChannel(ctx, f.orgID, other.ID)
	require.NoError(t, err)
	assert.Equal(t, *other, *unchanged)
	_, err = f.svc.UpdateChannel(ctx, f.orgID, owner.ID, ChannelSpec{Name: "Retained", Scope: Scope{DeviceIdentifiers: []string{"miner-0"}}})
	require.NoError(t, err)
}
