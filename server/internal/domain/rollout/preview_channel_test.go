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

func TestPreviewScopeValidatesEditedChannel(t *testing.T) {
	f := newFixture(t, 1)
	ctx := t.Context()
	groupA, groupB := f.addGroup(t, "A"), f.addGroup(t, "B")
	scope := Scope{GroupIDs: []int64{groupA}}
	first, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "A", Scope: scope})
	require.NoError(t, err)
	second, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "B", Scope: Scope{GroupIDs: []int64{groupB}}})
	require.NoError(t, err)
	f.placeInSet(t, groupA, "group", "miner-0")
	f.placeInSet(t, groupB, "group", "miner-0")
	creation, err := f.svc.PreviewScope(ctx, f.orgID, scope, 0)
	require.NoError(t, err)
	assert.Equal(t, int32(1), creation.MinerCount)
	assert.Equal(t, int32(2), creation.ConflictCount, "creation previews exclude no channel")
	edited, err := f.svc.PreviewScope(ctx, f.orgID, scope, first.ID)
	require.NoError(t, err)
	assert.Equal(t, creation.MinerCount, edited.MinerCount)
	assert.Equal(t, creation.Models, edited.Models)
	assert.Equal(t, []ScopeConflict{{ChannelID: second.ID, ChannelName: "B", MinerCount: 1}}, edited.Conflicts)
	for _, id := range []int64{0, first.ID} {
		empty, err := f.svc.PreviewScope(ctx, f.orgID, Scope{}, id)
		require.NoError(t, err, "empty scopes are valid for creation and an existing channel")
		assert.Zero(t, empty.MinerCount)
		assert.Empty(t, empty.Conflicts)
	}

	var otherOrg int64
	require.NoError(t, f.conn.QueryRowContext(ctx, `INSERT INTO organization (org_id, name)
		VALUES ('preview-other-org', 'Other') RETURNING id`).Scan(&otherOrg))
	foreign, err := f.svc.CreateChannel(ctx, otherOrg, 1, ChannelSpec{Name: "Foreign"})
	require.NoError(t, err)
	require.NoError(t, f.svc.DeleteChannel(ctx, f.orgID, second.ID))
	for _, tc := range []struct {
		name      string
		channelID int64
	}{{"missing", foreign.ID + 1}, {"deleted", second.ID}, {"foreign", foreign.ID}} {
		t.Run(tc.name, func(t *testing.T) {
			for _, scope := range []Scope{scope, {}} {
				preview, err := f.svc.PreviewScope(ctx, f.orgID, scope, tc.channelID)
				assert.True(t, fleeterror.IsNotFoundError(err), "empty scope=%t: %v", scope.IsEmpty(), err)
				assert.Nil(t, preview)
			}
		})
	}
}

func TestPreviewScopePreservesChannelLookupErrors(t *testing.T) {
	f := newFixture(t, 0)
	ctx := t.Context()
	channel, err := f.svc.CreateChannel(ctx, f.orgID, 1, ChannelSpec{Name: "Existing"})
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = f.svc.PreviewScope(canceled, f.orgID, Scope{}, channel.ID)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, fleeterror.IsNotFoundError(err))

	// An empty preview must still execute and correctly classify the lookup.
	_, err = f.conn.ExecContext(ctx, `ALTER TABLE release_channel RENAME COLUMN description TO hidden_description`)
	require.NoError(t, err)
	_, err = f.svc.PreviewScope(ctx, f.orgID, Scope{}, channel.ID)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, connect.CodeInternal, fleetErr.GRPCCode)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "42703", pgErr.Code)
}
