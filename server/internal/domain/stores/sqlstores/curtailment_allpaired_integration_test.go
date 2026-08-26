package sqlstores_test

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// curtailmentStoreAllPairedEvent is the closed-loop full-fleet fixture with
// the all-paired policy flag stamped, matching what Service.Start persists.
func curtailmentStoreAllPairedEvent(orgID, userID int64, eventUUID uuid.UUID, sourceActorID string) models.InsertEventParams {
	params := curtailmentStoreClosedLoopFullFleetEvent(orgID, userID, eventUUID, models.ScopeTypeWholeOrg, 0, sourceActorID)
	params.ForceIncludeAllPairedMiners = true
	return params
}

func curtailmentStoreAllPairedTarget(deviceID string, state models.TargetState, lastError string) models.InsertTargetParams {
	target := curtailmentStoreTestTarget(deviceID, state, models.DesiredStateCurtailed)
	if lastError != "" {
		target.LastError = &lastError
	}
	return target
}

// Pins ClaimAllPairedPolicyTargets' real SQL semantics: brand-new rows insert
// in their computed policy state, same-event RELEASED rows reopen with phase
// cursors reset, and devices owned by another non-terminal event are no-ops
// (the cross-event NOT EXISTS guard). The Go fakes used by reconciler tests
// reimplement these rules, so only this test catches a broken WHERE clause.
func TestSQLCurtailmentStore_ClaimAllPairedPolicyTargets_InsertsReopensAndSkipsCrossEventOwned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	db := testContext.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(db)

	policyEventUUID := uuid.New()
	policyEvent, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreAllPairedEvent(user.OrganizationID, user.DatabaseID, policyEventUUID, "all-paired-claim"),
		[]models.InsertTargetParams{
			curtailmentStoreAllPairedTarget("ap-claim-released", models.TargetStateReleased, "released without restore: no curtail command dispatched"),
		},
	)
	require.NoError(t, err)

	otherEventUUID := uuid.New()
	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, otherEventUUID, models.EventStateActive, "all-paired-claim-other"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("ap-claim-owned-elsewhere", models.TargetStateConfirmed, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)

	claimed, err := store.ClaimAllPairedPolicyTargets(ctx, policyEvent.ID, user.OrganizationID, 100, []models.InsertTargetParams{
		curtailmentStoreAllPairedTarget("ap-claim-new-pending", models.TargetStatePending, ""),
		curtailmentStoreAllPairedTarget("ap-claim-new-unavailable", models.TargetStateUnavailable, "offline"),
		curtailmentStoreAllPairedTarget("ap-claim-released", models.TargetStatePending, ""),
		curtailmentStoreAllPairedTarget("ap-claim-owned-elsewhere", models.TargetStatePending, ""),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), claimed, "two inserts + one reopen; the cross-event-owned device is a no-op")

	targets, err := store.ListTargetsByEvent(ctx, user.OrganizationID, policyEventUUID)
	require.NoError(t, err)
	byDevice := map[string]*models.Target{}
	for _, target := range targets {
		byDevice[target.DeviceIdentifier] = target
	}
	require.Len(t, byDevice, 3, "the cross-event-owned device must not gain a policy row")
	require.NotContains(t, byDevice, "ap-claim-owned-elsewhere")

	require.Contains(t, byDevice, "ap-claim-new-pending")
	assert.Equal(t, models.TargetStatePending, byDevice["ap-claim-new-pending"].State)

	require.Contains(t, byDevice, "ap-claim-new-unavailable")
	assert.Equal(t, models.TargetStateUnavailable, byDevice["ap-claim-new-unavailable"].State)
	require.NotNil(t, byDevice["ap-claim-new-unavailable"].LastError)
	assert.Equal(t, "offline", *byDevice["ap-claim-new-unavailable"].LastError)

	reopened := byDevice["ap-claim-released"]
	require.NotNil(t, reopened)
	assert.Equal(t, models.TargetStatePending, reopened.State, "same-event released rows reopen")
	assert.Nil(t, reopened.ReleasedAt)
	assert.Nil(t, reopened.LastDispatchedAt)
	assert.Equal(t, int32(0), reopened.RetryCount)
	assert.Equal(t, models.TargetStatePending, reopened.CurtailPhase.State)

	// Reopening while another event owns the device must also be a no-op:
	// release the policy row, hand the device to the other event, re-claim.
	_, err = db.ExecContext(ctx, `
		UPDATE curtailment_target
		SET state = 'released', released_at = CURRENT_TIMESTAMP
		WHERE curtailment_event_id = $1 AND device_identifier = 'ap-claim-new-pending'
	`, policyEvent.ID)
	require.NoError(t, err)
	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStateActive, "all-paired-claim-competitor"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("ap-claim-new-pending", models.TargetStateConfirmed, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)

	claimed, err = store.ClaimAllPairedPolicyTargets(ctx, policyEvent.ID, user.OrganizationID, 100, []models.InsertTargetParams{
		curtailmentStoreAllPairedTarget("ap-claim-new-pending", models.TargetStatePending, ""),
	})
	require.NoError(t, err)
	assert.Zero(t, claimed, "released rows must not reopen while another event owns the device")

	var state string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT state FROM curtailment_target
		WHERE curtailment_event_id = $1 AND device_identifier = 'ap-claim-new-pending'
	`, policyEvent.ID).Scan(&state))
	assert.Equal(t, string(models.TargetStateReleased), state)

	claimed, err = store.ClaimAllPairedPolicyTargets(ctx, policyEvent.ID, user.OrganizationID, 2, []models.InsertTargetParams{
		curtailmentStoreAllPairedTarget("ap-batch-first", models.TargetStatePending, ""),
		curtailmentStoreAllPairedTarget("ap-batch-second", models.TargetStatePending, ""),
		curtailmentStoreAllPairedTarget("ap-batch-deferred", models.TargetStatePending, ""),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), claimed, "one admission tick must not exceed its configured batch size")
	var deferredCount int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM curtailment_target
		WHERE curtailment_event_id = $1
		  AND device_identifier = 'ap-batch-deferred'
	`, policyEvent.ID).Scan(&deferredCount))
	assert.Zero(t, deferredCount)
}

// Pins the ownership-suppression semantics of ListActiveCurtailedDevices for
// all-paired events: the scope watcher keeps devices locked before their
// policy row exists (miners that became paired-like between admission ticks
// must not be claimable by other selectors), concrete non-terminal rows lock
// as usual, and RELEASED policy rows stay suppressed while the event is
// pending/active (the admission pass will reopen them — exempting them would
// let a regular start claim a re-paired miner in the gap before the next
// reopen). Only during the restoring wind-down, when reopen is impossible,
// does a released row free its device.
func TestSQLCurtailmentStore_ListActiveCurtailedDevices_AllPairedScopeLockAndReleasedRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	db := testContext.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(db)

	deviceIDs := testContext.DatabaseService.CreateTestMiners(user.OrganizationID, 3, "https://172.17.0.1:80")
	unclaimed, released, unavailable := deviceIDs[0], deviceIDs[1], deviceIDs[2]

	inserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreAllPairedEvent(user.OrganizationID, user.DatabaseID, uuid.New(), "all-paired-scope-lock"),
		[]models.InsertTargetParams{
			curtailmentStoreAllPairedTarget(released, models.TargetStateReleased, "released: device is no longer paired-like"),
			curtailmentStoreAllPairedTarget(unavailable, models.TargetStateUnavailable, "offline"),
		},
	)
	require.NoError(t, err)

	got, err := store.ListActiveCurtailedDevices(ctx, user.OrganizationID)
	require.NoError(t, err)
	assert.Contains(t, got, unclaimed, "scope lock must hold for in-scope miners without a policy row yet")
	assert.Contains(t, got, unavailable, "non-terminal policy rows lock their device")
	assert.Contains(t, got, released,
		"released rows stay suppressed while active: admission reopens them, so the device must not be claimable in the gap")

	_, err = db.ExecContext(ctx, `UPDATE curtailment_event SET state = 'restoring' WHERE id = $1`, inserted.ID)
	require.NoError(t, err)

	got, err = store.ListActiveCurtailedDevices(ctx, user.OrganizationID)
	require.NoError(t, err)
	assert.Contains(t, got, unclaimed, "the scope lock holds through the restoring wind-down")
	assert.Contains(t, got, unavailable)
	assert.NotContains(t, got, released,
		"during restoring, reopen is impossible: released rows free their device instead of holding it until terminal")

	_, err = store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(user.OrganizationID, user.DatabaseID, uuid.New(), models.EventStateActive, "released-policy-row-competitor"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget(released, models.TargetStateConfirmed, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err,
		"the transactional reservation check must apply the same restoring/released exception as the selector")
}

// Pins BulkRefreshAllPairedTargetReadiness' real SQL: curtail and topology
// restore readiness flips share one guarded batch, rows that advanced past
// refreshable states are skipped, a stale event state applies nothing, and
// curtail promotion baselines backfill NULL only.
func TestSQLCurtailmentStore_BulkRefreshAllPairedTargetReadiness_FlipsAndSkips(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	db := testContext.DatabaseService.DB
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(db)

	existingBaseline := 2500.0
	seededBaselineTarget := curtailmentStoreAllPairedTarget("bulk-keeps-baseline", models.TargetStateUnavailable, "offline")
	seededBaselineTarget.BaselinePowerW = &existingBaseline
	restoreFailedTarget := curtailmentStoreAllPairedTarget("bulk-parks-restore-failure", models.TargetStateRestoreFailed, "restore dispatch failed")
	restoreFailedTarget.DesiredState = models.DesiredStateActive
	restoreUnavailableTarget := curtailmentStoreAllPairedTarget("bulk-requeues-restore", models.TargetStateUnavailable, "unpaired")
	restoreUnavailableTarget.DesiredState = models.DesiredStateActive
	staleRestoreTarget := curtailmentStoreAllPairedTarget("bulk-skips-stale-restore", models.TargetStateRestoreFailed, "restore dispatch failed")
	staleRestoreTarget.DesiredState = models.DesiredStateActive
	staleCurtailDirection := curtailmentStoreAllPairedTarget("bulk-skips-active-direction", models.TargetStateUnavailable, "unpaired")
	staleCurtailDirection.DesiredState = models.DesiredStateActive

	eventUUID := uuid.New()
	inserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreAllPairedEvent(user.OrganizationID, user.DatabaseID, eventUUID, "all-paired-bulk-refresh"),
		[]models.InsertTargetParams{
			curtailmentStoreAllPairedTarget("bulk-promotes", models.TargetStateUnavailable, "offline"),
			curtailmentStoreAllPairedTarget("bulk-demotes", models.TargetStatePending, ""),
			curtailmentStoreAllPairedTarget("bulk-dispatched", models.TargetStateDispatched, ""),
			seededBaselineTarget,
			restoreFailedTarget,
			restoreUnavailableTarget,
			staleRestoreTarget,
			staleCurtailDirection,
			curtailmentStoreAllPairedTarget("bulk-skips-curtailed-direction", models.TargetStatePending, ""),
		},
	)
	require.NoError(t, err)

	promotionBaseline := 3000.0
	overwriteAttempt := 9999.0

	applied, err := store.BulkRefreshAllPairedTargetReadiness(ctx, inserted.ID, user.OrganizationID, models.EventStatePending,
		[]interfaces.AllPairedReadinessUpdate{
			{DeviceIdentifier: "bulk-promotes", ExpectedState: models.TargetStateUnavailable, ExpectedDesiredState: models.DesiredStateCurtailed, State: models.TargetStatePending},
		})
	require.NoError(t, err)
	assert.Empty(t, applied, "stale expected event state must apply nothing")

	applied, err = store.BulkRefreshAllPairedTargetReadiness(ctx, inserted.ID, user.OrganizationID, models.EventStateActive,
		[]interfaces.AllPairedReadinessUpdate{
			{DeviceIdentifier: "bulk-promotes", ExpectedState: models.TargetStateUnavailable, ExpectedDesiredState: models.DesiredStateCurtailed, State: models.TargetStatePending, BaselinePowerW: &promotionBaseline},
			{DeviceIdentifier: "bulk-demotes", ExpectedState: models.TargetStatePending, ExpectedDesiredState: models.DesiredStateCurtailed, State: models.TargetStateUnavailable, Reason: "offline"},
			{DeviceIdentifier: "bulk-dispatched", ExpectedState: models.TargetStateDispatched, ExpectedDesiredState: models.DesiredStateCurtailed, State: models.TargetStateUnavailable, Reason: "offline"},
			{DeviceIdentifier: "bulk-keeps-baseline", ExpectedState: models.TargetStateUnavailable, ExpectedDesiredState: models.DesiredStateCurtailed, State: models.TargetStatePending, BaselinePowerW: &overwriteAttempt},
			{DeviceIdentifier: "bulk-parks-restore-failure", ExpectedState: models.TargetStateRestoreFailed, ExpectedDesiredState: models.DesiredStateActive, State: models.TargetStateUnavailable, Reason: "unpaired"},
			{DeviceIdentifier: "bulk-requeues-restore", ExpectedState: models.TargetStateUnavailable, ExpectedDesiredState: models.DesiredStateActive, State: models.TargetStatePending},
			{DeviceIdentifier: "bulk-skips-stale-restore", ExpectedState: models.TargetStateUnavailable, ExpectedDesiredState: models.DesiredStateActive, State: models.TargetStatePending},
			{DeviceIdentifier: "bulk-skips-active-direction", ExpectedState: models.TargetStateUnavailable, ExpectedDesiredState: models.DesiredStateCurtailed, State: models.TargetStatePending},
			{DeviceIdentifier: "bulk-skips-curtailed-direction", ExpectedState: models.TargetStatePending, ExpectedDesiredState: models.DesiredStateActive, State: models.TargetStateUnavailable, Reason: "unpaired"},
		})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"bulk-promotes", "bulk-demotes", "bulk-keeps-baseline",
		"bulk-parks-restore-failure", "bulk-requeues-restore",
	}, applied,
		"the dispatched row is past the refreshable states and must be skipped — and not reported as applied")

	rows, err := db.QueryContext(ctx, `
		SELECT device_identifier, state, last_error, curtail_state, curtail_failure_count,
		       baseline_power_w, restore_state, restore_completed_at, restore_retry_count
		FROM curtailment_target
		WHERE curtailment_event_id = $1
	`, inserted.ID)
	require.NoError(t, err)
	defer rows.Close()

	type refreshedRow struct {
		state               string
		lastError           sql.NullString
		curtailState        sql.NullString
		curtailFailureCount int32
		baselinePowerW      sql.NullFloat64
		restoreState        sql.NullString
		restoreCompletedAt  sql.NullTime
		restoreRetryCount   int32
	}
	got := map[string]refreshedRow{}
	for rows.Next() {
		var device string
		var row refreshedRow
		require.NoError(t, rows.Scan(
			&device, &row.state, &row.lastError, &row.curtailState, &row.curtailFailureCount,
			&row.baselinePowerW, &row.restoreState, &row.restoreCompletedAt, &row.restoreRetryCount,
		))
		got[device] = row
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 9)

	promoted := got["bulk-promotes"]
	assert.Equal(t, string(models.TargetStatePending), promoted.state)
	assert.False(t, promoted.lastError.Valid, "pending promotion clears last_error")
	assert.Equal(t, string(models.TargetStatePending), promoted.curtailState.String)
	assert.Equal(t, int32(0), promoted.curtailFailureCount, "clearing the error must not count as a failure")
	require.True(t, promoted.baselinePowerW.Valid, "promotion backfills the missing pre-curtail baseline")
	assert.InDelta(t, promotionBaseline, promoted.baselinePowerW.Float64, 0.001)

	demoted := got["bulk-demotes"]
	assert.Equal(t, string(models.TargetStateUnavailable), demoted.state)
	require.True(t, demoted.lastError.Valid)
	assert.Equal(t, "offline", demoted.lastError.String)
	assert.Equal(t, string(models.TargetStateUnavailable), demoted.curtailState.String)
	assert.Equal(t, int32(1), demoted.curtailFailureCount, "demotion reasons count as curtail-phase failures, matching the per-row query")

	dispatched := got["bulk-dispatched"]
	assert.Equal(t, string(models.TargetStateDispatched), dispatched.state, "rows past pending/unavailable are skipped, not clobbered")
	assert.False(t, dispatched.lastError.Valid)

	keepsBaseline := got["bulk-keeps-baseline"]
	assert.Equal(t, string(models.TargetStatePending), keepsBaseline.state)
	require.True(t, keepsBaseline.baselinePowerW.Valid)
	assert.InDelta(t, existingBaseline, keepsBaseline.baselinePowerW.Float64, 0.001,
		"an existing pre-curtail baseline is never overwritten by promotion telemetry")

	parkedRestore := got["bulk-parks-restore-failure"]
	assert.Equal(t, string(models.TargetStateUnavailable), parkedRestore.state)
	assert.Equal(t, string(models.TargetStateUnavailable), parkedRestore.restoreState.String)
	assert.False(t, parkedRestore.restoreCompletedAt.Valid, "parking reopens the restore obligation")

	requeuedRestore := got["bulk-requeues-restore"]
	assert.Equal(t, string(models.TargetStatePending), requeuedRestore.state)
	assert.False(t, requeuedRestore.lastError.Valid)
	assert.Equal(t, string(models.TargetStatePending), requeuedRestore.restoreState.String)
	assert.False(t, requeuedRestore.restoreCompletedAt.Valid)
	assert.Zero(t, requeuedRestore.restoreRetryCount, "re-pairing grants a fresh bounded restore attempt")

	staleRestore := got["bulk-skips-stale-restore"]
	assert.Equal(t, string(models.TargetStateRestoreFailed), staleRestore.state)
	assert.Equal(t, "restore dispatch failed", staleRestore.lastError.String)
	assert.Equal(t, string(models.TargetStateUnavailable), got["bulk-skips-active-direction"].state,
		"a curtail-phase decision must not overwrite a target that entered restore")
	assert.Equal(t, string(models.TargetStatePending), got["bulk-skips-curtailed-direction"].state,
		"a restore-phase decision must not overwrite a target that re-entered curtail")
}

// Pins the graceful-Stop release predicate against real SQL: only all-paired
// targets with no dispatch attempt at all (NULL dispatch timestamps,
// retry_count = 0, and no prior restore cycle) are released; anything with
// attempt history routes through the restore reset instead.
// curtail_failure_count is deliberately ignored — readiness flaps inflate it
// without a command ever being sent. restore_started_at guards the
// Stop -> Recurtail -> Stop cascade: the recurtail reset wipes retry_count and
// dispatch timestamps, so the surviving restore stamp is the only evidence a
// row was ever actually dispatched.
func TestSQLCurtailmentStore_BeginRestoreTransition_ReleasesOnlyNeverAttemptedAllPairedTargets(t *testing.T) {
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
		curtailmentStoreAllPairedEvent(user.OrganizationID, user.DatabaseID, eventUUID, "all-paired-restore"),
		[]models.InsertTargetParams{
			curtailmentStoreAllPairedTarget("ap-restore-never-pending", models.TargetStatePending, ""),
			curtailmentStoreAllPairedTarget("ap-restore-never-unavailable", models.TargetStateUnavailable, "offline"),
			curtailmentStoreAllPairedTarget("ap-restore-flapped", models.TargetStateUnavailable, "offline"),
			curtailmentStoreAllPairedTarget("ap-restore-attempted", models.TargetStatePending, "curtail batch dispatch failed"),
			curtailmentStoreAllPairedTarget("ap-restore-dispatched", models.TargetStateDispatched, ""),
			curtailmentStoreAllPairedTarget("ap-restore-recurtailed", models.TargetStatePending, ""),
		},
	)
	require.NoError(t, err)

	// Flap history: readiness churn bumped curtail_failure_count without any
	// dispatch. Attempted: a failed dispatch bumped retry_count. Dispatched:
	// a successful enqueue stamped last_dispatched_at.
	_, err = db.ExecContext(ctx, `
		UPDATE curtailment_target SET curtail_failure_count = 2
		WHERE curtailment_event_id = $1 AND device_identifier = 'ap-restore-flapped'
	`, inserted.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		UPDATE curtailment_target SET retry_count = 1, curtail_failure_count = 1
		WHERE curtailment_event_id = $1 AND device_identifier = 'ap-restore-attempted'
	`, inserted.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		UPDATE curtailment_target SET last_dispatched_at = CURRENT_TIMESTAMP, curtail_dispatched_at = CURRENT_TIMESTAMP
		WHERE curtailment_event_id = $1 AND device_identifier = 'ap-restore-dispatched'
	`, inserted.ID)
	require.NoError(t, err)
	// Recurtailed: a prior Stop stamped restore_started_at, then the
	// recurtail reset wiped retry_count and both dispatch timestamps —
	// the exact never-attempted signature. Only the restore stamp
	// distinguishes it from a genuinely never-dispatched row.
	_, err = db.ExecContext(ctx, `
		UPDATE curtailment_target SET restore_started_at = CURRENT_TIMESTAMP
		WHERE curtailment_event_id = $1 AND device_identifier = 'ap-restore-recurtailed'
	`, inserted.ID)
	require.NoError(t, err)

	event, err := store.BeginRestoreTransition(ctx, user.OrganizationID, eventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	require.NotNil(t, event)
	assert.Equal(t, models.EventStateRestoring, event.State)

	rows, err := db.QueryContext(ctx, `
		SELECT device_identifier, state, desired_state, restore_state
		FROM curtailment_target
		WHERE curtailment_event_id = $1
	`, inserted.ID)
	require.NoError(t, err)
	defer rows.Close()

	type targetRow struct {
		state        string
		desiredState string
		restoreState sql.NullString
	}
	got := map[string]targetRow{}
	for rows.Next() {
		var device string
		var row targetRow
		require.NoError(t, rows.Scan(&device, &row.state, &row.desiredState, &row.restoreState))
		got[device] = row
	}
	require.NoError(t, rows.Err())
	require.Len(t, got, 6)

	for _, device := range []string{"ap-restore-never-pending", "ap-restore-never-unavailable", "ap-restore-flapped"} {
		row := got[device]
		assert.Equal(t, string(models.TargetStateReleased), row.state, "%s: never-attempted targets release without restore", device)
		assert.Equal(t, models.DesiredStateCurtailed, row.desiredState, "%s: released rows are untouched by the restore reset", device)
		assert.False(t, row.restoreState.Valid, "%s: no restore phase for released rows", device)
	}
	for _, device := range []string{"ap-restore-attempted", "ap-restore-dispatched", "ap-restore-recurtailed"} {
		row := got[device]
		assert.Equal(t, string(models.TargetStatePending), row.state, "%s: attempt history routes through the restore queue", device)
		assert.Equal(t, models.DesiredStateActive, row.desiredState, device)
		require.True(t, row.restoreState.Valid, device)
		assert.Equal(t, string(models.TargetStatePending), row.restoreState.String, device)
	}
}
