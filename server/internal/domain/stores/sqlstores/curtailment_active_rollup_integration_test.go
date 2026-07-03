package sqlstores_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// Pins the live per-event rollup on the active-events list: aggregate counts
// cover every target state (dispatching/dispatched conflated, unavailable
// included), and target-less events aggregate to a zeroed non-nil rollup
// instead of failing or returning nil.
func TestSQLCurtailmentStore_ListActiveEvents_ReturnsLiveTargetRollups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	targetedUUID := uuid.New()
	_, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, targetedUUID, models.EventStateActive, "active-rollup-targeted"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("rollup-pending", models.TargetStatePending, models.DesiredStateCurtailed),
			curtailmentStoreTestTarget("rollup-dispatching", models.TargetStateDispatching, models.DesiredStateCurtailed),
			curtailmentStoreTestTarget("rollup-dispatched", models.TargetStateDispatched, models.DesiredStateCurtailed),
			curtailmentStoreTestTarget("rollup-confirmed-a", models.TargetStateConfirmed, models.DesiredStateCurtailed),
			curtailmentStoreTestTarget("rollup-confirmed-b", models.TargetStateConfirmed, models.DesiredStateCurtailed),
			curtailmentStoreTestTarget("rollup-drifted", models.TargetStateDrifted, models.DesiredStateCurtailed),
			curtailmentStoreTestTarget("rollup-unavailable", models.TargetStateUnavailable, models.DesiredStateCurtailed),
			curtailmentStoreTestTarget("rollup-resolved", models.TargetStateResolved, models.DesiredStateActive),
			curtailmentStoreTestTarget("rollup-released", models.TargetStateReleased, models.DesiredStateActive),
			curtailmentStoreTestTarget("rollup-restore-failed", models.TargetStateRestoreFailed, models.DesiredStateActive),
		},
	)
	require.NoError(t, err)

	targetlessUUID := uuid.New()
	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, targetlessUUID, models.ScopeTypeWholeOrg, 0, "active-rollup-targetless"),
		nil,
	)
	require.NoError(t, err)

	events, err := store.ListActiveEvents(ctx, user.OrganizationID)
	require.NoError(t, err)

	byUUID := make(map[uuid.UUID]*models.Event, len(events))
	for _, event := range events {
		byUUID[event.EventUUID] = event
	}

	targeted := byUUID[targetedUUID]
	require.NotNil(t, targeted)
	require.NotNil(t, targeted.TargetRollup)
	assert.Equal(t, &models.TargetRollup{
		Pending:       1,
		Dispatched:    2, // dispatching + dispatched conflate in operator rollups
		Confirmed:     2,
		Drifted:       1,
		Resolved:      1,
		Released:      1,
		RestoreFailed: 1,
		Unavailable:   1,
		Total:         10,
	}, targeted.TargetRollup)

	targetless := byUUID[targetlessUUID]
	require.NotNil(t, targetless)
	require.NotNil(t, targetless.TargetRollup, "target-less events must carry a zeroed rollup, not nil")
	assert.Equal(t, &models.TargetRollup{}, targetless.TargetRollup)
}
