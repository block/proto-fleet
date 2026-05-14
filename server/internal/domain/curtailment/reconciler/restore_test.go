package reconciler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isRestored(tc.power, tc.baseline, tc.hash, tc.factor)
			assert.Equal(t, tc.wantResult, got)
		})
	}
}
