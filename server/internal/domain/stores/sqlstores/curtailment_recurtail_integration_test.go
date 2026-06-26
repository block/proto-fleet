package sqlstores_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	sitesmodels "github.com/block/proto-fleet/server/internal/domain/sites/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

func TestSQLCurtailmentStore_BeginRecurtailTransition_OverlapRollsBack(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	db := testContext.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(db)

	const deviceID = "miner-recurtail-overlap"
	sourceEventUUID := uuid.New()
	source, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, sourceEventUUID, models.EventStateActive, "mqtt:site-a"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget(deviceID, models.TargetStateConfirmed, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		UPDATE curtailment_event
		SET state = 'restoring'
		WHERE id = $1
	`, source.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		UPDATE curtailment_target
		SET desired_state = 'active', state = 'resolved'
		WHERE curtailment_event_id = $1 AND device_identifier = $2
	`, source.ID, deviceID)
	require.NoError(t, err)

	otherEventUUID := uuid.New()
	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, otherEventUUID, models.EventStateActive, "manual-operator"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget(deviceID, models.TargetStateConfirmed, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)

	_, err = store.BeginRecurtailTransition(ctx, user.OrganizationID, sourceEventUUID)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "overlap must be retryable, got %v", err)

	var eventState, targetDesiredState, targetState string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT state
		FROM curtailment_event
		WHERE id = $1
	`, source.ID).Scan(&eventState))
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT desired_state, state
		FROM curtailment_target
		WHERE curtailment_event_id = $1 AND device_identifier = $2
	`, source.ID, deviceID).Scan(&targetDesiredState, &targetState))

	assert.Equal(t, string(models.EventStateRestoring), eventState, "partial re-curtail must roll back the event state")
	assert.Equal(t, models.DesiredStateActive, targetDesiredState, "partial re-curtail must not reset any source targets")
	assert.Equal(t, string(models.TargetStateResolved), targetState, "partial re-curtail must not reopen skipped targets")
}

func TestSQLCurtailmentStore_ForceReleaseEvent_CancelsEventAndReleasesTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	db := testContext.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(db)

	eventUUID := uuid.New()
	inserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "force-release"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("force-release-confirmed", models.TargetStateConfirmed, models.DesiredStateCurtailed),
			curtailmentStoreTestTarget("force-release-dispatched", models.TargetStateDispatched, models.DesiredStateCurtailed),
			curtailmentStoreTestTarget("force-release-restoring", models.TargetStateDispatched, models.DesiredStateActive),
			curtailmentStoreTestTarget("force-release-resolved", models.TargetStateResolved, models.DesiredStateActive),
		},
	)
	require.NoError(t, err)

	activeBefore, err := store.ListActiveCurtailedDevices(ctx, user.OrganizationID)
	require.NoError(t, err)
	assert.Contains(t, activeBefore, "force-release-confirmed")
	assert.Contains(t, activeBefore, "force-release-dispatched")
	assert.Contains(t, activeBefore, "force-release-restoring")

	released, err := store.ForceReleaseEvent(ctx, user.OrganizationID, eventUUID, "operator needs manual control")
	require.NoError(t, err)
	event := released.Event
	require.NotNil(t, event)
	assert.Equal(t, models.EventStateCancelled, event.State)
	assert.Equal(t, int64(3), released.SweptTargets)
	assert.True(t, released.OwnershipReleased)
	assert.False(t, released.AutomationDisabled)

	targets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	got := map[string]*models.Target{}
	for _, target := range targets {
		got[target.DeviceIdentifier] = target
	}
	require.Contains(t, got, "force-release-confirmed")
	require.Contains(t, got, "force-release-dispatched")
	require.Contains(t, got, "force-release-restoring")
	require.Contains(t, got, "force-release-resolved")
	assert.Equal(t, models.TargetStateReleased, got["force-release-confirmed"].State)
	require.NotNil(t, got["force-release-confirmed"].LastError)
	assert.Equal(t, "operator needs manual control", *got["force-release-confirmed"].LastError)
	assert.Equal(t, models.TargetStateReleased, got["force-release-confirmed"].CurtailPhase.State)
	assert.NotNil(t, got["force-release-confirmed"].CurtailPhase.CompletedAt)
	assert.Equal(t, models.TargetStateReleased, got["force-release-dispatched"].State)
	assert.Equal(t, models.TargetStateReleased, got["force-release-restoring"].State)
	require.NotNil(t, got["force-release-restoring"].RestorePhase)
	assert.Equal(t, models.TargetStateReleased, got["force-release-restoring"].RestorePhase.State)
	assert.NotNil(t, got["force-release-restoring"].RestorePhase.CompletedAt)
	assert.Equal(t, models.TargetStateResolved, got["force-release-resolved"].State)

	activeAfter, err := store.ListActiveCurtailedDevices(ctx, user.OrganizationID)
	require.NoError(t, err)
	assert.NotContains(t, activeAfter, "force-release-confirmed")
	assert.NotContains(t, activeAfter, "force-release-dispatched")
	assert.NotContains(t, activeAfter, "force-release-restoring")

	// The return value should continue identifying the row the store updated.
	assert.Equal(t, inserted.ID, event.ID)
}

func TestSQLCurtailmentStore_ForceReleaseEvent_UnblocksClosedLoopFullFleetPreflight(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	device := testContext.DatabaseService.CreateDevice(user.OrganizationID, "proto")
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	eventUUID := uuid.New()
	_, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.ScopeTypeWholeOrg, 0, "force-release-full-fleet"),
		nil,
	)
	require.NoError(t, err)

	activeBefore, err := store.ListActiveCurtailedDevices(ctx, user.OrganizationID)
	require.NoError(t, err)
	assert.Contains(t, activeBefore, device.ID)

	released, err := store.ForceReleaseEvent(ctx, user.OrganizationID, eventUUID, "operator needs manual control")
	require.NoError(t, err)
	event := released.Event
	require.NotNil(t, event)
	assert.Equal(t, models.EventStateCancelled, event.State)
	assert.Zero(t, released.SweptTargets)
	assert.True(t, released.OwnershipReleased)
	assert.False(t, released.AutomationDisabled)

	activeAfter, err := store.ListActiveCurtailedDevices(ctx, user.OrganizationID)
	require.NoError(t, err)
	assert.NotContains(t, activeAfter, device.ID)
}

func TestSQLCurtailmentStore_ForceReleaseEvent_NoopsTerminalEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	eventUUID := uuid.New()
	_, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateFailed, "force-release-terminal"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("force-release-terminal", models.TargetStateRestoreFailed, models.DesiredStateActive),
		},
	)
	require.NoError(t, err)

	released, err := store.ForceReleaseEvent(ctx, user.OrganizationID, eventUUID, "operator needs manual control")
	require.NoError(t, err)
	event := released.Event
	require.NotNil(t, event)
	assert.Equal(t, models.EventStateFailed, event.State)
	assert.Zero(t, released.SweptTargets)
	assert.False(t, released.AutomationDisabled)
	assert.False(t, released.OwnershipReleased)

	current, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	require.NotNil(t, current)
	assert.Equal(t, models.EventStateFailed, current.State)
}

func TestSQLCurtailmentStore_BeginRecurtailTransition_ReopensResolvedTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	db := testContext.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(db)

	const deviceID = "miner-recurtail-resolved"
	sourceEventUUID := uuid.New()
	source, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, sourceEventUUID, models.EventStateActive, "mqtt:site-a"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget(deviceID, models.TargetStateConfirmed, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		UPDATE curtailment_event
		SET state = 'restoring'
		WHERE id = $1
	`, source.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		UPDATE curtailment_target
		SET desired_state = 'active',
		    state = 'resolved',
		    retry_count = 2,
		    last_dispatched_at = NOW(),
		    last_batch_uuid = $3,
		    confirmed_at = NOW(),
		    last_error = 'prior restore failure'
		WHERE curtailment_event_id = $1 AND device_identifier = $2
	`, source.ID, deviceID, uuid.New().String())
	require.NoError(t, err)

	got, err := store.BeginRecurtailTransition(ctx, user.OrganizationID, sourceEventUUID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, models.EventStatePending, got.State)

	var targetDesiredState, targetState string
	var retryCount int32
	var lastDispatchedAt, confirmedAt sql.NullTime
	var lastBatchUUID, lastError sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT desired_state, state, retry_count, last_dispatched_at, last_batch_uuid, confirmed_at, last_error
		FROM curtailment_target
		WHERE curtailment_event_id = $1 AND device_identifier = $2
	`, source.ID, deviceID).Scan(
		&targetDesiredState,
		&targetState,
		&retryCount,
		&lastDispatchedAt,
		&lastBatchUUID,
		&confirmedAt,
		&lastError,
	))

	assert.Equal(t, models.DesiredStateCurtailed, targetDesiredState)
	assert.Equal(t, string(models.TargetStatePending), targetState)
	assert.Equal(t, int32(0), retryCount)
	assert.False(t, lastDispatchedAt.Valid)
	assert.False(t, lastBatchUUID.Valid)
	assert.False(t, confirmedAt.Valid)
	assert.False(t, lastError.Valid)
}

func TestSQLCurtailmentStore_ClosedLoopScopeHierarchyConflicts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	db := testContext.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(db)
	siteStore := sqlstores.NewSQLSiteStore(db)

	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "site-a"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "site-b"})
	require.NoError(t, err)

	wholeOrg, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeWholeOrg, 0, "whole-org"),
		nil,
	)
	require.NoError(t, err)

	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeSite, siteA.ID, "site-a-blocked-by-org"),
		nil,
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "org watcher must block site starts, got %v", err)

	_, err = store.BeginRestoreTransition(ctx, user.OrganizationID, wholeOrg.EventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)

	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeSite, siteA.ID, "site-a"),
		nil,
	)
	require.NoError(t, err)
	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeSite, siteA.ID, "same-site-blocked"),
		nil,
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "same-site watcher must conflict, got %v", err)
	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeSite, siteB.ID, "site-b-allowed"),
		nil,
	)
	require.NoError(t, err)
	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeWholeOrg, 0, "org-blocked-by-site"),
		nil,
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "site watcher must block org starts, got %v", err)
}

func TestSQLCurtailmentStore_ClosedLoopMixedSiteScopeHierarchyConflicts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	db := testContext.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(db)
	siteStore := sqlstores.NewSQLSiteStore(db)

	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "mixed-site-a"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "mixed-site-b"})
	require.NoError(t, err)
	siteC, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "mixed-site-c"})
	require.NoError(t, err)

	mixedSites := curtailmentStoreClosedLoopFullFleetEvent(
		user.OrganizationID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeMixed,
		0,
		"mixed-sites",
	)
	mixedSites.ScopeJSON = []byte(fmt.Sprintf(
		`{"site_ids":[%d,%d],"device_identifiers":null}`,
		siteA.ID,
		siteB.ID,
	))
	_, err = store.InsertEventWithTargets(ctx, mixedSites, nil)
	require.NoError(t, err)

	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeSite, siteA.ID, "site-a-blocked-by-mixed"),
		nil,
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "mixed site watcher must block overlapping site starts, got %v", err)

	overlappingMixed := curtailmentStoreClosedLoopFullFleetEvent(
		user.OrganizationID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeMixed,
		0,
		"mixed-site-overlap",
	)
	overlappingMixed.ScopeJSON = []byte(fmt.Sprintf(
		`{"site_ids":[%d,%d],"device_identifiers":null}`,
		siteB.ID,
		siteC.ID,
	))
	_, err = store.InsertEventWithTargets(ctx, overlappingMixed, nil)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "mixed site watcher must block overlapping mixed starts, got %v", err)

	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeSite, siteC.ID, "site-c-allowed"),
		nil,
	)
	require.NoError(t, err)

	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeWholeOrg, 0, "org-blocked-by-mixed"),
		nil,
	)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err), "mixed site watcher must block org starts, got %v", err)
}

func TestSQLCurtailmentStore_ListActiveCurtailedDevicesIncludesTargetlessMixedSites(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	db := testContext.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(db)
	siteStore := sqlstores.NewSQLSiteStore(db)

	deviceIDs := testContext.DatabaseService.CreateTestMiners(user.OrganizationID, 3, "https://172.17.0.1:80")
	siteA, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "active-site-a"})
	require.NoError(t, err)
	siteB, err := siteStore.CreateSite(ctx, sitesmodels.CreateSiteParams{OrgID: user.OrganizationID, Name: "active-site-b"})
	require.NoError(t, err)

	_, err = siteStore.AssignDevicesToSite(ctx, user.OrganizationID, &siteA.ID, deviceIDs[:1])
	require.NoError(t, err)
	_, err = siteStore.AssignDevicesToSite(ctx, user.OrganizationID, &siteB.ID, deviceIDs[1:2])
	require.NoError(t, err)

	mixedSites := curtailmentStoreClosedLoopFullFleetEvent(
		user.OrganizationID,
		user.DatabaseID,
		uuid.New(),
		models.ScopeTypeMixed,
		0,
		"mixed-active-devices",
	)
	mixedSites.ScopeJSON = []byte(fmt.Sprintf(
		`{"site_ids":[%d,%d],"device_identifiers":null}`,
		siteA.ID,
		siteB.ID,
	))
	_, err = store.InsertEventWithTargets(ctx, mixedSites, nil)
	require.NoError(t, err)

	got, err := store.ListActiveCurtailedDevices(ctx, user.OrganizationID)
	require.NoError(t, err)
	assert.ElementsMatch(t, deviceIDs[:2], got)
}

func TestSQLCurtailmentStore_FixedKwDoesNotBlockClosedLoopScope(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	fixedKw := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStateActive, "fixed-kw")
	fixedKw.ScopeType = models.ScopeTypeWholeOrg
	fixedKw.ScopeJSON = []byte(`{}`)
	_, err := store.InsertEventWithTargets(ctx, fixedKw, []models.InsertTargetParams{
		curtailmentStoreTestTarget("fixed-kw-miner", models.TargetStateConfirmed, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)

	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeWholeOrg, 0, "full-fleet-after-fixed-kw"),
		nil,
	)
	require.NoError(t, err, "fixed-kW target ownership should not block a closed-loop full-fleet scope")
}

func TestSQLCurtailmentStore_ClosedLoopScopeConflictPreservesIdempotentReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	event := curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeWholeOrg, 0, "idempotent")
	idempotencyKey := "closed-loop-idempotent"
	event.IdempotencyKey = &idempotencyKey
	_, err := store.InsertEventWithTargets(ctx, event, nil)
	require.NoError(t, err)

	replay := curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeWholeOrg, 0, "idempotent-replay")
	replay.IdempotencyKey = &idempotencyKey
	_, err = store.InsertEventWithTargets(ctx, replay, nil)
	require.ErrorIs(t, err, interfaces.ErrCurtailmentReplayRaceLoss)
}

func TestSQLCurtailmentStore_ClaimClosedLoopFullFleetTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	source, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.ScopeTypeWholeOrg, 0, "claim-source"),
		nil,
	)
	require.NoError(t, err)

	claimed, err := store.ClaimClosedLoopFullFleetTargets(ctx, source.ID, user.OrganizationID, 0, []models.InsertTargetParams{
		curtailmentStoreTestTarget("claim-a", models.TargetStatePending, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget("claim-b", models.TargetStatePending, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	require.Len(t, claimed, 2)
	assert.Equal(t, models.TargetStateDispatching, claimed[0].State)
	assert.Equal(t, models.TargetStateDispatching, claimed[1].State)

	claimed, err = store.ClaimClosedLoopFullFleetTargets(ctx, source.ID, user.OrganizationID, 0, []models.InsertTargetParams{
		curtailmentStoreTestTarget("claim-a", models.TargetStatePending, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget("claim-b", models.TargetStatePending, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	assert.Empty(t, claimed, "same-event duplicates should no-op")

	other, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStateActive, "claim-other"),
		[]models.InsertTargetParams{curtailmentStoreTestTarget("claim-conflict", models.TargetStateConfirmed, models.DesiredStateCurtailed)},
	)
	require.NoError(t, err)
	require.NotZero(t, other.ID)

	claimed, err = store.ClaimClosedLoopFullFleetTargets(ctx, source.ID, user.OrganizationID, 0, []models.InsertTargetParams{
		curtailmentStoreTestTarget("claim-conflict", models.TargetStatePending, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget("claim-c", models.TargetStatePending, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.Equal(t, "claim-c", claimed[0].DeviceIdentifier)
}

func TestSQLCurtailmentStore_BeginRestoreTransition_CompletesTargetlessClosedLoopEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	eventUUID := uuid.New()
	_, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreClosedLoopFullFleetEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.ScopeTypeWholeOrg, 0, "empty-restore"),
		nil,
	)
	require.NoError(t, err)

	got, err := store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, models.EventStateCompleted, got.State)
	assert.NotNil(t, got.EndedAt)
}

func curtailmentStoreTestEvent(
	orgID int64,
	userID int64,
	eventUUID uuid.UUID,
	state models.EventState,
	sourceActorID string,
) models.InsertEventParams {
	return models.InsertEventParams{
		EventUUID:               eventUUID,
		OrgID:                   orgID,
		State:                   state,
		Mode:                    models.ModeFixedKw,
		Strategy:                models.StrategyLeastEfficientFirst,
		Level:                   models.LevelFull,
		Priority:                models.PriorityNormal,
		LoopType:                models.LoopTypeOpen,
		ScopeType:               models.ScopeTypeDeviceList,
		ScopeJSON:               []byte(`{"device_identifiers":["miner-recurtail-overlap"]}`),
		ModeParamsJSON:          []byte(`{"target_kw":1,"tolerance_kw":0}`),
		RestoreBatchSize:        10,
		RestoreBatchIntervalSec: 0,
		MinCurtailedDurationSec: 0,
		AllowUnbounded:          false,
		IncludeMaintenance:      false,
		ForceIncludeMaintenance: false,
		DecisionSnapshotJSON:    []byte(`{}`),
		SourceActorType:         models.SourceActorWebhook,
		SourceActorID:           &sourceActorID,
		Reason:                  "recurtail integration test",
		CreatedByUserID:         userID,
		EffectiveBatchSize:      10,
	}
}

func curtailmentStoreClosedLoopFullFleetEvent(
	orgID int64,
	userID int64,
	eventUUID uuid.UUID,
	scopeType models.ScopeType,
	siteID int64,
	sourceActorID string,
) models.InsertEventParams {
	scopeJSON := []byte(`{}`)
	if scopeType == models.ScopeTypeSite {
		scopeJSON = []byte(fmt.Sprintf(`{"site_id":%d}`, siteID))
	}
	startedAt := time.Now().UTC()
	return models.InsertEventParams{
		EventUUID:               eventUUID,
		OrgID:                   orgID,
		State:                   models.EventStateActive,
		Mode:                    models.ModeFullFleet,
		Strategy:                models.StrategyLeastEfficientFirst,
		Level:                   models.LevelFull,
		Priority:                models.PriorityNormal,
		LoopType:                models.LoopTypeClosed,
		ScopeType:               scopeType,
		ScopeJSON:               scopeJSON,
		ModeParamsJSON:          []byte(`{}`),
		RestoreBatchSize:        10,
		RestoreBatchIntervalSec: 0,
		MinCurtailedDurationSec: 0,
		AllowUnbounded:          false,
		IncludeMaintenance:      false,
		ForceIncludeMaintenance: false,
		DecisionSnapshotJSON:    []byte(`{}`),
		SourceActorType:         models.SourceActorWebhook,
		SourceActorID:           &sourceActorID,
		Reason:                  "closed-loop integration test",
		StartedAt:               &startedAt,
		CreatedByUserID:         userID,
		EffectiveBatchSize:      10,
	}
}

func curtailmentStoreTestTarget(deviceID string, state models.TargetState, desiredState string) models.InsertTargetParams {
	return models.InsertTargetParams{
		DeviceIdentifier: deviceID,
		TargetType:       "miner",
		State:            state,
		DesiredState:     desiredState,
	}
}
