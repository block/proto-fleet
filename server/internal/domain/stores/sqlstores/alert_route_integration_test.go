package sqlstores_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/alerts"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func newAlertRouteStores(t *testing.T) (*sqlstores.SQLAlertRouteStore, *sqlstores.SQLAlertChannelStore) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}
	db := testutil.GetTestDB(t)
	return sqlstores.NewSQLAlertRouteStore(db), sqlstores.NewSQLAlertChannelStore(db)
}

func seedRouteChannel(t *testing.T, channels *sqlstores.SQLAlertChannelStore, org int64, name string) int64 {
	t.Helper()
	rec, err := channels.Insert(context.Background(), alerts.ChannelRecord{
		OrganizationID:  org,
		Name:            name,
		Kind:            alerts.ChannelKindSlack,
		EncryptedConfig: "blob",
		ValidationState: alerts.ValidationPending,
	})
	require.NoError(t, err)
	return rec.ID
}

func TestAlertRouteStoreSetListDelete(t *testing.T) {
	routes, channels := newAlertRouteStores(t)
	ctx := context.Background()
	chA := seedRouteChannel(t, channels, 7, "route-a")
	chB := seedRouteChannel(t, channels, 7, "route-b")

	require.NoError(t, routes.SetPolicy(ctx, 7, alerts.RoutePolicy{RuleUID: "rule-1", Mode: alerts.RouteModeCustom, ChannelIDs: []int64{chA, chB}}))
	require.NoError(t, routes.SetPolicy(ctx, 7, alerts.RoutePolicy{RuleUID: "rule-2", Mode: alerts.RouteModeNone}))

	got, err := routes.ListPolicies(ctx, 7)
	require.NoError(t, err)
	require.Len(t, got, 2)
	byRule := map[string]alerts.RoutePolicy{}
	for _, p := range got {
		byRule[p.RuleUID] = p
	}
	assert.Equal(t, []int64{chA, chB}, byRule["rule-1"].ChannelIDs)
	assert.Equal(t, alerts.RouteModeNone, byRule["rule-2"].Mode)
	assert.Empty(t, byRule["rule-2"].ChannelIDs)

	// Org scoping: another org sees nothing.
	other, err := routes.ListPolicies(ctx, 8)
	require.NoError(t, err)
	assert.Empty(t, other)

	// Upsert replaces the channel set, not appends.
	require.NoError(t, routes.SetPolicy(ctx, 7, alerts.RoutePolicy{RuleUID: "rule-1", Mode: alerts.RouteModeCustom, ChannelIDs: []int64{chB}}))
	got, err = routes.ListPolicies(ctx, 7)
	require.NoError(t, err)
	for _, p := range got {
		if p.RuleUID == "rule-1" {
			assert.Equal(t, []int64{chB}, p.ChannelIDs)
		}
	}

	require.NoError(t, routes.DeletePolicy(ctx, 7, "rule-1"))
	got, err = routes.ListPolicies(ctx, 7)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "rule-2", got[0].RuleUID)

	// Deleting a missing policy is a no-op, matching SetRuleRouting's default mode.
	require.NoError(t, routes.DeletePolicy(ctx, 7, "rule-1"))
}

func TestAlertRouteStoreDropsSoftDeletedChannels(t *testing.T) {
	routes, channels := newAlertRouteStores(t)
	ctx := context.Background()
	chA := seedRouteChannel(t, channels, 9, "live")
	chB := seedRouteChannel(t, channels, 9, "doomed")
	require.NoError(t, routes.SetPolicy(ctx, 9, alerts.RoutePolicy{RuleUID: "rule-1", Mode: alerts.RouteModeCustom, ChannelIDs: []int64{chA, chB}}))

	require.NoError(t, channels.SoftDelete(ctx, 9, chB))

	got, err := routes.ListPolicies(ctx, 9)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, []int64{chA}, got[0].ChannelIDs, "a soft-deleted channel drops out of routing reads")
}
