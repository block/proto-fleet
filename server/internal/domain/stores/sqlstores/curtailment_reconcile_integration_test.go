package sqlstores_test

import (
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
	"github.com/block/proto-fleet/server/migrations"
)

// Retry history was the only send evidence before curtail_possibly_sent, so
// the upgrade keeps live events' retried targets owing a compensating Uncurtail.
func TestSQLCurtailmentStore_SendEvidenceUpgradeKeepsRetriedLiveTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	db := fixture.DatabaseService.DB
	store := sqlstores.NewSQLCurtailmentStore(db)
	insert := func(label string, devices ...string) int64 {
		t.Helper()
		targets := make([]models.InsertTargetParams, 0, len(devices))
		for _, device := range devices {
			targets = append(targets, curtailmentStoreTestTarget(device, models.TargetStatePending, models.DesiredStateCurtailed))
		}
		inserted, err := store.InsertEventWithTargets(ctx, curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStateActive, label), targets)
		require.NoError(t, err)
		return inserted.ID
	}
	insert("upgrade-live", "upgrade-retried", "upgrade-curtail-retried", "upgrade-fresh")
	ended := insert("upgrade-ended", "upgrade-ended-retried")
	for _, statement := range []string{
		`UPDATE curtailment_target SET retry_count = 2 WHERE device_identifier IN ('upgrade-retried', 'upgrade-ended-retried')`,
		`UPDATE curtailment_target SET curtail_retry_count = 1 WHERE device_identifier = 'upgrade-curtail-retried'`,
	} {
		_, err := db.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	_, err := db.ExecContext(ctx, `UPDATE curtailment_event SET state = 'completed', ended_at = now() WHERE id = $1`, ended)
	require.NoError(t, err)

	for _, name := range []string{
		"current/001058_internal_curtailment_wave4_runtime.down.sql",
		"current/001057_shared_curtailment_wave4_runtime.down.sql",
		"current/001057_shared_curtailment_wave4_runtime.up.sql",
		"current/001058_internal_curtailment_wave4_runtime.up.sql",
	} {
		migration, err := migrations.Current.ReadFile(name)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err, name)
	}

	possiblySent := func(device string) bool {
		t.Helper()
		var sent bool
		require.NoError(t, db.QueryRowContext(ctx, `SELECT curtail_possibly_sent FROM curtailment_target WHERE device_identifier = $1`, device).Scan(&sent))
		return sent
	}
	assert.True(t, possiblySent("upgrade-retried"), "a live target with retry history may have received a Curtail")
	assert.True(t, possiblySent("upgrade-curtail-retried"))
	assert.False(t, possiblySent("upgrade-fresh"), "a live target without retries was never sent")
	assert.False(t, possiblySent("upgrade-ended-retried"), "ended events are not restored again")
}

func TestSQLCurtailmentStore_ReconcileObservationsGuardStateAndSurviveStop(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
	first := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	second := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	eventUUID := uuid.New()
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStateActive, "observations")
	params.ScopeType = models.ScopeTypeWholeOrg
	params.ScopeJSON = []byte(`{}`)
	inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{
		curtailmentStoreTestTarget(first, models.TargetStateConfirmed, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget(second, models.TargetStateDispatched, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	event, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	now := time.Now().UTC()
	observations := []interfaces.CurtailmentTargetObservation{
		{DeviceIdentifier: first, State: models.TargetStateDrifted, ObservedAt: now},
		{DeviceIdentifier: second, State: models.TargetStateConfirmed, ObservedAt: now},
	}
	oldBatch := "superseded-batch"
	observations[0].ExpectedBatchUUID = &oldBatch
	applied, err := store.ObserveTargets(ctx, event, observations)
	require.NoError(t, err)
	assert.Empty(t, applied, "a stale observation cannot clobber a replacement dispatch batch")
	observations[0].ExpectedBatchUUID = nil
	applied, err = store.ObserveTargets(ctx, event, observations)
	require.NoError(t, err)
	assert.Equal(t, []string{first}, applied, "only a still-confirmed target can accept drift observations")
	require.NoError(t, store.RejectDispatch(ctx, user.OrganizationID, inserted.ID, "permission revoked"))
	restarted := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
	rejected, err := restarted.GetDispatchRejection(ctx, user.OrganizationID, inserted.ID)
	require.NoError(t, err)
	require.NotNil(t, rejected)
	assert.Equal(t, "permission revoked", *rejected)
	for _, dispatchFence := range []func() error{
		func() error {
			return store.WithCurtailmentDispatchFence(ctx, event, interfaces.ListCandidatesParams{}, []string{first}, func(interfaces.CurtailmentTopologyDispatchFenceSnapshot) error {
				t.Error("durably rejected event dispatched")
				return nil
			})
		},
		func() error {
			return store.WithCurtailmentTopologyDispatchFence(ctx, event, interfaces.ListCandidatesParams{BuildingIDs: []int64{999}}, []string{first}, func(interfaces.CurtailmentTopologyDispatchFenceSnapshot) error {
				t.Error("durably rejected topology event dispatched")
				return nil
			})
		},
	} {
		require.Error(t, dispatchFence(), "the locked event must recheck a rejection that raced the initial latch read")
	}
	_, err = store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	applied, err = store.ObserveTargets(ctx, event, observations)
	require.NoError(t, err)
	assert.Empty(t, applied, "a stale active tick cannot write after Stop")
	targets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	for _, target := range targets {
		assert.Equal(t, models.DesiredStateActive, target.DesiredState)
		assert.Equal(t, models.TargetStatePending, target.State)
	}
}

func TestSQLCurtailmentStore_GivenUpCurtailStillReceivesCompensatingRestore(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
	device := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	eventUUID := uuid.New()
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStatePending, "give-up")
	params.ScopeType = models.ScopeTypeWholeOrg
	params.ScopeJSON = []byte(`{}`)
	inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{curtailmentStoreTestTarget(device, models.TargetStatePending, models.DesiredStateCurtailed)})
	require.NoError(t, err)
	now := time.Now().UTC()
	batch := "curtail-attempt"
	require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, device, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateDispatched, LastDispatchedAt: &now, LastBatchUUID: &batch}))
	failure := "confirmation budget exhausted"
	require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, device, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateRestoreFailed, LastError: &failure}))
	_, err = store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	targets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, models.TargetStatePending, targets[0].State)
	assert.Equal(t, models.DesiredStateActive, targets[0].DesiredState)
	assert.Equal(t, models.TargetStateRestoreFailed, targets[0].CurtailPhase.State, "the failed curtail remains in phase history")
}

func TestSQLCurtailmentStore_LatchedDispatchRejectionRefusesRecurtail(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
	device := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	eventUUID := uuid.New()
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStatePending, "latched-recurtail")
	params.ScopeType = models.ScopeTypeWholeOrg
	params.ScopeJSON = []byte(`{}`)
	inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{curtailmentStoreTestTarget(device, models.TargetStateConfirmed, models.DesiredStateCurtailed)})
	require.NoError(t, err)
	require.NoError(t, store.RejectDispatch(ctx, user.OrganizationID, inserted.ID, "permission revoked"))
	_, err = store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)

	_, err = store.BeginRecurtailTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRecurtailTransitionParams{})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err), "%v", err)
	event, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	assert.Equal(t, models.EventStateRestoring, event.State, "a latched event finishes restoring instead of re-entering dispatch")
}

// Retry history alone does not prove a Curtail reached the miner: admission
// skips and unenqueued devices consume retries without sending anything.
func TestSQLCurtailmentStore_RestoreOwesUncurtailOnlyForPossibleSends(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
	unsentPending := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	sentPending := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	unsentFailed := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	sentFailed := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	fallback := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	eventUUID := uuid.New()
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStatePending, "possible-sends")
	params.ScopeType = models.ScopeTypeWholeOrg
	params.ScopeJSON = []byte(`{}`)
	inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{
		curtailmentStoreTestTarget(unsentPending, models.TargetStatePending, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget(sentPending, models.TargetStatePending, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget(unsentFailed, models.TargetStatePending, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget(sentFailed, models.TargetStatePending, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget(fallback, models.TargetStatePending, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	skip := "device lacks curtail capability"
	enqueueErr := "command queue unavailable"
	retries := int32(2)
	exhausted := int32(3)
	for _, update := range []struct {
		device       string
		state        models.TargetState
		reason       string
		retry        int32
		possiblySent bool
	}{
		{unsentPending, models.TargetStatePending, skip, retries, false},
		{sentPending, models.TargetStatePending, enqueueErr, retries, true},
		{unsentFailed, models.TargetStateRestoreFailed, skip, exhausted, false},
		{sentFailed, models.TargetStateRestoreFailed, enqueueErr, exhausted, true},
	} {
		require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, update.device, interfaces.UpdateCurtailmentTargetStateParams{
			State: update.state, LastError: &update.reason, RetryCount: &update.retry, CurtailPossiblySent: update.possiblySent,
		}))
	}
	// A later known-unsent failure never clears earlier send evidence, and the
	// retry fallback has lost its classification, so it records a possible send.
	require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, sentPending, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStatePending, LastError: &skip, RetryCount: &exhausted}))
	require.NoError(t, store.BumpTargetRetry(ctx, inserted.ID, fallback))

	_, err = store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	targets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	byDevice := map[string]*models.Target{}
	for _, target := range targets {
		byDevice[target.DeviceIdentifier] = target
	}
	assert.Equal(t, models.TargetStateReleased, byDevice[unsentPending].State, "retries from admission skips release without Uncurtail")
	assert.Equal(t, models.DesiredStateCurtailed, byDevice[unsentPending].DesiredState)
	assert.Equal(t, models.TargetStateRestoreFailed, byDevice[unsentFailed].State, "a known-unsent give-up stays a terminal curtail failure")
	assert.Equal(t, models.DesiredStateCurtailed, byDevice[unsentFailed].DesiredState)
	for _, device := range []string{sentPending, sentFailed, fallback} {
		assert.Equal(t, models.TargetStatePending, byDevice[device].State, "%s: a possible send owes a compensating restore", device)
		assert.Equal(t, models.DesiredStateActive, byDevice[device].DesiredState, device)
		assert.True(t, byDevice[device].CurtailPossiblySent, device)
	}
}

func TestSQLCurtailmentStore_PendingDeadlineCanLeaveInFlightTargetsForActivePhase(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
	confirmed := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	stalled := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	inFlight := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	eventUUID := uuid.New()
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStatePending, "deadline-keeps-curtailed")
	params.ScopeType = models.ScopeTypeWholeOrg
	params.ScopeJSON = []byte(`{}`)
	inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{
		curtailmentStoreTestTarget(confirmed, models.TargetStateConfirmed, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget(stalled, models.TargetStateUnavailable, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget(inFlight, models.TargetStatePending, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	batch := "in-flight"
	require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, inFlight, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateDispatched, LastDispatchedAt: &now, LastBatchUUID: &batch}))
	event, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)

	require.NoError(t, store.FailPendingCurtailTargets(ctx, event, "pending readiness deadline", false))
	targets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	byDevice := map[string]*models.Target{}
	for _, target := range targets {
		byDevice[target.DeviceIdentifier] = target
	}
	assert.Equal(t, models.TargetStateConfirmed, byDevice[confirmed].State)
	assert.Equal(t, models.TargetStateRestoreFailed, byDevice[stalled].State)
	assert.Equal(t, models.TargetStateDispatched, byDevice[inFlight].State)
	assert.Equal(t, models.TargetStateDispatched, byDevice[inFlight].CurtailPhase.State, "the in-flight curtail phase can still confirm")
}

// A miner whose curtail gave up is no longer owned by its event, so another
// event may claim it. Restoring the first event must leave that miner to its
// new owner and still restore the rest.
func TestSQLCurtailmentStore_CompensatingRestoreSkipsMinerClaimedByAnotherEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
	contested := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	uncontested := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	firstUUID := uuid.New()
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, firstUUID, models.EventStatePending, "first-owner")
	params.ScopeType = models.ScopeTypeWholeOrg
	params.ScopeJSON = []byte(`{}`)
	first, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{
		curtailmentStoreTestTarget(contested, models.TargetStatePending, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget(uncontested, models.TargetStatePending, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	batch := "curtail-attempt"
	failure := "confirmation budget exhausted"
	for _, device := range []string{contested, uncontested} {
		require.NoError(t, store.UpdateTargetState(ctx, first.ID, device, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateDispatched, LastDispatchedAt: &now, LastBatchUUID: &batch}))
	}
	require.NoError(t, store.UpdateTargetState(ctx, first.ID, contested, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateRestoreFailed, LastError: &failure}))

	secondUUID := uuid.New()
	secondParams := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, secondUUID, models.EventStatePending, "second-owner")
	secondParams.ScopeType = models.ScopeTypeWholeOrg
	secondParams.ScopeJSON = []byte(`{}`)
	_, err = store.InsertEventWithTargets(ctx, secondParams, []models.InsertTargetParams{curtailmentStoreTestTarget(contested, models.TargetStatePending, models.DesiredStateCurtailed)})
	require.NoError(t, err)

	_, err = store.BeginRestoreTransition(ctx, user.OrganizationID, firstUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err, "a claimed miner must not block restoring the rest of the event")
	targets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, firstUUID)
	require.NoError(t, err)
	byDevice := map[string]*models.Target{}
	for _, target := range targets {
		byDevice[target.DeviceIdentifier] = target
	}
	assert.Equal(t, models.TargetStateRestoreFailed, byDevice[contested].State, "the new owner keeps the claimed miner")
	assert.Equal(t, models.DesiredStateCurtailed, byDevice[contested].DesiredState)
	assert.Equal(t, models.TargetStatePending, byDevice[uncontested].State)
	assert.Equal(t, models.DesiredStateActive, byDevice[uncontested].DesiredState)
	secondTargets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, secondUUID)
	require.NoError(t, err)
	require.Len(t, secondTargets, 1)
	assert.Equal(t, models.DesiredStateCurtailed, secondTargets[0].DesiredState)
}

func TestSQLCurtailmentStore_PendingDeadlinePreservesAmbiguousIntentsAndBlocksStaleConfirmation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres")
	}
	fixture := testutil.InitializeDBServiceInfrastructure(t)
	user := fixture.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(fixture.DatabaseService.DB)
	first := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	second := fixture.DatabaseService.CreateDevice(user.OrganizationID, "proto").ID
	eventUUID := uuid.New()
	params := curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, eventUUID, models.EventStatePending, "deadline-intents")
	params.ScopeType = models.ScopeTypeWholeOrg
	params.ScopeJSON = []byte(`{}`)
	inserted, err := store.InsertEventWithTargets(ctx, params, []models.InsertTargetParams{
		curtailmentStoreTestTarget(first, models.TargetStatePending, models.DesiredStateCurtailed),
		curtailmentStoreTestTarget(second, models.TargetStateDispatching, models.DesiredStateCurtailed),
	})
	require.NoError(t, err)
	now := time.Now().UTC()
	batch := "before-deadline"
	require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, first, interfaces.UpdateCurtailmentTargetStateParams{State: models.TargetStateDispatched, LastDispatchedAt: &now, LastBatchUUID: &batch}))
	eligible, err := store.ListEligibleConfirmationTargets(ctx, interfaces.ConfirmationPageCursor{})
	require.NoError(t, err)
	var deviceDatabaseID int64
	for _, row := range eligible {
		if row.EventID == inserted.ID && row.DeviceIdentifier == first {
			deviceDatabaseID = row.DeviceDatabaseID
		}
	}
	require.NotZero(t, deviceDatabaseID)
	event, err := store.GetEventByUUID(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	require.NoError(t, store.FailPendingCurtailTargets(ctx, event, "pending readiness deadline", true))
	result, err := store.BulkConfirmTargets(ctx, inserted.ID, models.EventStatePending, []interfaces.ConfirmationUpdate{{DeviceIdentifier: first, DeviceDatabaseID: deviceDatabaseID, Phase: models.TargetPhaseCurtail, BatchUUID: batch, ObservedAt: now, ConfirmedAt: now}})
	require.NoError(t, err)
	assert.Zero(t, result.AppliedCount, "a pulse sampled before the deadline cannot resurrect its failed curtail phase")
	_, err = store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	targets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, eventUUID)
	require.NoError(t, err)
	require.Len(t, targets, 2)
	for _, target := range targets {
		assert.Equal(t, models.TargetStatePending, target.State)
		assert.Equal(t, models.DesiredStateActive, target.DesiredState)
		assert.Equal(t, models.TargetStateRestoreFailed, target.CurtailPhase.State)
	}
}
