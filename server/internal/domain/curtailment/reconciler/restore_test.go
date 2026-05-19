package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/command"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// --- enforceMaxDuration ---

func TestReconciler_EnforceMaxDuration_ElapsedTransitionsToRestoring(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	// max_duration=3600s, started 2h ago → elapsed.
	startedAt := r.now().Add(-2 * time.Hour)
	maxDur := int32(3600)
	eventID := int64(20)
	ev := &models.Event{
		ID:                 eventID,
		EventUUID:          uuid.New(),
		OrgID:              1,
		State:              models.EventStateActive,
		StartedAt:          &startedAt,
		MaxDurationSeconds: &maxDur,
		RestoreBatchSize:   10,
	}
	store.events = []*models.Event{ev}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed},
		{CurtailmentEventID: eventID, DeviceIdentifier: "m2", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed},
	}

	r.runTick(context.Background())

	assert.Equal(t, 1, store.beginRestoreCalls,
		"max_duration elapsed must call BeginRestoreTransition exactly once")
	assert.Equal(t, ev.EventUUID, store.beginRestoreLastEventID)
	assert.Equal(t, int32(10), store.beginRestoreLastBatch,
		"effective_batch_size = max(restore_batch_size=10, ceil(0.01*2)=1) clamped → 10")
	// Drift detection must not run on a force-restored event.
	assert.Equal(t, 0, disp.curtailCalls)
}

func TestReconciler_EnforceMaxDuration_NotElapsedNoOps(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	startedAt := r.now().Add(-1 * time.Minute) // well under any reasonable cap
	maxDur := int32(3600)
	eventID := int64(20)
	store.events = []*models.Event{{
		ID:                 eventID,
		EventUUID:          uuid.New(),
		OrgID:              1,
		State:              models.EventStateActive,
		StartedAt:          &startedAt,
		MaxDurationSeconds: &maxDur,
		RestoreBatchSize:   10,
	}}
	// One confirmed target to make drift detection a meaningful no-op (no
	// telemetry change, stays confirmed).
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed, BaselinePowerW: ptrFloat64(3000)},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "m1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r.runTick(context.Background())

	assert.Equal(t, 0, store.beginRestoreCalls,
		"max_duration not elapsed must leave the event untouched")
}

func TestReconciler_EnforceMaxDuration_AllowUnboundedSkipsCap(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	startedAt := r.now().Add(-30 * 24 * time.Hour) // 30 days; well beyond any cap
	maxDur := int32(3600)
	eventID := int64(20)
	store.events = []*models.Event{{
		ID:                 eventID,
		EventUUID:          uuid.New(),
		OrgID:              1,
		State:              models.EventStateActive,
		StartedAt:          &startedAt,
		MaxDurationSeconds: &maxDur,
		AllowUnbounded:     true, // <-- key: opt-out of the cap
		RestoreBatchSize:   10,
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed, BaselinePowerW: ptrFloat64(3000)},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "m1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r.runTick(context.Background())

	assert.Equal(t, 0, store.beginRestoreCalls,
		"AllowUnbounded events must never trigger forced restore")
}

// TestReconciler_EnforceMaxDuration_BeginRestoreErrorLeavesEventUnchanged
// pins the BeginRestoreTransition failure path: the event state stays Active
// (no in-memory mutation), the transition call counter still records the
// attempt, and drift detection runs on the same tick since enforceMaxDuration
// returns false on error.
func TestReconciler_EnforceMaxDuration_BeginRestoreErrorLeavesEventUnchanged(t *testing.T) {
	store := newFakeStore()
	store.beginRestoreErr = errors.New("db boom")
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	startedAt := r.now().Add(-2 * time.Hour)
	maxDur := int32(3600)
	eventID := int64(70)
	ev := &models.Event{
		ID:                 eventID,
		EventUUID:          uuid.New(),
		OrgID:              1,
		State:              models.EventStateActive,
		StartedAt:          &startedAt,
		MaxDurationSeconds: &maxDur,
		RestoreBatchSize:   10,
	}
	store.events = []*models.Event{ev}
	// Confirmed target with drifted telemetry so we can witness drift
	// dispatch still firing after enforceMaxDuration returns false.
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed, BaselinePowerW: ptrFloat64(3000)},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "m1", LatestPowerW: ptrFloat64(2500), LatestHashRateHS: ptrFloat64(100)},
	}

	r.runTick(context.Background())

	assert.Equal(t, 1, store.beginRestoreCalls, "BeginRestoreTransition is attempted exactly once even on error")
	assert.Equal(t, models.EventStateActive, ev.State,
		"event state must not flip when BeginRestoreTransition errors")
	assert.Equal(t, 1, disp.curtailCalls,
		"drift dispatch must still run on the same tick when enforceMaxDuration returns false")
}

func TestReconciler_EnforceMaxDuration_NilStartedAtSkips(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	maxDur := int32(3600)
	eventID := int64(20)
	store.events = []*models.Event{{
		ID:                 eventID,
		EventUUID:          uuid.New(),
		OrgID:              1,
		State:              models.EventStateActive,
		MaxDurationSeconds: &maxDur,
		// StartedAt nil — shouldn't happen for an active event in production,
		// but the guard prevents a nil-deref if a stale row sneaks in.
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed},
	}
	r.runTick(context.Background())
	assert.Equal(t, 0, store.beginRestoreCalls)
}

// --- observeRestoring: claim + dispatch + confirm + completion ---

func TestReconciler_Restoring_ClaimDispatchesUncurtailBatch(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(2)
	eventID := int64(30)
	store.events = []*models.Event{{
		ID:                      eventID,
		EventUUID:               uuid.New(),
		OrgID:                   1,
		State:                   models.EventStateRestoring,
		EffectiveBatchSize:      &effBatch,
		RestoreBatchIntervalSec: 0, // ignore interval gate
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStatePending, DesiredState: models.DesiredStateActive, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "m2", State: models.TargetStatePending, DesiredState: models.DesiredStateActive, BaselinePowerW: ptrFloat64(3000)},
	}

	r.runTick(context.Background())

	require.Equal(t, 1, disp.uncurtailCalls,
		"one Uncurtail call must cover the whole batch (shared batch_uuid)")
	assert.ElementsMatch(t, []string{"m1", "m2"}, disp.uncurtailLastIDs)
	// Both targets transition to dispatched.
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][1].State)
	// Both targets must share the same LastBatchUUID — one Uncurtail call →
	// one batch identifier on every kept target.
	require.NotNil(t, store.targetsByEventID[eventID][0].LastBatchUUID)
	require.NotNil(t, store.targetsByEventID[eventID][1].LastBatchUUID)
	assert.Equal(t,
		*store.targetsByEventID[eventID][0].LastBatchUUID,
		*store.targetsByEventID[eventID][1].LastBatchUUID,
		"batched Uncurtail targets must share a single batch_uuid")
}

func TestReconciler_Restoring_InFlightGateBlocksClaim(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(2)
	eventID := int64(30)
	store.events = []*models.Event{{
		ID:                      eventID,
		EventUUID:               uuid.New(),
		OrgID:                   1,
		State:                   models.EventStateRestoring,
		EffectiveBatchSize:      &effBatch,
		RestoreBatchIntervalSec: 0,
	}}
	// One target still dispatched from a prior batch; the gate should block a
	// new claim until it terminates.
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStateDispatched, DesiredState: models.DesiredStateActive, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "m2", State: models.TargetStatePending, DesiredState: models.DesiredStateActive, BaselinePowerW: ptrFloat64(3000)},
	}
	// Telemetry doesn't show restored yet, so m1 stays dispatched.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "m1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r.runTick(context.Background())

	assert.Equal(t, 0, disp.uncurtailCalls,
		"in-flight batch must block new claim regardless of pending count")
	assert.Equal(t, models.TargetStatePending, store.targetsByEventID[eventID][1].State,
		"pending target must stay pending while gate is closed")
}

func TestReconciler_Restoring_IntervalGateBlocksClaim(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(2)
	eventID := int64(30)
	// Newest restore dispatch 60s ago; interval=120s → gate closed.
	recent := r.now().Add(-60 * time.Second)
	store.events = []*models.Event{{
		ID:                      eventID,
		EventUUID:               uuid.New(),
		OrgID:                   1,
		State:                   models.EventStateRestoring,
		EffectiveBatchSize:      &effBatch,
		RestoreBatchIntervalSec: 120,
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		// Prior batch already resolved; in-flight gate would pass.
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStateResolved, DesiredState: models.DesiredStateActive, LastDispatchedAt: &recent},
		{CurtailmentEventID: eventID, DeviceIdentifier: "m2", State: models.TargetStatePending, DesiredState: models.DesiredStateActive},
	}

	r.runTick(context.Background())

	assert.Equal(t, 0, disp.uncurtailCalls,
		"interval gate must hold the next batch until restore_batch_interval_sec elapses")
}

func TestReconciler_Restoring_AllTerminalCompletesEvent(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	eventID := int64(40)
	store.events = []*models.Event{{
		ID:        eventID,
		EventUUID: uuid.New(),
		OrgID:     1,
		State:     models.EventStateRestoring,
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStateResolved, DesiredState: models.DesiredStateActive},
		{CurtailmentEventID: eventID, DeviceIdentifier: "m2", State: models.TargetStateResolved, DesiredState: models.DesiredStateActive},
	}

	r.runTick(context.Background())

	assert.Equal(t, models.EventStateCompleted, store.updateEventLast[eventID],
		"all-resolved restoring event must transition to COMPLETED")
	assert.Equal(t, 0, disp.uncurtailCalls,
		"completion path must not enqueue new dispatch")
}

func TestReconciler_Restoring_MixedResolvedAndFailedCompletesWithFailures(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	eventID := int64(41)
	store.events = []*models.Event{{
		ID:        eventID,
		EventUUID: uuid.New(),
		OrgID:     1,
		State:     models.EventStateRestoring,
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStateResolved, DesiredState: models.DesiredStateActive},
		{CurtailmentEventID: eventID, DeviceIdentifier: "m2", State: models.TargetStateRestoreFailed, DesiredState: models.DesiredStateActive},
	}

	r.runTick(context.Background())

	assert.Equal(t, models.EventStateCompletedWithFailures, store.updateEventLast[eventID],
		"a single failure must route the terminal transition to COMPLETED_WITH_FAILURES")
}

func TestReconciler_Restoring_ConfirmsDispatchedTargetWithTelemetry(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	eventID := int64(50)
	store.events = []*models.Event{{
		ID:        eventID,
		EventUUID: uuid.New(),
		OrgID:     1,
		State:     models.EventStateRestoring,
	}}
	// Target already dispatched; telemetry shows power back above baseline*0.5.
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStateDispatched, DesiredState: models.DesiredStateActive, BaselinePowerW: ptrFloat64(3000)},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "m1", LatestPowerW: ptrFloat64(2900), LatestHashRateHS: ptrFloat64(100e12)},
	}

	r.runTick(context.Background())

	assert.Equal(t, models.TargetStateResolved, store.targetsByEventID[eventID][0].State,
		"telemetry > baseline*0.5 must promote dispatched restore to resolved")
	// Event has a single terminal target now → flips to COMPLETED in the same tick.
	assert.Equal(t, models.EventStateCompleted, store.updateEventLast[eventID])
}

// TestReconciler_Restoring_UncurtailErrorKeepsBatchPending pins
// dispatchRestoreBatch's bulk-error path: a dispatcher error rolls every
// batch target's retry count, leaves them Pending with LastError set, and
// emits no per-device Dispatched writes.
func TestReconciler_Restoring_UncurtailErrorKeepsBatchPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{uncurtailErr: errors.New("queue down")}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(2)
	eventID := int64(80)
	store.events = []*models.Event{{
		ID:                      eventID,
		EventUUID:               uuid.New(),
		OrgID:                   1,
		State:                   models.EventStateRestoring,
		EffectiveBatchSize:      &effBatch,
		RestoreBatchIntervalSec: 0,
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStatePending, DesiredState: models.DesiredStateActive},
		{CurtailmentEventID: eventID, DeviceIdentifier: "m2", State: models.TargetStatePending, DesiredState: models.DesiredStateActive},
	}

	r.runTick(context.Background())

	for i, deviceID := range []string{"m1", "m2"} {
		final := store.targetsByEventID[eventID][i]
		assert.Equal(t, models.TargetStatePending, final.State, "%s stays Pending on bulk error", deviceID)
		assert.Equal(t, int32(1), final.RetryCount, "%s retry count bumped", deviceID)
		require.NotNil(t, final.LastError, "%s LastError must be set", deviceID)
		assert.Contains(t, *final.LastError, "queue down")
	}
}

// TestReconciler_Restoring_EmptyBatchIdentifierKeepsBatchPending pins
// dispatchRestoreBatch's empty-result path: an Uncurtail returning nil error
// but an empty BatchIdentifier means the command produced no batch (all
// devices unpaired/deleted post-Stop). Every batch target should burn retry
// budget and surface the no-batch reason.
func TestReconciler_Restoring_EmptyBatchIdentifierKeepsBatchPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{
		uncurtailResultOverride: &command.CommandResult{BatchIdentifier: "", DispatchedCount: 0},
	}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(2)
	eventID := int64(81)
	store.events = []*models.Event{{
		ID:                      eventID,
		EventUUID:               uuid.New(),
		OrgID:                   1,
		State:                   models.EventStateRestoring,
		EffectiveBatchSize:      &effBatch,
		RestoreBatchIntervalSec: 0,
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStatePending, DesiredState: models.DesiredStateActive},
		{CurtailmentEventID: eventID, DeviceIdentifier: "m2", State: models.TargetStatePending, DesiredState: models.DesiredStateActive},
	}

	r.runTick(context.Background())

	for i, deviceID := range []string{"m1", "m2"} {
		final := store.targetsByEventID[eventID][i]
		assert.Equal(t, models.TargetStatePending, final.State, "%s stays Pending on empty batch", deviceID)
		assert.Equal(t, int32(1), final.RetryCount, "%s retry count bumped", deviceID)
		require.NotNil(t, final.LastError, "%s LastError must be set", deviceID)
		assert.Contains(t, *final.LastError, "no batch")
	}
}

// TestReconciler_Restoring_PerDeviceFilterSkipsTargetStaysPending pins
// dispatchRestoreBatch's per-device filter-skip path: an Uncurtail returning
// one Skipped entry must move the kept device to Dispatched and leave the
// skipped device Pending with retry consumed (mirrors
// TestReconciler_DispatchSkippedKeepsTargetPending for the restore phase).
func TestReconciler_Restoring_PerDeviceFilterSkipsTargetStaysPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{
		uncurtailResultOverride: &command.CommandResult{
			BatchIdentifier: "batch-uncurtail",
			DispatchedCount: 1,
			Skipped: []command.SkippedDevice{{
				DeviceIdentifier: "m2",
				FilterName:       "schedule_conflict",
				Reason:           "schedule 99 holds higher priority",
			}},
		},
	}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(2)
	eventID := int64(82)
	store.events = []*models.Event{{
		ID:                      eventID,
		EventUUID:               uuid.New(),
		OrgID:                   1,
		State:                   models.EventStateRestoring,
		EffectiveBatchSize:      &effBatch,
		RestoreBatchIntervalSec: 0,
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetStatePending, DesiredState: models.DesiredStateActive},
		{CurtailmentEventID: eventID, DeviceIdentifier: "m2", State: models.TargetStatePending, DesiredState: models.DesiredStateActive},
	}

	r.runTick(context.Background())

	kept := store.targetsByEventID[eventID][0]
	skipped := store.targetsByEventID[eventID][1]
	assert.Equal(t, models.TargetStateDispatched, kept.State, "kept device must move to Dispatched")
	assert.Equal(t, models.TargetStatePending, skipped.State, "filter-skipped device must stay Pending")
	assert.Equal(t, int32(1), skipped.RetryCount, "filter-skipped device must burn one retry")
	require.NotNil(t, skipped.LastError)
	assert.Contains(t, *skipped.LastError, "schedule 99")
}

// TestReconciler_Restoring_DispatchedAgesOutToRestoreFailed pins the restore
// telemetry-timeout: a Dispatched target whose telemetry never resumes and
// whose retry budget is already at the cap transitions to RestoreFailed via
// recordDispatchFailure; the event then completes with failures.
func TestReconciler_Restoring_DispatchedAgesOutToRestoreFailed(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	// Dispatched 10 minutes ago — well past the 5-minute default timeout.
	lastDispatch := r.now().Add(-10 * time.Minute)
	eventID := int64(60)
	store.events = []*models.Event{{
		ID:        eventID,
		EventUUID: uuid.New(),
		OrgID:     1,
		State:     models.EventStateRestoring,
	}}
	// RetryCount=2 (MaxRetries=3): one more failure tips into RestoreFailed.
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "m1",
			State:              models.TargetStateDispatched,
			DesiredState:       models.DesiredStateActive,
			BaselinePowerW:     ptrFloat64(3000),
			LastDispatchedAt:   &lastDispatch,
			RetryCount:         2,
		},
	}
	// Candidate row exists but power telemetry stays low → isRestored=false.
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "m1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateRestoreFailed, final.State,
		"stale Dispatched + exhausted retry must transition to RestoreFailed")
	assert.Equal(t, int32(3), final.RetryCount)
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "restore telemetry timeout")
	assert.Equal(t, models.EventStateCompletedWithFailures, store.updateEventLast[eventID],
		"all-terminal restoring event must complete with failures")
	assert.Equal(t, 0, disp.uncurtailCalls,
		"a target that hit RestoreFailed must not be re-dispatched")
}

// TestReconciler_Restoring_DispatchedWithinTimeoutDoesNotFail pins the
// happy-path: a Dispatched target whose telemetry is missing but whose
// last_dispatched_at is still within the timeout window stays Dispatched and
// does not consume retry budget.
func TestReconciler_Restoring_DispatchedWithinTimeoutDoesNotFail(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	// Dispatched 1 minute ago — well under the 5-minute default timeout.
	lastDispatch := r.now().Add(-1 * time.Minute)
	eventID := int64(61)
	store.events = []*models.Event{{
		ID:        eventID,
		EventUUID: uuid.New(),
		OrgID:     1,
		State:     models.EventStateRestoring,
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "m1",
			State:              models.TargetStateDispatched,
			DesiredState:       models.DesiredStateActive,
			BaselinePowerW:     ptrFloat64(3000),
			LastDispatchedAt:   &lastDispatch,
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "m1", LatestPowerW: ptrFloat64(50), LatestHashRateHS: ptrFloat64(0)},
	}

	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateDispatched, final.State,
		"within-window Dispatched target must stay Dispatched")
	assert.Equal(t, int32(0), final.RetryCount,
		"within-window timeout check must not consume retry budget")
	assert.Nil(t, final.LastError)
}

// --- isRestored predicate ---

func TestIsRestored(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		power      *float64
		baseline   *float64
		hash       *float64
		factor     float64
		wantResult bool
	}{
		{"power_above_threshold_restored", ptrFloat64(2000), ptrFloat64(3000), ptrFloat64(0), 0.5, true},
		{"power_at_threshold_not_restored", ptrFloat64(1500), ptrFloat64(3000), ptrFloat64(0), 0.5, false},
		{"power_below_threshold_not_restored", ptrFloat64(50), ptrFloat64(3000), ptrFloat64(0), 0.5, false},
		{"baseline_nil_positive_hash_restored", ptrFloat64(2000), nil, ptrFloat64(100e12), 0.5, true},
		{"baseline_nil_zero_hash_not_restored", ptrFloat64(2000), nil, ptrFloat64(0), 0.5, false},
		{"no_telemetry_not_restored", nil, ptrFloat64(3000), nil, 0.5, false},
		{"baseline_zero_falls_back_to_hash", ptrFloat64(2000), ptrFloat64(0), ptrFloat64(100), 0.5, true},
		{"power_present_baseline_nil_hash_nil_not_restored", ptrFloat64(2000), nil, nil, 0.5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isRestored(tc.power, tc.baseline, tc.hash, tc.factor)
			assert.Equal(t, tc.wantResult, got)
		})
	}
}
