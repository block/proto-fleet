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
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
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
	// effective_batch_size was stamped at Start; the transition does not touch it.
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

// On BeginRestoreTransition error: event stays Active, drift dispatch
// skipped this tick (re-curtailing would extend past max_duration); next
// tick retries.
func TestReconciler_EnforceMaxDuration_BeginRestoreErrorSkipsDriftDispatch(t *testing.T) {
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
	// Confirmed target with drifted telemetry: drift dispatch WOULD fire if
	// enforceMaxDuration fell through on error. The fix returns true on error
	// so observeActive skips drift; the assertion below pins that.
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
	assert.Equal(t, 0, disp.curtailCalls,
		"drift dispatch must NOT run when max_duration elapsed and the transition failed; re-curtailing would extend past the cap")
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
	assert.True(t, disp.uncurtailLastSuppressed, "curtailment-owned restore batches must suppress command activity")
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

// TestReconciler_Restoring_OrphanDispatchingPriorityOverFreshPending:
// after a reconciler restart leaves DISPATCHING orphans alongside
// untouched PENDING targets, the next tick must redispatch ONLY the
// orphans. Mixing them with fresh PENDING would double the inrush and
// bypass the one-batch-per-interval throttle.
func TestReconciler_Restoring_OrphanDispatchingPriorityOverFreshPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(3)
	eventID := int64(30)
	store.events = []*models.Event{{
		ID:                      eventID,
		EventUUID:               uuid.New(),
		OrgID:                   1,
		State:                   models.EventStateRestoring,
		EffectiveBatchSize:      &effBatch,
		RestoreBatchIntervalSec: 0,
	}}
	// orphan-A and orphan-B carry State=DISPATCHING from an interrupted
	// prior tick; fresh-C and fresh-D are untouched PENDING claims.
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "orphan-A", State: models.TargetStateDispatching, DesiredState: models.DesiredStateActive, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "orphan-B", State: models.TargetStateDispatching, DesiredState: models.DesiredStateActive, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "fresh-C", State: models.TargetStatePending, DesiredState: models.DesiredStateActive, BaselinePowerW: ptrFloat64(3000)},
		{CurtailmentEventID: eventID, DeviceIdentifier: "fresh-D", State: models.TargetStatePending, DesiredState: models.DesiredStateActive, BaselinePowerW: ptrFloat64(3000)},
	}

	r.runTick(context.Background())

	require.Equal(t, 1, disp.uncurtailCalls,
		"orphan-recovery wave must fire exactly one Uncurtail call")
	assert.ElementsMatch(t, []string{"orphan-A", "orphan-B"}, disp.uncurtailLastIDs,
		"the wave must include only orphans; fresh PENDING is held for the next tick")

	// fresh-C and fresh-D must still be PENDING — orphan-priority means
	// they don't share the batch and the interval/throttle works as
	// designed on the next tick.
	for _, tgt := range store.targetsByEventID[eventID] {
		switch tgt.DeviceIdentifier {
		case "fresh-C", "fresh-D":
			assert.Equalf(t, models.TargetStatePending, tgt.State,
				"fresh PENDING target %q must not be claimed alongside orphans", tgt.DeviceIdentifier)
		}
	}
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

// An unknown TargetState must NOT complete the event — forces future
// schema additions to ship their handling alongside.
func TestReconciler_Restoring_UnknownTargetStateKeepsEventNonTerminal(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	eventID := int64(42)
	store.events = []*models.Event{{
		ID:        eventID,
		EventUUID: uuid.New(),
		OrgID:     1,
		State:     models.EventStateRestoring,
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "m1", State: models.TargetState("future_state"), DesiredState: models.DesiredStateActive},
	}

	r.runTick(context.Background())

	_, called := store.updateEventLast[eventID]
	assert.False(t, called,
		"unknown target state must keep the event non-terminal; UpdateEventState must not fire")
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

// A non-race-loss pre-write failure drops one target from the batch
// (remaining devices proceed) and burns one retry slot so persistent
// failures escalate to RESTORE_FAILED instead of cycling.
func TestReconciler_Restoring_PreWriteFailureSkipsTargetButDispatchesRest(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	// Fail the first DISPATCHING pre-write (m1) with a non-race-loss
	// error; subsequent calls (m2's pre-write + m1's recordDispatchFailure
	// recovery write + m2's post-cmd DISPATCHED) succeed.
	failedOnce := false
	store.updateTargetStateHook = func(device string, _ interfaces.UpdateCurtailmentTargetStateParams, _ int) error {
		if device == "m1" && !failedOnce {
			failedOnce = true
			return errors.New("transient db blip")
		}
		return nil
	}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(2)
	eventID := int64(84)
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

	// Uncurtail fired exactly once with just m2 in the batch.
	assert.Equal(t, 1, disp.uncurtailCalls, "Uncurtail must still fire for the surviving target(s)")
	assert.Equal(t, []string{"m2"}, disp.uncurtailLastIDs,
		"failed pre-write target must be excluded from the Uncurtail selector")

	// m1 stays Pending for next-tick reclaim but burns one retry slot.
	m1 := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, m1.State,
		"failed pre-write target must remain in PENDING for next-tick reclaim")
	assert.Equal(t, int32(1), m1.RetryCount,
		"non-race-loss pre-write failure must burn one retry slot so persistent failure escalates to RESTORE_FAILED")
	require.NotNil(t, m1.LastError, "pre-write failure must stamp last_error for operator visibility")
	assert.Contains(t, *m1.LastError, "transient db blip")
	// m2 successfully advanced to Dispatched.
	m2 := store.targetsByEventID[eventID][1]
	assert.Equal(t, models.TargetStateDispatched, m2.State,
		"surviving target must complete the dispatch cycle")
}

// If every pre-write fails, dispatchSet is empty and Uncurtail does NOT
// fire — guards against a regression dropping the len==0 check.
func TestReconciler_Restoring_AllPreWriteFailuresSkipUncurtail(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	// Every write fails (DB fully degraded). The dispatch loop's pre-write
	// fails, the recordDispatchFailure recovery write also fails — neither
	// the target's state nor retry_count advances. Pin the dispatchSet-empty
	// → no-Uncurtail contract; retry budget cycles to the next tick (when
	// the DB is presumably back up). For the intermittent-failure path
	// where recovery succeeds, see TestReconciler_Restoring_PreWriteFailurePersistsExhaustsRetryBudget.
	store.updateTargetStateHook = func(string, interfaces.UpdateCurtailmentTargetStateParams, int) error {
		return errors.New("transient db blip")
	}
	// Fully-degraded DB: the fallback retry-budget bump also fails, so
	// retry_count stays at 0 in both the DB and the in-memory mirror.
	store.bumpTargetRetryErr = errors.New("transient db blip")

	r := newReconcilerForTest(store, disp)
	effBatch := int32(2)
	eventID := int64(85)
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

	assert.Equal(t, 0, disp.uncurtailCalls,
		"Uncurtail must not fire when every batch target's pre-write failed")
	for _, t0 := range store.targetsByEventID[eventID] {
		assert.Equal(t, models.TargetStatePending, t0.State,
			"%s stays Pending so next tick can re-claim it", t0.DeviceIdentifier)
		assert.Equal(t, int32(0), t0.RetryCount,
			"%s retry budget unchanged when the recovery write also fails (fully degraded DB)", t0.DeviceIdentifier)
	}
}

// A target whose DISPATCHING pre-write keeps failing burns one retry
// slot per tick (when the recovery write succeeds) and lands in
// RESTORE_FAILED after MaxRetries so the event can complete.
func TestReconciler_Restoring_PreWriteFailurePersistsExhaustsRetryBudget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	// Fail every DISPATCHING pre-write but let the subsequent
	// recordDispatchFailure recovery write succeed. The hook differentiates
	// by inspecting params.State: pre-write is models.TargetStateDispatching;
	// recordDispatchFailure writes the retry-bumped state which is either
	// TargetStatePending (budget remaining) or TargetStateRestoreFailed.
	store.updateTargetStateHook = func(_ string, params interfaces.UpdateCurtailmentTargetStateParams, _ int) error {
		if params.State == models.TargetStateDispatching {
			return errors.New("transient db blip")
		}
		return nil
	}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(1)
	eventID := int64(86)
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
	}

	// Tick 1: pre-write fails → retry 1, still PENDING.
	r.runTick(context.Background())
	m1 := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, m1.State)
	assert.Equal(t, int32(1), m1.RetryCount)

	// Tick 2: pre-write fails → retry 2, still PENDING.
	r.runTick(context.Background())
	m1 = store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, m1.State)
	assert.Equal(t, int32(2), m1.RetryCount)

	// Tick 3: pre-write fails → retry 3 (hits MaxRetries) → RESTORE_FAILED.
	r.runTick(context.Background())
	m1 = store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStateRestoreFailed, m1.State,
		"persistently-failing pre-write must escalate to RESTORE_FAILED at MaxRetries — without the fix the target cycles forever")
	assert.Equal(t, int32(3), m1.RetryCount)
	assert.Equal(t, 0, disp.uncurtailCalls,
		"cmd.Uncurtail never fires because the pre-write never lands")
}

// When the rich UpdateTargetState write inside recordDispatchFailure fails
// non-race-loss, the fallback BumpTargetRetry persists a retry_count
// advance even though state stays at the prior value. This lets a
// persistently-failing state-change write still escalate to RESTORE_FAILED
// on the next successful UpdateTargetState rather than looping forever
// without budget progress.
func TestReconciler_Restoring_RecoveryWriteFailureFallsBackToBumpRetry(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	// Every UpdateTargetState write fails non-race-loss, but BumpTargetRetry
	// succeeds — models the case where the rich UPDATE is blocked (lock
	// timeout, deadline, etc.) but the simple counter UPDATE still lands.
	store.updateTargetStateHook = func(string, interfaces.UpdateCurtailmentTargetStateParams, int) error {
		return errors.New("transient db blip")
	}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(1)
	eventID := int64(91)
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
	}

	r.runTick(context.Background())

	m1 := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, m1.State,
		"state stays at the prior value when the rich UPDATE fails")
	assert.Equal(t, int32(1), m1.RetryCount,
		"retry budget advances via BumpTargetRetry fallback even though the rich UPDATE didn't land")
	assert.Equal(t, 1, store.bumpTargetRetryCalls,
		"fallback fires exactly once per recordDispatchFailure invocation")
	assert.Equal(t, "m1", store.lastBumpTargetRetry.DeviceIdentifier)
	assert.Equal(t, 0, disp.uncurtailCalls,
		"cmd.Uncurtail does not fire because the DISPATCHING pre-write failed")
}

// Uncurtail returning empty BatchIdentifier (no live devices) burns
// retry budget on every batch target.
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

// A per-device Skipped entry on Uncurtail leaves the kept device
// Dispatched and the skipped device Pending with retry consumed.
func TestReconciler_Restoring_PerDeviceFilterSkipsTargetStaysPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{
		uncurtailResultOverride: &command.CommandResult{
			BatchIdentifier:             "batch-uncurtail",
			DispatchedCount:             1,
			DispatchedDeviceIdentifiers: []string{"m1"},
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

func TestReconciler_Restoring_NotEnqueuedTargetStaysPending(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{
		uncurtailResultOverride: &command.CommandResult{
			BatchIdentifier:             "batch-uncurtail",
			DispatchedCount:             1,
			DispatchedDeviceIdentifiers: []string{"m1"},
		},
	}

	r := newReconcilerForTest(store, disp)
	effBatch := int32(2)
	eventID := int64(83)
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

	dispatched := store.targetsByEventID[eventID][0]
	notEnqueued := store.targetsByEventID[eventID][1]
	assert.Equal(t, models.TargetStateDispatched, dispatched.State)
	assert.Equal(t, models.TargetStatePending, notEnqueued.State,
		"target missing from DispatchedDeviceIdentifiers must not block the in-flight gate")
	assert.Equal(t, int32(1), notEnqueued.RetryCount)
	require.NotNil(t, notEnqueued.LastError)
	assert.Contains(t, *notEnqueued.LastError, "did not enqueue")
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

// Restore-phase: a vanished candidate burns retry budget. Interval gate
// held closed so the freshly-Pending target isn't re-claimed this tick.
func TestReconciler_Restoring_MissingCandidateDuringConfirmConsumesRetryBudget(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}

	r := newReconcilerForTest(store, disp)
	lastDispatch := r.now().Add(-1 * time.Minute)
	effBatch := int32(2)
	eventID := int64(62)
	store.events = []*models.Event{{
		ID:                      eventID,
		EventUUID:               uuid.New(),
		OrgID:                   1,
		State:                   models.EventStateRestoring,
		EffectiveBatchSize:      &effBatch,
		RestoreBatchIntervalSec: 600, // 10m > 1m gap → interval gate stays closed
	}}
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "m1",
			State:              models.TargetStateDispatched,
			DesiredState:       models.DesiredStateActive,
			BaselinePowerW:     ptrFloat64(3000),
			LastDispatchedAt:   &lastDispatch,
			RetryCount:         0,
		},
	}
	// Candidate row deliberately absent: device was unpaired or deleted
	// after dispatch.
	store.candidates = nil

	r.runTick(context.Background())

	final := store.targetsByEventID[eventID][0]
	assert.Equal(t, models.TargetStatePending, final.State,
		"missing candidate routes restore-phase target back to Pending while retry budget remains")
	assert.Equal(t, int32(1), final.RetryCount,
		"missing candidate burns one retry slot per tick")
	require.NotNil(t, final.LastError)
	assert.Contains(t, *final.LastError, "candidate row missing")
	// disp.uncurtailCalls==0 is enforced by Gate 2 (interval gate) rather than
	// by the missing-candidate path itself: after recordDispatchFailure the
	// target is Pending with retry budget left, so the in-flight gate would
	// permit a re-claim. The 600s interval against a 60s-old LastDispatchedAt
	// holds the re-claim, isolating the missing-candidate assertions above.
	assert.Equal(t, 0, disp.uncurtailCalls,
		"interval gate must hold the re-claim until restore_batch_interval_sec elapses")
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
