package rollout

import (
	"slices"
	"strconv"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListChannelsPagination(t *testing.T) {
	f := newFixture(t, 0)
	ctx := t.Context()
	empty, next, err := f.svc.ListChannels(ctx, f.orgID, 0, "")
	require.NoError(t, err)
	assert.Empty(t, empty)
	assert.Empty(t, next)
	var otherOrg int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name)
		VALUES ('other-pagination-org', 'Other') RETURNING id`).Scan(&otherOrg))
	foreign, err := f.svc.CreateChannel(ctx, otherOrg, 1, ChannelSpec{Name: "Foreign"})
	require.NoError(t, err)
	rows, err := f.conn.QueryContext(ctx, `INSERT INTO release_channel (org_id, name, created_by)
		SELECT $1, 'Channel ' || lpad((1006 - n)::text, 4, '0'), 1
		FROM generate_series(1, 1005) n RETURNING id`, f.orgID)
	require.NoError(t, err)
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	slices.Sort(ids)
	require.Len(t, ids, 1005)
	channelIDs := func(channels []Channel) []int64 {
		result := make([]int64, 0, len(channels))
		for _, channel := range channels {
			result = append(result, channel.ID)
		}
		return result
	}
	for _, tc := range []struct {
		name string
		size int32
		want int
	}{{"default", 0, 100}, {"custom", 17, 17}, {"maximum", 1000, 1000}, {"clamped", 1001, 1000}} {
		t.Run(tc.name, func(t *testing.T) {
			page, cursor, err := f.svc.ListChannels(ctx, f.orgID, tc.size, "")
			require.NoError(t, err)
			assert.Equal(t, ids[:tc.want], channelIDs(page), "order follows IDs, not names")
			assert.Equal(t, encodeCursor(strconv.FormatInt(ids[tc.want-1], 10)), cursor)
			assert.LessOrEqual(t, len(cursor), 100)
		})
	}
	first, cursor, err := f.svc.ListChannels(ctx, f.orgID, 1000, "")
	require.NoError(t, err)
	rest, next, err := f.svc.ListChannels(ctx, f.orgID, 1000, cursor)
	require.NoError(t, err)
	assert.Equal(t, ids, channelIDs(append(first, rest...)), "every channel appears exactly once")
	assert.Empty(t, next)
	empty, next, err = f.svc.ListChannels(ctx, f.orgID, 1000, encodeCursor(strconv.FormatInt(ids[1004], 10)))
	require.NoError(t, err)
	assert.Empty(t, empty)
	assert.Empty(t, next)
	other, next, err := f.svc.ListChannels(ctx, otherOrg, 1, "")
	require.NoError(t, err)
	assert.Equal(t, []int64{foreign.ID}, channelIDs(other))
	assert.Empty(t, next)

	anchor := ids[16]
	cursor = encodeCursor(strconv.FormatInt(anchor, 10))
	_, err = f.svc.UpdateChannel(ctx, f.orgID, anchor, ChannelSpec{Name: "A renamed anchor"})
	require.NoError(t, err)
	page, _, err := f.svc.ListChannels(ctx, f.orgID, 17, cursor)
	require.NoError(t, err)
	assert.Equal(t, ids[17:34], channelIDs(page))
	require.NoError(t, f.svc.DeleteChannel(ctx, f.orgID, anchor))
	page, _, err = f.svc.ListChannels(ctx, f.orgID, 17, cursor)
	require.NoError(t, err)
	assert.Equal(t, ids[17:34], channelIDs(page), "continuation does not need the cursor's channel")
	for _, cursor := range []string{"%", encodeCursor("invalid"), encodeCursor("0"), encodeCursor("-1"), encodeCursor("9223372036854775808"), encodeCursor("1", "2")} {
		_, _, err := f.svc.ListChannels(ctx, f.orgID, 1, cursor)
		assert.True(t, fleeterror.IsInvalidArgumentError(err), "cursor %q: %v", cursor, err)
	}
}

func TestListChannelsPageCountsIncludeOffPageResolution(t *testing.T) {
	for _, tie := range []bool{false, true} {
		name := "more specific winner"
		if tie {
			name = "same specificity tie"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, 3)
			ctx := t.Context()
			f.addMiner(t, "folded", " RIG ")
			site, group := f.addSite(t, "Site"), f.addGroup(t, "First")
			firstScope := Scope{SiteIDs: []int64{site}}
			if tie {
				firstScope = Scope{GroupIDs: []int64{group}}
			}
			first, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "First", Scope: firstScope})
			require.NoError(t, err)
			competitor := f.addGroup(t, "Off page")
			second, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Second", Scope: Scope{GroupIDs: []int64{competitor}}})
			require.NoError(t, err)
			// Placement changes introduce an overlap after channel creation.
			for _, miner := range []string{"miner-0", "miner-1", "miner-2", "folded"} {
				f.placeAtSite(t, site, miner)
				f.placeInSet(t, group, "group", miner)
			}
			f.placeInSet(t, competitor, "group", "miner-0")
			_, err = f.conn.ExecContext(ctx, `INSERT INTO release_channel_firmware
				(channel_id, manufacturer, model, firmware_checksum, assigned_by) VALUES
				($1, 'PROTO', 'rig', 'assigned', 1), ($1, 'proto', 'orphan', 'assigned', 1),
				($1, 'proto', 'cleared', '', 1), ($2, 'proto', 'off-page', 'assigned', 1)`, first.ID, second.ID)
			require.NoError(t, err)
			page, cursor, err := f.svc.ListChannels(ctx, f.orgID, 1, "")
			require.NoError(t, err)
			require.Len(t, page, 1)
			assert.Equal(t, first.ID, page[0].ID)
			assert.True(t, page[0].Scope.IsEmpty(), "listing returns summaries without scope selectors")
			assert.Equal(t, int32(3), page[0].MinerCount, "off-page winners and ties remove miner-0")
			assert.Equal(t, int32(3), page[0].ModelGroupCount, "two raw observed pairs plus one active orphan; folded matches and cleared assignments add nothing")
			require.NotEmpty(t, cursor)
		})
	}
}

func TestListChannelsDoesNotHydrateRetainedScopeSelectors(t *testing.T) {
	f := newFixture(t, 2)
	ctx := t.Context()
	site := f.addSite(t, "Empty site")
	channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{
		Name: "Large scope", Scope: Scope{SiteIDs: []int64{site}, DeviceIdentifiers: []string{"miner-0", "miner-1"}},
	})
	require.NoError(t, err)
	softDeleteSelectedMiner(t, f, f.deviceIDs["miner-1"])
	// Seed the durable identifiers left behind after miners are removed.
	// A full scope may retain 10,000 miner selectors even with one live member.
	_, err = f.conn.ExecContext(ctx, `INSERT INTO release_channel_target (channel_id, target_type, device_identifier)
		SELECT $1, 'miner', 'retained-' || lpad(n::text, 4, '0') FROM generate_series(1, 9998) n`, channel.ID)
	require.NoError(t, err)
	_, err = f.conn.ExecContext(ctx, `INSERT INTO release_channel_firmware
		(channel_id, manufacturer, model, firmware_checksum, assigned_by) VALUES ($1, 'proto', 'orphan', 'assigned', 1)`, channel.ID)
	require.NoError(t, err)
	second, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Second"})
	require.NoError(t, err)
	full, err := f.svc.GetChannel(ctx, f.orgID, channel.ID)
	require.NoError(t, err)
	require.Len(t, full.Scope.DeviceIdentifiers, 10000)
	assert.Contains(t, full.Scope.DeviceIdentifiers, "miner-1", "deleted miner selectors remain durable")
	assert.Equal(t, []int64{site}, full.Scope.SiteIDs)
	assert.Equal(t, int32(1), full.MinerCount)
	assert.Equal(t, int32(2), full.ModelGroupCount)

	// PostgreSQL keeps the membership views bound to the renamed column, but
	// the sqlc target-hydration query still references its old name. Summary
	// listing must only read the grouped membership results, not target rows.
	_, err = f.conn.ExecContext(ctx, `ALTER TABLE release_channel_target RENAME COLUMN device_identifier TO hidden_identifier`)
	require.NoError(t, err)
	_, err = f.svc.store.GetQueries(ctx).ListReleaseChannelPageTargets(ctx, sqlc.ListReleaseChannelPageTargetsParams{
		OrgID: f.orgID, ChannelIds: []int64{channel.ID},
	})
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "42703", pgErr.Code, "the full-scope hydration query is unavailable")

	page, cursor, err := f.svc.ListChannels(ctx, f.orgID, 1, "")
	require.NoError(t, err, "summaries must not depend on target hydration")
	require.Len(t, page, 1)
	expectedSummary := *full
	expectedSummary.Scope = Scope{}
	assert.Equal(t, expectedSummary, page[0])
	assert.Equal(t, encodeCursor(strconv.FormatInt(channel.ID, 10)), cursor)
	page, next, err := f.svc.ListChannels(ctx, f.orgID, 1, cursor)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, second.ID, page[0].ID)
	assert.Empty(t, next)

	_, err = f.svc.GetChannel(ctx, f.orgID, channel.ID)
	require.ErrorAs(t, err, &pgErr, "full channel reads must still hydrate their scope")
	assert.Equal(t, "42703", pgErr.Code)
	_, err = f.conn.ExecContext(ctx, `ALTER TABLE release_channel_target RENAME COLUMN hidden_identifier TO device_identifier`)
	require.NoError(t, err)
	restored, err := f.svc.GetChannel(ctx, f.orgID, channel.ID)
	require.NoError(t, err)
	assert.Equal(t, full, restored, "full channel scope round-trips after the query is restored")
}
