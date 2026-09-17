package rollout

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListMembershipConflictsValidatesChannel(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	groupA, groupB := f.addGroup(t, "A"), f.addGroup(t, "B")
	first, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "A", Scope: Scope{GroupIDs: []int64{groupA}}})
	require.NoError(t, err)
	second, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "B", Scope: Scope{GroupIDs: []int64{groupB}}})
	require.NoError(t, err)
	f.placeInSet(t, groupA, "group", "miner-0")
	f.placeInSet(t, groupB, "group", "miner-0")
	all, cursor, err := f.svc.ListMembershipConflicts(ctx, f.orgID, 0, 0, "")
	require.NoError(t, err)
	require.Len(t, all, 2, "zero channel ID retains the organization-wide view")
	assert.Empty(t, cursor)
	for _, channel := range []*Channel{first, second} {
		scoped, cursor, err := f.svc.ListMembershipConflicts(ctx, f.orgID, channel.ID, 0, "")
		require.NoError(t, err)
		require.Len(t, scoped, 1)
		assert.Equal(t, channel.ID, scoped[0].ChannelID)
		assert.Equal(t, f.deviceIDs["miner-0"], scoped[0].DeviceID)
		assert.Empty(t, cursor)
	}
	empty, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Empty"})
	require.NoError(t, err)
	conflicts, cursor, err := f.svc.ListMembershipConflicts(ctx, f.orgID, empty.ID, 0, "")
	require.NoError(t, err, "a real channel with no conflicts is a successful empty page")
	assert.Empty(t, conflicts)
	assert.Empty(t, cursor)

	var otherOrg int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name)
		VALUES ('conflict-other-org', 'Other') RETURNING id`).Scan(&otherOrg))
	foreign, err := f.svc.CreateChannel(ctx, otherOrg, 1, ChannelSpec{Name: "Foreign"})
	require.NoError(t, err)
	require.NoError(t, f.svc.DeleteChannel(ctx, f.orgID, empty.ID))
	for _, tc := range []struct {
		name      string
		channelID int64
	}{{"missing", foreign.ID + 1}, {"deleted", empty.ID}, {"foreign", foreign.ID}} {
		t.Run(tc.name, func(t *testing.T) {
			conflicts, cursor, err := f.svc.ListMembershipConflicts(ctx, f.orgID, tc.channelID, 0, "")
			assert.True(t, fleeterror.IsNotFoundError(err), "%v", err)
			assert.Empty(t, conflicts)
			assert.Empty(t, cursor)
		})
	}
}

func TestListMembershipConflictsPreservesLookupErrors(t *testing.T) {
	f := newFixture(t, 0)
	ctx := t.Context()
	channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Existing"})
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = f.svc.ListMembershipConflicts(canceled, f.orgID, channel.ID, 0, "")
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, fleeterror.IsNotFoundError(err))

	// Break only the lookup in this disposable database. A query failure is
	// distinct from a missing or foreign channel and preserves its SQL cause.
	_, err = f.conn.ExecContext(ctx, `ALTER TABLE release_channel RENAME COLUMN description TO hidden_description`)
	require.NoError(t, err)
	_, _, err = f.svc.ListMembershipConflicts(ctx, f.orgID, channel.ID, 0, "")
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "42703", pgErr.Code)
}
