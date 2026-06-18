package sqlstores_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_InsertTargetsForFullFleetCurtailmentPhaseGuardsAndRefreshesSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	eventUUID := uuid.New()
	event := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "dynamic-full-fleet")
	event.Mode = models.ModeFullFleet
	event.ScopeType = models.ScopeTypeWholeOrg
	event.ScopeJSON = []byte(`{}`)
	event.ModeParamsJSON = []byte(`{}`)
	event.DecisionSnapshotJSON = []byte(`{"selected_count":1,"estimated_reduction_kw":3}`)

	initialBaseline := 3000.0
	inserted, err := store.InsertEventWithTargets(
		ctx,
		event,
		[]models.InsertTargetParams{
			{
				DeviceIdentifier: "miner-existing",
				TargetType:       "miner",
				State:            models.TargetStateConfirmed,
				DesiredState:     models.DesiredStateCurtailed,
				BaselinePowerW:   &initialBaseline,
			},
		},
	)
	require.NoError(t, err)

	newBaseline := 2000.0
	rows, err := store.InsertTargetsForFullFleetCurtailmentPhase(
		ctx,
		user.OrganizationID,
		inserted.ID,
		[]models.InsertTargetParams{
			{
				DeviceIdentifier: "miner-existing",
				TargetType:       "miner",
				State:            models.TargetStatePending,
				DesiredState:     models.DesiredStateCurtailed,
				BaselinePowerW:   &initialBaseline,
			},
			{
				DeviceIdentifier: "miner-new",
				TargetType:       "miner",
				State:            models.TargetStatePending,
				DesiredState:     models.DesiredStateCurtailed,
				BaselinePowerW:   &newBaseline,
			},
		},
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "miner-new", rows[0].DeviceIdentifier)

	updated, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(updated.DecisionSnapshotJSON, &snapshot))
	assert.Equal(t, float64(2), snapshot["selected_count"])
	assert.Equal(t, float64(5), snapshot["estimated_reduction_kw"])

	fixedEventUUID := uuid.New()
	fixedInserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, fixedEventUUID, models.EventStateActive, "fixed-kw"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-fixed-existing", models.TargetStatePending, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)
	guardedRows, err := store.InsertTargetsForFullFleetCurtailmentPhase(
		ctx,
		user.OrganizationID,
		fixedInserted.ID,
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-fixed-new", models.TargetStatePending, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)
	assert.Empty(t, guardedRows)
}

func TestSQLCurtailmentStore_TargetPhaseSummariesThroughCurtailRestoreLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	eventUUID := uuid.New()
	inserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "phase-lifecycle"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-phase-lifecycle", models.TargetStatePending, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)

	curtailedDesired := models.DesiredStateCurtailed
	curtailDispatchedAt := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	curtailBatch := "batch-curtail-phase"
	require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, "miner-phase-lifecycle", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateDispatched,
		LastDispatchedAt:     &curtailDispatchedAt,
		LastBatchUUID:        &curtailBatch,
		ExpectedDesiredState: &curtailedDesired,
	}))
	curtailCompletedAt := curtailDispatchedAt.Add(10 * time.Second)
	require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, "miner-phase-lifecycle", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateConfirmed,
		ConfirmedAt:          &curtailCompletedAt,
		ExpectedDesiredState: &curtailedDesired,
	}))

	targets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, models.TargetStateConfirmed, targets[0].CurtailPhase.State)
	assertTimeEqual(t, curtailDispatchedAt, targets[0].CurtailPhase.DispatchedAt)
	require.NotNil(t, targets[0].CurtailPhase.BatchUUID)
	assert.Equal(t, curtailBatch, *targets[0].CurtailPhase.BatchUUID)
	assertTimeEqual(t, curtailCompletedAt, targets[0].CurtailPhase.CompletedAt)

	_, err = store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	activeDesired := models.DesiredStateActive
	restoreDispatchedAt := curtailCompletedAt.Add(30 * time.Second)
	restoreBatch := "batch-restore-phase"
	require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, "miner-phase-lifecycle", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateDispatched,
		LastDispatchedAt:     &restoreDispatchedAt,
		LastBatchUUID:        &restoreBatch,
		ExpectedDesiredState: &activeDesired,
	}))
	restoreCompletedAt := restoreDispatchedAt.Add(10 * time.Second)
	require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, "miner-phase-lifecycle", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateResolved,
		ConfirmedAt:          &restoreCompletedAt,
		ExpectedDesiredState: &activeDesired,
	}))

	targets, err = store.ListTargetsByEvent(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.NotNil(t, targets[0].RestorePhase)
	assert.Equal(t, models.TargetStateResolved, targets[0].RestorePhase.State)
	assertTimeEqual(t, restoreDispatchedAt, targets[0].RestorePhase.DispatchedAt)
	require.NotNil(t, targets[0].RestorePhase.BatchUUID)
	assert.Equal(t, restoreBatch, *targets[0].RestorePhase.BatchUUID)
	assertTimeEqual(t, restoreCompletedAt, targets[0].RestorePhase.CompletedAt)
}

func TestSQLCurtailmentStore_ReleasedTargetsCompletePhaseSummaries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	curtailEventUUID := uuid.New()
	curtailInserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, curtailEventUUID, models.EventStateActive, "released-curtail"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-released-curtail", models.TargetStatePending, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)
	curtailedDesired := models.DesiredStateCurtailed
	curtailReleasedAt := time.Date(2026, 6, 6, 11, 0, 0, 0, time.UTC)
	require.NoError(t, store.UpdateTargetState(ctx, curtailInserted.ID, "miner-released-curtail", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateReleased,
		ConfirmedAt:          &curtailReleasedAt,
		ExpectedDesiredState: &curtailedDesired,
	}))

	curtailTargets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, curtailEventUUID)
	require.NoError(t, err)
	require.Len(t, curtailTargets, 1)
	assert.Equal(t, models.TargetStateReleased, curtailTargets[0].CurtailPhase.State)
	assertTimeEqual(t, curtailReleasedAt, curtailTargets[0].CurtailPhase.CompletedAt)

	restoreEventUUID := uuid.New()
	restoreInserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, restoreEventUUID, models.EventStateActive, "released-restore"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-released-restore", models.TargetStateConfirmed, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)
	_, err = store.BeginRestoreTransition(ctx, user.OrganizationID, restoreEventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	activeDesired := models.DesiredStateActive
	restoreReleasedAt := time.Date(2026, 6, 6, 11, 30, 0, 0, time.UTC)
	require.NoError(t, store.UpdateTargetState(ctx, restoreInserted.ID, "miner-released-restore", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateReleased,
		ConfirmedAt:          &restoreReleasedAt,
		ExpectedDesiredState: &activeDesired,
	}))

	restoreTargets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, restoreEventUUID)
	require.NoError(t, err)
	require.Len(t, restoreTargets, 1)
	require.NotNil(t, restoreTargets[0].RestorePhase)
	assert.Equal(t, models.TargetStateReleased, restoreTargets[0].RestorePhase.State)
	assertTimeEqual(t, restoreReleasedAt, restoreTargets[0].RestorePhase.CompletedAt)
}

func TestSQLCurtailmentStore_ListTargetsByEventPageBoundaries(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	eventUUID := uuid.New()
	targets := make([]models.InsertTargetParams, 0, 1001)
	for i := range 1001 {
		targets = append(targets, curtailmentStoreTestTarget(
			fmt.Sprintf("miner-page-%04d", i),
			models.TargetStatePending,
			models.DesiredStateCurtailed,
		))
	}
	_, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "target-pages"),
		targets,
	)
	require.NoError(t, err)

	first, nextToken, err := store.ListTargetsByEventPage(ctx, interfaces.ListTargetsByEventPageParams{
		OrgID:     user.OrganizationID,
		EventUUID: eventUUID,
		PageSize:  2,
	})
	require.NoError(t, err)
	require.Len(t, first, 2)
	assert.Equal(t, "miner-page-0000", first[0].DeviceIdentifier)
	assert.Equal(t, "miner-page-0001", first[1].DeviceIdentifier)
	require.NotEmpty(t, nextToken)

	second, _, err := store.ListTargetsByEventPage(ctx, interfaces.ListTargetsByEventPageParams{
		OrgID:     user.OrganizationID,
		EventUUID: eventUUID,
		PageSize:  2,
		PageToken: nextToken,
	})
	require.NoError(t, err)
	require.Len(t, second, 2)
	assert.Equal(t, "miner-page-0002", second[0].DeviceIdentifier)
	assert.Equal(t, "miner-page-0003", second[1].DeviceIdentifier)

	capped, cappedToken, err := store.ListTargetsByEventPage(ctx, interfaces.ListTargetsByEventPageParams{
		OrgID:     user.OrganizationID,
		EventUUID: eventUUID,
		PageSize:  5000,
	})
	require.NoError(t, err)
	assert.Len(t, capped, 1000)
	assert.NotEmpty(t, cappedToken)

	otherEventUUID := uuid.New()
	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, otherEventUUID, models.EventStateActive, "target-pages-other"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-page-other", models.TargetStatePending, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)
	_, _, err = store.ListTargetsByEventPage(ctx, interfaces.ListTargetsByEventPageParams{
		OrgID:     user.OrganizationID,
		EventUUID: otherEventUUID,
		PageSize:  2,
		PageToken: nextToken,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err), "cross-event target cursor must reject, got %v", err)

	emptyEventUUID := uuid.New()
	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, emptyEventUUID, models.EventStateCompleted, "target-pages-empty"),
		nil,
	)
	require.NoError(t, err)
	emptyTargets, emptyToken, err := store.ListTargetsByEventPage(ctx, interfaces.ListTargetsByEventPageParams{
		OrgID:     user.OrganizationID,
		EventUUID: emptyEventUUID,
		PageSize:  2,
	})
	require.NoError(t, err)
	assert.Empty(t, emptyTargets)
	assert.Empty(t, emptyToken)
	rollup, err := store.GetTargetRollupByEvent(ctx, user.OrganizationID, emptyEventUUID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), rollup.Total)
}

func assertTimeEqual(t *testing.T, expected time.Time, actual *time.Time) {
	t.Helper()
	require.NotNil(t, actual)
	assert.True(t, expected.Equal(*actual), "expected %s, got %s", expected, *actual)
}
