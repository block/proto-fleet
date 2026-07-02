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

	claimed, err := store.ClaimAllPairedPolicyTargets(ctx, policyEvent.ID, []models.InsertTargetParams{
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

	claimed, err = store.ClaimAllPairedPolicyTargets(ctx, policyEvent.ID, []models.InsertTargetParams{
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
}

// Pins the ownership-suppression semantics of ListActiveCurtailedDevices for
// all-paired events: the scope watcher keeps devices locked before their
// policy row exists (miners that became paired-like between admission ticks
// must not be claimable by other selectors), concrete non-terminal rows lock
// as usual, and an explicitly RELEASED policy row lets the device leave the
// suppression set while the event is still active.
func TestSQLCurtailmentStore_ListActiveCurtailedDevices_AllPairedScopeLockAndReleasedRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)

	deviceIDs := testContext.DatabaseService.CreateTestMiners(user.OrganizationID, 3, "https://172.17.0.1:80")
	unclaimed, released, unavailable := deviceIDs[0], deviceIDs[1], deviceIDs[2]

	_, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreAllPairedEvent(user.OrganizationID, user.DatabaseID, uuid.New(), "all-paired-scope-lock"),
		[]models.InsertTargetParams{
			curtailmentStoreAllPairedTarget(released, models.TargetStateReleased, "released without restore: no curtail command dispatched"),
			curtailmentStoreAllPairedTarget(unavailable, models.TargetStateUnavailable, "offline"),
		},
	)
	require.NoError(t, err)

	got, err := store.ListActiveCurtailedDevices(ctx, user.OrganizationID)
	require.NoError(t, err)
	assert.Contains(t, got, unclaimed, "scope lock must hold for in-scope miners without a policy row yet")
	assert.Contains(t, got, unavailable, "non-terminal policy rows lock their device")
	assert.NotContains(t, got, released, "released policy rows relinquish the device")
}

// Pins the graceful-Stop release predicate against real SQL: only all-paired
// targets with no dispatch attempt at all (NULL dispatch timestamps and
// retry_count = 0) are released; anything with attempt history routes through
// the restore reset instead. curtail_failure_count is deliberately ignored —
// readiness flaps inflate it without a command ever being sent.
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
	require.Len(t, got, 5)

	for _, device := range []string{"ap-restore-never-pending", "ap-restore-never-unavailable", "ap-restore-flapped"} {
		row := got[device]
		assert.Equal(t, string(models.TargetStateReleased), row.state, "%s: never-attempted targets release without restore", device)
		assert.Equal(t, models.DesiredStateCurtailed, row.desiredState, "%s: released rows are untouched by the restore reset", device)
		assert.False(t, row.restoreState.Valid, "%s: no restore phase for released rows", device)
	}
	for _, device := range []string{"ap-restore-attempted", "ap-restore-dispatched"} {
		row := got[device]
		assert.Equal(t, string(models.TargetStatePending), row.state, "%s: attempt history routes through the restore queue", device)
		assert.Equal(t, models.DesiredStateActive, row.desiredState, device)
		require.True(t, row.restoreState.Valid, device)
		assert.Equal(t, string(models.TargetStatePending), row.restoreState.String, device)
	}
}
