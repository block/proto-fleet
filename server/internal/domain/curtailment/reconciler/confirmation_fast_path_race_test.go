package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// --- stale full-tick observations vs. pulse promotions ---

type dispatchedObservationFailureScenario struct {
	name           string
	desiredState   string
	pulseState     models.TargetState
	pulseRetry     int32
	staleRetry     int32
	invalidPairing bool
	failureState   models.TargetState
}

var dispatchedObservationFailureScenarios = []dispatchedObservationFailureScenario{
	{
		name:         "curtail candidate missing",
		desiredState: models.DesiredStateCurtailed,
		pulseState:   models.TargetStateConfirmed,
		staleRetry:   1,
		failureState: models.TargetStateDispatched,
	},
	{
		name:           "curtail pairing invalid",
		desiredState:   models.DesiredStateCurtailed,
		pulseState:     models.TargetStateConfirmed,
		staleRetry:     1,
		invalidPairing: true,
		failureState:   models.TargetStateDispatched,
	},
	{
		name:         "restore candidate missing",
		desiredState: models.DesiredStateActive,
		pulseState:   models.TargetStateResolved,
		pulseRetry:   2,
		staleRetry:   2,
		failureState: models.TargetStatePending,
	},
}

func runDispatchedObservationFailure(
	r *Reconciler,
	ev *models.Event,
	target *models.Target,
	scenario dispatchedObservationFailureScenario,
) {
	var candidate *models.Candidate
	if scenario.invalidPairing {
		candidate = &models.Candidate{
			DeviceIdentifier: target.DeviceIdentifier,
			PairingStatus:    "UNPAIRED",
		}
	}
	if scenario.desiredState == models.DesiredStateActive {
		r.confirmOneRestore(context.Background(), ev, target, candidate)
		return
	}
	r.confirmOneDispatched(context.Background(), ev, target, candidate, scenario.failureState)
}

func TestDispatchedObservationFailure_RaceLosesToPulseAdvance(t *testing.T) {
	for _, scenario := range dispatchedObservationFailureScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			store := newConfirmationFakeStore()
			metrics := newRecordingMetrics()
			dispatchedAt := fastPathTestNow.Add(-time.Minute)
			item := seedDispatchedWork(store, 10, "miner-1", scenario.desiredState, "batch-a", dispatchedAt)
			durable := store.targetsByEventID[10][0]
			stale := *durable
			stale.RetryCount = scenario.staleRetry

			// The pulse advanced the durable row after the tick loaded its
			// dispatched snapshot.
			durable.State = scenario.pulseState
			durable.RetryCount = scenario.pulseRetry

			r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
			ev := &models.Event{
				ID:                          10,
				EventUUID:                   item.EventUUID,
				OrgID:                       1,
				State:                       item.EventState,
				ForceIncludeAllPairedMiners: scenario.invalidPairing,
			}
			runDispatchedObservationFailure(r, ev, &stale, scenario)

			assert.Equal(t, scenario.pulseState, store.targetState(10, "miner-1"))
			assert.Equal(t, scenario.pulseRetry, durable.RetryCount,
				"pulse-updated retry budget must survive the stale failure write")
			assert.Equal(t, models.TargetStateDispatched, stale.State,
				"stale snapshot must not mutate on race-loss")
			assert.Equal(t, scenario.staleRetry, stale.RetryCount,
				"stale snapshot retry budget must not advance on race-loss")
			assert.Equal(t, 1, metrics.EventStateRaceLossCount())
			assert.Equal(t, 0, metrics.TargetWriteFailureCount())

			_, _, params := store.lastWrite()
			require.NotNil(t, params.ExpectedState)
			assert.Equal(t, models.TargetStateDispatched, *params.ExpectedState)
			require.NotNil(t, params.ExpectedDispatchBatchUUID)
			assert.Equal(t, "batch-a", *params.ExpectedDispatchBatchUUID)
		})
	}
}

func TestDispatchedObservationFailure_ProceedsWhenStillDispatched(t *testing.T) {
	for _, scenario := range dispatchedObservationFailureScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			store := newConfirmationFakeStore()
			metrics := newRecordingMetrics()
			dispatchedAt := fastPathTestNow.Add(-time.Minute)
			item := seedDispatchedWork(store, 10, "miner-1", scenario.desiredState, "batch-a", dispatchedAt)
			target := store.targetsByEventID[10][0]
			target.RetryCount = 1

			r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
			ev := &models.Event{
				ID:                          10,
				EventUUID:                   item.EventUUID,
				OrgID:                       1,
				State:                       item.EventState,
				ForceIncludeAllPairedMiners: scenario.invalidPairing,
			}
			runDispatchedObservationFailure(r, ev, target, scenario)

			assert.Equal(t, scenario.failureState, store.targetState(10, "miner-1"))
			assert.Equal(t, int32(2), target.RetryCount,
				"a still-dispatched failure must consume one retry")
			assert.Equal(t, 0, metrics.EventStateRaceLossCount())
			assert.Equal(t, 0, metrics.TargetWriteFailureCount())
		})
	}
}

func TestDispatchedObservationFailure_WriteErrorSkipsUnguardedRetryFallback(t *testing.T) {
	store := newConfirmationFakeStore()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-time.Minute)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	target := store.targetsByEventID[10][0]
	target.RetryCount = 1
	store.updateTargetStateErr = errors.New("injected guarded write failure")

	r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
	ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: item.EventState}
	runDispatchedObservationFailure(r, ev, target, dispatchedObservationFailureScenarios[0])

	assert.Equal(t, models.TargetStateDispatched, target.State)
	assert.Equal(t, int32(1), target.RetryCount,
		"a failed guarded write must not consume retry budget through an unguarded fallback")
	assert.Equal(t, 0, store.bumpTargetRetryCalls)
	assert.Equal(t, 1, metrics.TargetWriteFailureCount())
	_, _, params := store.lastWrite()
	require.NotNil(t, params.ExpectedState)
	require.NotNil(t, params.ExpectedDispatchBatchUUID)
	assert.Equal(t, "batch-a", *params.ExpectedDispatchBatchUUID)
}

type fullTickPositiveConfirmationScenario struct {
	name         string
	desiredState string
	pulseState   models.TargetState
	powerW       float64
}

var fullTickPositiveConfirmationScenarios = []fullTickPositiveConfirmationScenario{
	{
		name:         "curtail",
		desiredState: models.DesiredStateCurtailed,
		pulseState:   models.TargetStateConfirmed,
		powerW:       100,
	},
	{
		name:         "restore",
		desiredState: models.DesiredStateActive,
		pulseState:   models.TargetStateResolved,
		powerW:       2900,
	},
}

func copyTargetForTick(target *models.Target) models.Target {
	stale := *target
	if target.RestorePhase != nil {
		restorePhase := *target.RestorePhase
		stale.RestorePhase = &restorePhase
	}
	return stale
}

func runFullTickPositiveConfirmation(
	r *Reconciler,
	ev *models.Event,
	target *models.Target,
	scenario fullTickPositiveConfirmationScenario,
) {
	candidate := &models.Candidate{
		DeviceIdentifier: target.DeviceIdentifier,
		LatestPowerW:     ptrFloat64(scenario.powerW),
		LatestHashRateHS: ptrFloat64(100),
	}
	if scenario.desiredState == models.DesiredStateActive {
		r.confirmOneRestore(context.Background(), ev, target, candidate)
		return
	}
	r.confirmOneDispatched(context.Background(), ev, target, candidate, models.TargetStateDispatching)
}

func TestFullTickPositiveConfirmation_RaceLosesToPulseAdvance(t *testing.T) {
	for _, scenario := range fullTickPositiveConfirmationScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			store := newConfirmationFakeStore()
			metrics := newRecordingMetrics()
			dispatchedAt := fastPathTestNow.Add(-time.Minute)
			item := seedDispatchedWork(store, 10, "miner-1", scenario.desiredState, "batch-a", dispatchedAt)
			durable := store.targetsByEventID[10][0]
			stale := copyTargetForTick(durable)

			durable.State = scenario.pulseState
			pulseObservedAt := fastPathTestNow.Add(-time.Second)
			durable.ObservedAt = &pulseObservedAt

			r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
			ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: item.EventState}
			runFullTickPositiveConfirmation(r, ev, &stale, scenario)

			assert.Equal(t, scenario.pulseState, durable.State)
			assert.Equal(t, pulseObservedAt, *durable.ObservedAt,
				"the stale tick must not overwrite pulse observation metadata")
			assert.Equal(t, 1, metrics.EventStateRaceLossCount())
			_, _, params := store.lastWrite()
			require.NotNil(t, params.ExpectedState)
			assert.Equal(t, models.TargetStateDispatched, *params.ExpectedState)
			require.NotNil(t, params.ExpectedDispatchBatchUUID)
			assert.Equal(t, "batch-a", *params.ExpectedDispatchBatchUUID)
		})
	}
}

func TestFullTickPositiveConfirmation_StaleBatchRaceLosesToRedispatch(t *testing.T) {
	for _, scenario := range fullTickPositiveConfirmationScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			store := newConfirmationFakeStore()
			metrics := newRecordingMetrics()
			dispatchedAt := fastPathTestNow.Add(-time.Minute)
			item := seedDispatchedWork(store, 10, "miner-1", scenario.desiredState, "batch-a", dispatchedAt)
			durable := store.targetsByEventID[10][0]
			stale := copyTargetForTick(durable)

			batchB := "batch-b"
			durable.LastBatchUUID = &batchB
			if scenario.desiredState == models.DesiredStateActive {
				durable.RestorePhase.BatchUUID = &batchB
			} else {
				durable.CurtailPhase.BatchUUID = &batchB
			}

			r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
			ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: item.EventState}
			runFullTickPositiveConfirmation(r, ev, &stale, scenario)

			assert.Equal(t, models.TargetStateDispatched, durable.State)
			assert.Equal(t, "batch-b", *phaseBatchUUIDForTest(durable),
				"batch-B dispatch must survive batch-A confirmation evidence")
			assert.Equal(t, 1, metrics.EventStateRaceLossCount())
			_, _, params := store.lastWrite()
			require.NotNil(t, params.ExpectedDispatchBatchUUID)
			assert.Equal(t, "batch-a", *params.ExpectedDispatchBatchUUID)
		})
	}
}

// --- tick timeout aging vs. a pulse-confirmed target ---

// A dispatch-timeout aging write must not clobber a target the confirmation
// pulse already confirmed. The tick acts on a stale in-memory snapshot that
// still says dispatched; the guarded write must race-lose against the durable
// row the pulse advanced, leaving state and retry budget intact.
func TestConfirmOneDispatched_TimeoutAgingRaceLosesToPulseConfirmation(t *testing.T) {
	store := newConfirmationFakeStore()
	metrics := newRecordingMetrics()
	// Dispatched an hour ago: well past the 5s curtail dispatch timeout.
	dispatchedAt := fastPathTestNow.Add(-time.Hour)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)

	// The pulse already confirmed the target: advance the durable store row
	// out of dispatched (batch UUID unchanged).
	store.targetsByEventID[10][0].State = models.TargetStateConfirmed

	r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)

	// The tick still holds a stale snapshot: dispatched, retry budget spent once.
	dispatched := dispatchedAt
	batch := "batch-a"
	stale := &models.Target{
		CurtailmentEventID: 10,
		DeviceIdentifier:   "miner-1",
		State:              models.TargetStateDispatched,
		DesiredState:       models.DesiredStateCurtailed,
		BaselinePowerW:     ptrFloat64(3000),
		LastDispatchedAt:   &dispatched,
		LastBatchUUID:      &batch,
		RetryCount:         1,
		CurtailPhase: models.TargetPhaseSummary{
			Phase:        models.TargetPhaseCurtail,
			State:        models.TargetStateDispatched,
			DispatchedAt: &dispatched,
			BatchUUID:    &batch,
		},
	}
	ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: models.EventStateActive}
	// Telemetry still shows full power, so the tick enters the timeout-aging branch.
	cand := &models.Candidate{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2900), LatestHashRateHS: ptrFloat64(100)}

	r.confirmOneDispatched(context.Background(), ev, stale, cand, models.TargetStateDispatching)

	assert.Equal(t, models.TargetStateConfirmed, store.targetState(10, "miner-1"),
		"pulse-confirmed target must survive the stale timeout-aging write")
	assert.Equal(t, models.TargetStateDispatched, stale.State, "stale snapshot must not mutate on race-loss")
	assert.Equal(t, int32(1), stale.RetryCount, "retry budget must not be burned on race-loss")
	assert.Equal(t, 1, metrics.EventStateRaceLossCount())
	assert.Equal(t, 0, metrics.TargetWriteFailureCount(), "a race-loss is not a write failure")
}

// The restore variant: a pulse-resolved restore target at the retry ceiling
// must not be reverted to RESTORE_FAILED by the tick's stale timeout aging.
func TestConfirmOneRestore_TimeoutAgingRaceLosesToPulseResolution(t *testing.T) {
	store := newConfirmationFakeStore()
	metrics := newRecordingMetrics()
	// Dispatched an hour ago: past the 30s restore dispatch timeout.
	dispatchedAt := fastPathTestNow.Add(-time.Hour)
	item := seedDispatchedWork(store, 20, "miner-r", models.DesiredStateActive, "batch-r", dispatchedAt)

	// The pulse already resolved it.
	store.targetsByEventID[20][0].State = models.TargetStateResolved

	r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)

	// Stale snapshot at the retry ceiling: newRetry hits MaxRetries, so
	// unguarded aging would terminalize a genuinely-restored miner.
	dispatched := dispatchedAt
	batch := "batch-r"
	stale := &models.Target{
		CurtailmentEventID: 20,
		DeviceIdentifier:   "miner-r",
		State:              models.TargetStateDispatched,
		DesiredState:       models.DesiredStateActive,
		BaselinePowerW:     ptrFloat64(3000),
		LastDispatchedAt:   &dispatched,
		LastBatchUUID:      &batch,
		RetryCount:         r.cfg.MaxRetries - 1,
		RestorePhase: &models.TargetPhaseSummary{
			Phase:        models.TargetPhaseRestore,
			State:        models.TargetStateDispatched,
			DispatchedAt: &dispatched,
			BatchUUID:    &batch,
		},
	}
	ev := &models.Event{ID: 20, EventUUID: item.EventUUID, OrgID: 1, State: models.EventStateRestoring}
	// Still below the restore threshold, so the tick enters timeout aging.
	cand := &models.Candidate{DeviceIdentifier: "miner-r", LatestPowerW: ptrFloat64(100)}

	r.confirmOneRestore(context.Background(), ev, stale, cand)

	assert.Equal(t, models.TargetStateResolved, store.targetState(20, "miner-r"),
		"pulse-resolved restore must not be reverted (esp. not to RESTORE_FAILED) by stale timeout aging")
	assert.Equal(t, models.TargetStateDispatched, stale.State)
	assert.Equal(t, r.cfg.MaxRetries-1, stale.RetryCount, "retry budget must survive the race-loss")
	assert.Equal(t, 1, metrics.EventStateRaceLossCount())
	assert.Equal(t, 0, metrics.TargetWriteFailureCount())
}

// A state-only timeout guard is vulnerable to ABA: another fleetd can age
// batch A, redispatch batch B, and return the durable row to dispatched before
// the stale batch-A tick writes. The batch UUID guard must reject that stale
// aging write in both dispatch directions.
func TestTimeoutAging_StaleBatchRaceLosesToRedispatch(t *testing.T) {
	tests := []struct {
		name         string
		desiredState string
	}{
		{
			name:         "curtail redispatch",
			desiredState: models.DesiredStateCurtailed,
		},
		{
			name:         "restore redispatch",
			desiredState: models.DesiredStateActive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newConfirmationFakeStore()
			metrics := newRecordingMetrics()
			dispatchedAt := fastPathTestNow.Add(-time.Hour)
			item := seedDispatchedWork(store, 10, "miner-1", tt.desiredState, "batch-a", dispatchedAt)

			// Preserve the batch-A snapshot, including a deep copy of the
			// restore phase pointer, before advancing the durable row to batch B.
			durable := store.targetsByEventID[10][0]
			stale := copyTargetForTick(durable)
			batchB := "batch-b"
			durable.LastBatchUUID = &batchB
			if tt.desiredState == models.DesiredStateActive {
				durable.RestorePhase.BatchUUID = &batchB
			} else {
				durable.CurtailPhase.BatchUUID = &batchB
			}

			r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
			if tt.desiredState == models.DesiredStateActive {
				stale.RetryCount = r.cfg.MaxRetries - 1
			}
			ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: item.EventState}
			cand := &models.Candidate{
				DeviceIdentifier: "miner-1",
				LatestPowerW:     ptrFloat64(2900),
				LatestHashRateHS: ptrFloat64(100),
			}
			if tt.desiredState == models.DesiredStateActive {
				cand.LatestPowerW = ptrFloat64(100)
				r.confirmOneRestore(context.Background(), ev, &stale, cand)
			} else {
				r.confirmOneDispatched(context.Background(), ev, &stale, cand, models.TargetStateDispatching)
			}

			assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"),
				"batch-B dispatch must survive stale batch-A timeout aging")
			assert.Equal(t, int32(0), durable.RetryCount, "batch-B retry budget must remain untouched")
			assert.Equal(t, 1, metrics.EventStateRaceLossCount())
			assert.Equal(t, 0, metrics.TargetWriteFailureCount())
			_, _, params := store.lastWrite()
			require.NotNil(t, params.ExpectedDispatchBatchUUID)
			assert.Equal(t, "batch-a", *params.ExpectedDispatchBatchUUID)
		})
	}
}

func TestTimeoutAging_MissingPhaseBatchRetainsStateGuard(t *testing.T) {
	store := newConfirmationFakeStore()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-time.Hour)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	stale := *store.targetsByEventID[10][0]
	// Migration 000078 intentionally did not backfill phase summaries, so a
	// legacy row can retain rolling last_batch_uuid while its curtail-phase
	// batch token is absent. Only the phase token is safe to use as the guard.
	stale.CurtailPhase.BatchUUID = nil
	stale.RetryCount = 1

	r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
	ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: models.EventStateActive}
	cand := &models.Candidate{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2900), LatestHashRateHS: ptrFloat64(100)}

	r.confirmOneDispatched(context.Background(), ev, &stale, cand, models.TargetStateDispatching)

	assert.Equal(t, 1, store.confirmCalls())
	assert.Equal(t, models.TargetStateDispatching, store.targetState(10, "miner-1"),
		"a legacy dispatched row without a phase batch token must still age out")
	assert.Equal(t, int32(2), stale.RetryCount)
	assert.Equal(t, 0, metrics.EventStateRaceLossCount())
	assert.Equal(t, 0, metrics.TargetWriteFailureCount())
	_, _, params := store.lastWrite()
	require.NotNil(t, params.ExpectedState)
	assert.Nil(t, params.ExpectedDispatchBatchUUID)
}

// Positive control: when the target is still dispatched (no pulse race), the
// dispatched-state and batch guards are transparent and normal timeout aging
// proceeds.
func TestConfirmOneDispatched_TimeoutAgingProceedsWhenStillDispatched(t *testing.T) {
	store := newConfirmationFakeStore()
	metrics := newRecordingMetrics()
	dispatchedAt := fastPathTestNow.Add(-time.Hour)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)

	r := newFastPathReconcilerForTest(store, newFakeSampler(), metrics)
	// Use the durable row as the tick's snapshot: it is still dispatched, so
	// the guard matches and the aging write applies.
	stale := store.targetsByEventID[10][0]
	ev := &models.Event{ID: 10, EventUUID: item.EventUUID, OrgID: 1, State: models.EventStateActive}
	cand := &models.Candidate{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(2900), LatestHashRateHS: ptrFloat64(100)}

	r.confirmOneDispatched(context.Background(), ev, stale, cand, models.TargetStateDispatching)

	assert.Equal(t, models.TargetStateDispatching, store.targetState(10, "miner-1"),
		"a still-dispatched target ages normally through the guarded write")
	assert.Equal(t, int32(1), store.targetsByEventID[10][0].RetryCount, "normal aging burns one retry")
	assert.Equal(t, 0, metrics.EventStateRaceLossCount(), "no race when the target is still dispatched")
	_, _, params := store.lastWrite()
	require.NotNil(t, params.ExpectedDispatchBatchUUID)
	assert.Equal(t, "batch-a", *params.ExpectedDispatchBatchUUID)
}
