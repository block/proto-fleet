package rollout

import (
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSummariesExcludeClearedAssignmentHistory(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	f.addMiner(t, "observed-cleared", "Observed cleared")
	channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{
		Name: "Assignment history", Scope: Scope{DeviceIdentifiers: []string{"miner-0", "observed-cleared"}},
	})
	require.NoError(t, err)
	offPage, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Off page"})
	require.NoError(t, err)
	var otherOrg int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name)
		VALUES ('other-assignment-history-org', 'Other') RETURNING id`).Scan(&otherOrg))
	foreign, err := f.svc.CreateChannel(ctx, otherOrg, 1, ChannelSpec{Name: "Foreign"})
	require.NoError(t, err)

	q := f.svc.store.GetQueries(ctx)
	assign := func(channelID int64, model string) sqlc.ReleaseChannelFirmware {
		t.Helper()
		row, err := q.UpsertReleaseChannelFirmware(ctx, sqlc.UpsertReleaseChannelFirmwareParams{
			ChannelID: channelID, Manufacturer: "proto", Model: model, FirmwareChecksum: checksum1,
			FirmwareVersion: "1.5.0", FirmwareTargetManufacturer: "proto", FirmwareTargetModel: model, AssignedBy: 1,
		})
		require.NoError(t, err)
		return row
	}
	assign(channel.ID, "rig")
	assign(channel.ID, "active orphan")
	assign(channel.ID, "observed cleared")
	cleared, err := q.ClearReleaseChannelFirmware(ctx, sqlc.ClearReleaseChannelFirmwareParams{
		ChannelID: channel.ID, Manufacturer: "proto", Model: "observed cleared", AssignedBy: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), cleared.AssignmentGeneration)
	assign(offPage.ID, "off-page assignment")
	assign(foreign.ID, "foreign assignment")
	// Cleared pairs remain durable so later assignments can advance their
	// generations. This history can grow independently of the channel's members.
	_, err = f.conn.ExecContext(ctx, `INSERT INTO release_channel_firmware
		(channel_id, manufacturer, model, firmware_checksum, assignment_generation, assigned_by)
		SELECT $1, 'proto', 'retained-' || n, '', 2, 1 FROM generate_series(1, 1000) n`, channel.ID)
	require.NoError(t, err)

	page, cursor, err := f.svc.ListChannels(ctx, f.orgID, 1, "")
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, channel.ID, page[0].ID)
	assert.NotEmpty(t, cursor)
	assert.Equal(t, int32(2), page[0].MinerCount)
	assert.Equal(t, int32(3), page[0].ModelGroupCount, "count both observed pairs and the active orphan, but no cleared-only orphans")

	rows, err := q.ListReleaseChannelPageFirmware(ctx, sqlc.ListReleaseChannelPageFirmwareParams{
		OrgID: f.orgID, ChannelIds: []int64{channel.ID, foreign.ID},
	})
	require.NoError(t, err)
	require.Equal(t, 2, len(rows), "summary hydration must exclude durable cleared rows before returning them to Go")
	for _, row := range rows {
		assert.Equal(t, channel.ID, row.ChannelID, "exclude foreign and off-page channels")
		assert.Equal(t, checksum1, row.FirmwareChecksum)
	}
	assert.Equal(t, []string{"active orphan", "rig"}, []string{rows[0].Model, rows[1].Model})

	retained, err := q.GetReleaseChannelFirmware(ctx, sqlc.GetReleaseChannelFirmwareParams{
		ChannelID: channel.ID, Manufacturer: "proto", Model: "retained-1",
	})
	require.NoError(t, err, "summary filtering must not delete retained assignment generations")
	assert.Empty(t, retained.FirmwareChecksum)
	assert.Equal(t, int64(2), retained.AssignmentGeneration)
	reassigned := assign(channel.ID, "retained-1")
	assert.Equal(t, retained.AssignmentGeneration+1, reassigned.AssignmentGeneration)
	page, _, err = f.svc.ListChannels(ctx, f.orgID, 1, "")
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, int32(4), page[0].ModelGroupCount, "reassigning a retained orphan makes it count again")
}
