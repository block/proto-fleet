package sqlstores_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/testutil"
)

// confirmationTestMinerURL is the placeholder mock-miner endpoint used when a
// test needs a real (paired) device row so the pairing-status join resolves.
const confirmationTestMinerURL = "https://172.17.0.1:80"

// insertDispatchedCurtailTarget inserts an active event with one target and
// dispatches it in the curtail phase (state='dispatched', desired='curtailed',
// curtail_dispatched_at/curtail_batch_uuid stamped via the mirror logic). It
// returns the event's DB id and UUID so callers can transition the event or
// re-read targets.
func insertDispatchedCurtailTarget(
	t *testing.T,
	ctx context.Context,
	store *sqlstores.SQLCurtailmentStore,
	orgID, userID int64,
	label, device, batch string,
	dispatchedAt time.Time,
) (int64, uuid.UUID) {
	t.Helper()
	eventUUID := uuid.New()
	inserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(orgID, userID, eventUUID, models.EventStateActive, label),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget(device, models.TargetStatePending, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)
	curtailed := models.DesiredStateCurtailed
	require.NoError(t, store.UpdateTargetState(ctx, inserted.ID, device, interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateDispatched,
		LastDispatchedAt:     &dispatchedAt,
		LastBatchUUID:        &batch,
		ExpectedDesiredState: &curtailed,
	}))
	return inserted.ID, eventUUID
}

// confirmationTargetsByDevice reads the global eligibility list and indexes it
// by device identifier for order-independent assertions.
func confirmationTargetsByDevice(
	t *testing.T,
	ctx context.Context,
	store *sqlstores.SQLCurtailmentStore,
) map[string]models.ConfirmationTarget {
	t.Helper()
	rows, err := store.ListEligibleConfirmationTargets(ctx)
	require.NoError(t, err)
	byDevice := make(map[string]models.ConfirmationTarget, len(rows))
	for _, row := range rows {
		byDevice[row.DeviceIdentifier] = row
	}
	return byDevice
}

// TestSQLCurtailmentStore_ConfirmationEligibleWorkIncludesDispatchedPhases
// covers the inclusion cases: dispatched curtail targets under pending and
// active events, and dispatched restore targets under restoring events. It
// also pins that each returned row carries the phase-correct dispatch
// timestamp and batch UUID (the restore row must reflect its restore phase,
// not the earlier curtail phase).
func TestSQLCurtailmentStore_ConfirmationEligibleWorkIncludesDispatchedPhases(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	org := user.OrganizationID

	// Pending curtail event with a dispatched curtail target.
	pendingUUID := uuid.New()
	pendingInserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(org, user.DatabaseID, pendingUUID, models.EventStatePending, "confirm-pending"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-confirm-pending", models.TargetStatePending, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)
	curtailed := models.DesiredStateCurtailed
	pendingDispatchedAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	pendingBatch := "batch-confirm-pending"
	require.NoError(t, store.UpdateTargetState(ctx, pendingInserted.ID, "miner-confirm-pending", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateDispatched,
		LastDispatchedAt:     &pendingDispatchedAt,
		LastBatchUUID:        &pendingBatch,
		ExpectedDesiredState: &curtailed,
	}))

	// Active curtail event with a dispatched curtail target.
	activeDispatchedAt := time.Date(2026, 7, 1, 11, 0, 0, 0, time.UTC)
	activeID, _ := insertDispatchedCurtailTarget(t, ctx, store, org, user.DatabaseID, "confirm-active", "miner-confirm-active", "batch-confirm-active", activeDispatchedAt)

	// Restoring event with a dispatched restore target. Curtail phase is
	// stamped first (different timestamp + batch), then the event moves to
	// restoring and the restore phase is dispatched; the query must return the
	// restore phase, not the curtail phase.
	restoreID, restoreUUID := insertDispatchedCurtailTarget(t, ctx, store, org, user.DatabaseID, "confirm-restore", "miner-confirm-restore", "batch-confirm-restore-curtail", time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
	_, err = store.BeginRestoreTransition(ctx, org, restoreUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)
	activeDesired := models.DesiredStateActive
	restoreDispatchedAt := time.Date(2026, 7, 1, 13, 0, 0, 0, time.UTC)
	restoreBatch := "batch-confirm-restore"
	require.NoError(t, store.UpdateTargetState(ctx, restoreID, "miner-confirm-restore", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateDispatched,
		LastDispatchedAt:     &restoreDispatchedAt,
		LastBatchUUID:        &restoreBatch,
		ExpectedDesiredState: &activeDesired,
	}))

	byDevice := confirmationTargetsByDevice(t, ctx, store)
	require.Len(t, byDevice, 3)

	pendingRow, ok := byDevice["miner-confirm-pending"]
	require.True(t, ok, "pending curtail target must be eligible")
	assert.Equal(t, pendingInserted.ID, pendingRow.EventID)
	assert.Equal(t, pendingUUID, pendingRow.EventUUID)
	assert.Equal(t, org, pendingRow.OrgID)
	assert.Equal(t, models.EventStatePending, pendingRow.EventState)
	assert.Equal(t, models.DesiredStateCurtailed, pendingRow.DesiredState)
	assertTimeEqual(t, pendingDispatchedAt, &pendingRow.DispatchedAt)
	assert.Equal(t, pendingBatch, pendingRow.BatchUUID)

	activeRow, ok := byDevice["miner-confirm-active"]
	require.True(t, ok, "active curtail target must be eligible")
	assert.Equal(t, activeID, activeRow.EventID)
	assert.Equal(t, models.EventStateActive, activeRow.EventState)
	assert.Equal(t, models.DesiredStateCurtailed, activeRow.DesiredState)
	assertTimeEqual(t, activeDispatchedAt, &activeRow.DispatchedAt)
	assert.Equal(t, "batch-confirm-active", activeRow.BatchUUID)

	restoreRow, ok := byDevice["miner-confirm-restore"]
	require.True(t, ok, "restoring restore target must be eligible")
	assert.Equal(t, restoreID, restoreRow.EventID)
	assert.Equal(t, models.EventStateRestoring, restoreRow.EventState)
	assert.Equal(t, models.DesiredStateActive, restoreRow.DesiredState)
	// The returned phase values must be the restore phase, not the curtail phase.
	assertTimeEqual(t, restoreDispatchedAt, &restoreRow.DispatchedAt)
	assert.Equal(t, restoreBatch, restoreRow.BatchUUID)
}

// TestSQLCurtailmentStore_ConfirmationEligibleWorkExcludesIneligible covers the
// exclusion cases: non-dispatched target states, targets under every terminal
// event state, and phase mismatches (a curtail-desired target under a restoring
// event and a restore-desired target under an active event). All created rows
// are ineligible, so the global read must be empty.
func TestSQLCurtailmentStore_ConfirmationEligibleWorkExcludesIneligible(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	orgID := user.OrganizationID
	userID := user.DatabaseID
	curtailed := models.DesiredStateCurtailed
	activeDesired := models.DesiredStateActive

	// Not dispatched: target still pending under an active event.
	pendingUUID := uuid.New()
	_, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(orgID, userID, pendingUUID, models.EventStateActive, "exclude-pending-state"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-exclude-pending", models.TargetStatePending, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)

	// Confirmed: target advanced past dispatched.
	confirmedID, _ := insertDispatchedCurtailTarget(t, ctx, store, orgID, userID, "exclude-confirmed", "miner-exclude-confirmed", "batch-exclude-confirmed", time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC))
	confirmedAt := time.Date(2026, 7, 2, 10, 0, 30, 0, time.UTC)
	require.NoError(t, store.UpdateTargetState(ctx, confirmedID, "miner-exclude-confirmed", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateConfirmed,
		ConfirmedAt:          &confirmedAt,
		ExpectedDesiredState: &curtailed,
	}))

	// Terminal events: a fully phase-valid dispatched curtail target that is
	// excluded only because its parent event reached a terminal state.
	for i, terminal := range []models.EventState{
		models.EventStateCompleted,
		models.EventStateCompletedWithFailures,
		models.EventStateCancelled,
		models.EventStateFailed,
	} {
		device := "miner-exclude-terminal-" + string(terminal)
		eventID, _ := insertDispatchedCurtailTarget(t, ctx, store, orgID, userID, "exclude-terminal-"+string(terminal), device, "batch-terminal", time.Date(2026, 7, 2, 11, i, 0, 0, time.UTC))
		require.NoError(t, store.UpdateEventState(ctx, eventID, models.EventStateActive, terminal, nil, nil))
	}

	// Phase mismatch: curtail-desired dispatched target under a restoring event.
	// Directly seed a restoring event, then stamp the curtail phase while the
	// target still wants 'curtailed'.
	mismatchRestoringUUID := uuid.New()
	mismatchRestoringInserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(orgID, userID, mismatchRestoringUUID, models.EventStateRestoring, "exclude-mismatch-restoring"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-exclude-mismatch-restoring", models.TargetStateDispatched, models.DesiredStateCurtailed),
		},
	)
	require.NoError(t, err)
	mismatchCurtailAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	mismatchCurtailBatch := "batch-exclude-mismatch-restoring"
	require.NoError(t, store.UpdateTargetState(ctx, mismatchRestoringInserted.ID, "miner-exclude-mismatch-restoring", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateDispatched,
		LastDispatchedAt:     &mismatchCurtailAt,
		LastBatchUUID:        &mismatchCurtailBatch,
		ExpectedDesiredState: &curtailed,
	}))

	// Phase mismatch: restore-desired dispatched target under an active event.
	mismatchActiveUUID := uuid.New()
	mismatchActiveInserted, err := store.InsertEventWithTargets(
		ctx,
		curtailmentStoreTestEvent(orgID, userID, mismatchActiveUUID, models.EventStateActive, "exclude-mismatch-active"),
		[]models.InsertTargetParams{
			curtailmentStoreTestTarget("miner-exclude-mismatch-active", models.TargetStateDispatched, models.DesiredStateActive),
		},
	)
	require.NoError(t, err)
	mismatchRestoreAt := time.Date(2026, 7, 2, 13, 0, 0, 0, time.UTC)
	mismatchRestoreBatch := "batch-exclude-mismatch-active"
	require.NoError(t, store.UpdateTargetState(ctx, mismatchActiveInserted.ID, "miner-exclude-mismatch-active", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateDispatched,
		LastDispatchedAt:     &mismatchRestoreAt,
		LastBatchUUID:        &mismatchRestoreBatch,
		ExpectedDesiredState: &activeDesired,
	}))

	rows, err := store.ListEligibleConfirmationTargets(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows, "no ineligible target may appear in confirmation work")
}

// TestSQLCurtailmentStore_ConfirmationEligibleWorkReturnsPairingBaselineAndPolicyFlag
// pins the confirmation-support fields: the pairing-status join (a real paired
// device vs a device row that does not exist -> 'UNPAIRED'), the baseline power,
// and the all-paired policy flag the pulse needs to reproduce the pairing gate.
func TestSQLCurtailmentStore_ConfirmationEligibleWorkReturnsPairingBaselineAndPolicyFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	orgID := user.OrganizationID
	curtailed := models.DesiredStateCurtailed

	// Paired device under an all-paired FULL_FLEET policy event, with a baseline.
	pairedDevices := testContext.DatabaseService.CreateTestMiners(orgID, 1, confirmationTestMinerURL)
	pairedDevice := pairedDevices[0]
	policyUUID := uuid.New()
	policyEvent := curtailmentStoreTestEvent(orgID, user.DatabaseID, policyUUID, models.EventStateActive, "confirm-policy")
	policyEvent.Mode = models.ModeFullFleet
	policyEvent.ForceIncludeAllPairedMiners = true
	baseline := 3200.5
	policyTarget := curtailmentStoreTestTarget(pairedDevice, models.TargetStatePending, models.DesiredStateCurtailed)
	policyTarget.BaselinePowerW = &baseline
	policyInserted, err := store.InsertEventWithTargets(ctx, policyEvent, []models.InsertTargetParams{policyTarget})
	require.NoError(t, err)
	policyDispatchedAt := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	policyBatch := "batch-confirm-policy"
	require.NoError(t, store.UpdateTargetState(ctx, policyInserted.ID, pairedDevice, interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateDispatched,
		LastDispatchedAt:     &policyDispatchedAt,
		LastBatchUUID:        &policyBatch,
		ExpectedDesiredState: &curtailed,
	}))

	// Phantom device (no device row) under a normal event: pairing -> UNPAIRED,
	// flag false, no baseline.
	_, _ = insertDispatchedCurtailTarget(t, ctx, store, orgID, user.DatabaseID, "confirm-phantom", "miner-confirm-phantom", "batch-confirm-phantom", time.Date(2026, 7, 3, 11, 0, 0, 0, time.UTC))

	byDevice := confirmationTargetsByDevice(t, ctx, store)
	require.Len(t, byDevice, 2)

	pairedRow, ok := byDevice[pairedDevice]
	require.True(t, ok)
	assert.Equal(t, "PAIRED", pairedRow.PairingStatus)
	assert.True(t, pairedRow.ForceIncludeAllPairedMiners)
	require.NotNil(t, pairedRow.BaselinePowerW)
	assert.InDelta(t, baseline, *pairedRow.BaselinePowerW, 0.001)

	phantomRow, ok := byDevice["miner-confirm-phantom"]
	require.True(t, ok)
	assert.Equal(t, "UNPAIRED", phantomRow.PairingStatus)
	assert.False(t, phantomRow.ForceIncludeAllPairedMiners)
	assert.Nil(t, phantomRow.BaselinePowerW)
}

// TestSQLCurtailmentStore_ConfirmationGuardPromotesOnceAndRejectsDuplicate
// proves the single-winner guard: the first confirmation promotes the target,
// and a second confirmation carrying the same expected batch UUID but now
// finding the target past 'dispatched' race-loses.
func TestSQLCurtailmentStore_ConfirmationGuardPromotesOnceAndRejectsDuplicate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	orgID := user.OrganizationID

	batch := "batch-guard-dup"
	eventID, eventUUID := insertDispatchedCurtailTarget(t, ctx, store, orgID, user.DatabaseID, "guard-dup", "miner-guard-dup", batch, time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC))

	curtailed := models.DesiredStateCurtailed
	dispatched := models.TargetStateDispatched
	confirmedAt := time.Date(2026, 7, 4, 10, 0, 30, 0, time.UTC)
	confirm := interfaces.UpdateCurtailmentTargetStateParams{
		State:                     models.TargetStateConfirmed,
		ConfirmedAt:               &confirmedAt,
		ExpectedDesiredState:      &curtailed,
		ExpectedState:             &dispatched,
		ExpectedDispatchBatchUUID: &batch,
	}

	// First guarded confirmation wins.
	require.NoError(t, store.UpdateTargetState(ctx, eventID, "miner-guard-dup", confirm))
	targets, err := store.ListTargetsByEvent(ctx, orgID, eventUUID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, models.TargetStateConfirmed, targets[0].State)

	// Duplicate confirmation with the same expected batch UUID now finds the
	// target already 'confirmed' (state != dispatched) and must race-lose.
	err = store.UpdateTargetState(ctx, eventID, "miner-guard-dup", confirm)
	require.ErrorIs(t, err, interfaces.ErrCurtailmentEventStateRaceLoss)
}

// TestSQLCurtailmentStore_ConfirmationGuardRejectsStaleBatchAfterRedispatch
// proves the batch UUID is the ABA token: after a timeout/redispatch stamps a
// new batch UUID, a confirmation guarded with the old batch UUID race-loses
// even though the target is still 'dispatched', while the current batch UUID
// still promotes.
func TestSQLCurtailmentStore_ConfirmationGuardRejectsStaleBatchAfterRedispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	orgID := user.OrganizationID

	oldBatch := "batch-guard-aba-old"
	eventID, eventUUID := insertDispatchedCurtailTarget(t, ctx, store, orgID, user.DatabaseID, "guard-aba", "miner-guard-aba", oldBatch, time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC))

	curtailed := models.DesiredStateCurtailed
	dispatched := models.TargetStateDispatched

	// Timeout + redispatch stamps a new batch UUID while keeping state dispatched.
	newBatch := "batch-guard-aba-new"
	redispatchedAt := time.Date(2026, 7, 5, 10, 0, 5, 0, time.UTC)
	require.NoError(t, store.UpdateTargetState(ctx, eventID, "miner-guard-aba", interfaces.UpdateCurtailmentTargetStateParams{
		State:                models.TargetStateDispatched,
		LastDispatchedAt:     &redispatchedAt,
		LastBatchUUID:        &newBatch,
		ExpectedDesiredState: &curtailed,
	}))

	confirmedAt := time.Date(2026, 7, 5, 10, 0, 10, 0, time.UTC)

	// Confirmation guarded with the stale (old) batch UUID race-loses.
	staleErr := store.UpdateTargetState(ctx, eventID, "miner-guard-aba", interfaces.UpdateCurtailmentTargetStateParams{
		State:                     models.TargetStateConfirmed,
		ConfirmedAt:               &confirmedAt,
		ExpectedDesiredState:      &curtailed,
		ExpectedState:             &dispatched,
		ExpectedDispatchBatchUUID: &oldBatch,
	})
	require.ErrorIs(t, staleErr, interfaces.ErrCurtailmentEventStateRaceLoss)

	// Confirmation guarded with the current batch UUID still promotes.
	require.NoError(t, store.UpdateTargetState(ctx, eventID, "miner-guard-aba", interfaces.UpdateCurtailmentTargetStateParams{
		State:                     models.TargetStateConfirmed,
		ConfirmedAt:               &confirmedAt,
		ExpectedDesiredState:      &curtailed,
		ExpectedState:             &dispatched,
		ExpectedDispatchBatchUUID: &newBatch,
	}))
	targets, err := store.ListTargetsByEvent(ctx, orgID, eventUUID)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, models.TargetStateConfirmed, targets[0].State)
}

// TestSQLCurtailmentStore_ConfirmationGuardExpectedDesiredStateStillEnforced
// proves the pre-existing desired-state guard still fires alongside the new
// target-state and batch-UUID guards: a curtail confirmation guarded with
// expected desired state 'curtailed' race-loses once a Stop has flipped the
// target's desired state to 'active'.
func TestSQLCurtailmentStore_ConfirmationGuardExpectedDesiredStateStillEnforced(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	testContext := testutil.InitializeDBServiceInfrastructure(t)
	user := testContext.DatabaseService.CreateSuperAdminUser()
	ctx := t.Context()
	store := sqlstores.NewSQLCurtailmentStore(testContext.DatabaseService.DB)
	orgID := user.OrganizationID

	batch := "batch-guard-coexist"
	eventID, eventUUID := insertDispatchedCurtailTarget(t, ctx, store, orgID, user.DatabaseID, "guard-coexist", "miner-guard-coexist", batch, time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC))

	// Stop flips the target to the restore phase (desired_state='active').
	_, err := store.BeginRestoreTransition(ctx, orgID, eventUUID, interfaces.BeginRestoreTransitionParams{})
	require.NoError(t, err)

	curtailed := models.DesiredStateCurtailed
	dispatched := models.TargetStateDispatched
	confirmedAt := time.Date(2026, 7, 6, 10, 0, 30, 0, time.UTC)

	// A curtail-phase confirmation (all guards set) must race-lose because the
	// desired-state guard no longer matches.
	err = store.UpdateTargetState(ctx, eventID, "miner-guard-coexist", interfaces.UpdateCurtailmentTargetStateParams{
		State:                     models.TargetStateConfirmed,
		ConfirmedAt:               &confirmedAt,
		ExpectedDesiredState:      &curtailed,
		ExpectedState:             &dispatched,
		ExpectedDispatchBatchUUID: &batch,
	})
	require.ErrorIs(t, err, interfaces.ErrCurtailmentEventStateRaceLoss)
}
