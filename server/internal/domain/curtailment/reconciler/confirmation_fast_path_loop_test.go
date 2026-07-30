package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// --- pulse lifecycle: park, wake, re-park, failure backoff ---

// startConfirmationLoop runs the pulse goroutine in isolation (no tick
// loop) and returns a stop func that cancels it and waits for exit.
func startConfirmationLoop(r *Reconciler) (stop func()) {
	stopCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.confirmationLoop(stopCtx, context.Background())
	}()
	return func() {
		cancel()
		<-done
	}
}

func TestConfirmationLoop_ParksWithoutWorkAndConfirmsAfterWake(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	r := newFastPathReconcilerForTest(store, sampler, nil)

	stop := startConfirmationLoop(r)
	defer stop()

	// Wake with no eligible work: exactly one read, then parked again.
	r.wakeConfirmation()
	require.Eventually(t, func() bool { return store.eligibleCalls() == 1 },
		2*time.Second, time.Millisecond)
	time.Sleep(30 * time.Millisecond) // several pulse intervals
	assert.Equal(t, 1, store.eligibleCalls(), "parked pulse must do zero periodic work")

	// Seed dispatched work and wake: the pulse confirms it, then the row
	// leaves eligibility and the pulse parks again.
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(time.Second)))
	r.wakeConfirmation()

	require.Eventually(t, func() bool {
		return store.targetState(10, "miner-1") == models.TargetStateConfirmed
	}, 2*time.Second, time.Millisecond)

	// Parked again: the eligibility call count stabilizes.
	var settled int
	require.Eventually(t, func() bool {
		calls := store.eligibleCalls()
		if calls == settled {
			return true
		}
		settled = calls
		return false
	}, 2*time.Second, 20*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	assert.Equal(t, settled, store.eligibleCalls(), "pulse must re-park after the last row confirms")
}

func TestConfirmationLoop_RetriesFailedPassesThenRecovers(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	store.setListEligibleErr(errors.New("injected read failure"))
	r := newFastPathReconcilerForTest(store, sampler, nil)

	stop := startConfirmationLoop(r)
	defer stop()

	// Failed passes keep the pulse active (with backoff), not parked.
	r.wakeConfirmation()
	require.Eventually(t, func() bool { return store.eligibleCalls() >= 3 },
		5*time.Second, time.Millisecond, "failed passes must retry")

	// Recovery: the read starts succeeding with work present; the pulse
	// confirms it and parks.
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(time.Second)))
	store.setListEligibleErr(nil)

	require.Eventually(t, func() bool {
		return store.targetState(10, "miner-1") == models.TargetStateConfirmed
	}, 5*time.Second, time.Millisecond)
}

// --- wiring: tick wakes, Start/Stop lifecycle, disabled mode ---

func TestRunTick_DispatchedWorkWakesConfirmationPulse(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	effBatch := int32(2)
	eventID := int64(10)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: uuid.New(), OrgID: 1, State: models.EventStatePending, CurtailBatchSize: &effBatch, EffectiveBatchSize: &effBatch},
	}
	store.targetsByEventID[eventID] = []*models.Target{
		{CurtailmentEventID: eventID, DeviceIdentifier: "miner-1", State: models.TargetStatePending, BaselinePowerW: ptrFloat64(3000)},
	}

	r := newReconcilerForTest(store, disp)
	require.Empty(t, r.confirmationWake, "no wake before the tick")
	r.runTick(context.Background())

	assert.Len(t, r.confirmationWake, 1,
		"a tick that leaves targets dispatched must wake the confirmation pulse")
}

func TestRunTick_NoDispatchedWorkLeavesPulseParked(t *testing.T) {
	store := newFakeStore()
	r := newReconcilerForTest(store, &fakeDispatcher{})
	r.runTick(context.Background())
	assert.Empty(t, r.confirmationWake, "nothing dispatched, no wake")
}

func TestObserveActive_DispatchedWorkWakesConfirmationPulse(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	eventID := int64(10)
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) // == newReconcilerForTest clock
	store.events = []*models.Event{
		{ID: eventID, EventUUID: uuid.New(), OrgID: 1, State: models.EventStateActive},
	}
	// A drifted-then-redispatched target re-entering the active phase in
	// dispatched: freshly dispatched with not-yet-curtailed telemetry, so the
	// tick observes it without confirming or aging and leaves it dispatched.
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-1",
			State:              models.TargetStateDispatched,
			DesiredState:       models.DesiredStateCurtailed,
			BaselinePowerW:     ptrFloat64(3000),
			LastDispatchedAt:   &now,
			CurtailPhase: models.TargetPhaseSummary{
				Phase:        models.TargetPhaseCurtail,
				State:        models.TargetStateDispatched,
				DispatchedAt: &now,
			},
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-1", LatestPowerW: ptrFloat64(3000), LatestHashRateHS: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	require.Empty(t, r.confirmationWake, "no wake before the tick")
	r.runTick(context.Background())

	assert.Len(t, r.confirmationWake, 1,
		"an active-phase tick that leaves a target dispatched must wake the confirmation pulse")
}

func TestObserveRestoring_DispatchedWorkWakesConfirmationPulse(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	eventID := int64(30)
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	store.events = []*models.Event{
		{ID: eventID, EventUUID: uuid.New(), OrgID: 1, State: models.EventStateRestoring},
	}
	// A freshly dispatched restore target whose telemetry has not yet crossed
	// the restore threshold, so the tick leaves it dispatched.
	store.targetsByEventID[eventID] = []*models.Target{
		{
			CurtailmentEventID: eventID,
			DeviceIdentifier:   "miner-r",
			State:              models.TargetStateDispatched,
			DesiredState:       models.DesiredStateActive,
			BaselinePowerW:     ptrFloat64(3000),
			LastDispatchedAt:   &now,
			RestorePhase: &models.TargetPhaseSummary{
				Phase:        models.TargetPhaseRestore,
				State:        models.TargetStateDispatched,
				DispatchedAt: &now,
			},
		},
	}
	store.candidates = []*models.Candidate{
		{DeviceIdentifier: "miner-r", LatestPowerW: ptrFloat64(100)},
	}

	r := newReconcilerForTest(store, disp)
	require.Empty(t, r.confirmationWake, "no wake before the tick")
	r.runTick(context.Background())

	assert.Len(t, r.confirmationWake, 1,
		"a restoring-phase tick that leaves a restore target dispatched must wake the pulse")
}

func TestRunTick_ClaimedClosedLoopTargetWakesConfirmationPulse(t *testing.T) {
	store := newFakeStore()
	disp := &fakeDispatcher{}
	eventID := int64(40)
	store.events = []*models.Event{
		{
			ID:              eventID,
			EventUUID:       uuid.New(),
			OrgID:           1,
			State:           models.EventStateActive,
			Mode:            models.ModeFullFleet,
			LoopType:        models.LoopTypeClosed,
			ScopeType:       models.ScopeTypeWholeOrg,
			CreatedByUserID: 99,
		},
	}
	// No pre-existing targets: the only dispatched work this tick is the
	// dynamically-claimed miner-new, which is a separate slice the deferred
	// wakeIfDispatchedWork(targets) never sees. The explicit claimed-work wake
	// is what must fire here.
	driverName := "antminer"
	now := time.Now()
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "miner-new",
			DriverName:       &driverName,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &now,
			LatestPowerW:     ptrFloat64(100),
			LatestHashRateHS: ptrFloat64(100),
			AvgEfficiencyJH:  ptrFloat64(40),
		},
	}

	r := newReconcilerForTest(store, disp)
	require.Empty(t, r.confirmationWake, "no wake before the tick")
	r.runTick(context.Background())

	require.Len(t, store.targetsByEventID[eventID], 1, "the closed-loop target was claimed")
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[eventID][0].State)
	assert.Len(t, r.confirmationWake, 1,
		"a tick that dispatches only a dynamically-claimed target must wake the pulse")
}

func TestStart_FastPathEnabledRequiresSampler(t *testing.T) {
	r := New(Config{
		TickInterval:                time.Hour,
		ConfirmationFastPathEnabled: true,
	}, newFakeStore(), &fakeDispatcher{})

	err := r.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sampler")
}

func TestStart_RunsStartupRecoveryPass(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	// Rows already dispatched before startup (e.g. crash recovery).
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.setResult("miner-1", confirmationSample("miner-1", 100, fastPathTestNow.Add(time.Second)))

	r := newFastPathReconcilerForTest(store, sampler, nil)
	require.NoError(t, r.Start(context.Background()))
	defer func() { require.NoError(t, r.Stop(context.Background())) }()

	// No tick ran (interval 1h); the initial wake alone must confirm.
	require.Eventually(t, func() bool {
		return store.targetState(10, "miner-1") == models.TargetStateConfirmed
	}, 2*time.Second, time.Millisecond,
		"startup recovery must confirm pre-existing dispatched rows without a tick")
}

func TestStop_CancelsActiveConfirmationPass(t *testing.T) {
	store := newConfirmationFakeStore()
	sampler := newFakeSampler()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)
	sampler.blockUntilCtxDone = true

	r := newFastPathReconcilerForTest(store, sampler, nil)
	r.confirmationPassTimeout = time.Minute
	require.NoError(t, r.Start(context.Background()))
	require.Eventually(t, func() bool {
		return sampler.callCount() == 1
	}, time.Second, time.Millisecond, "startup pass must enter the sampler")

	stopCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	require.NoError(t, r.Stop(stopCtx),
		"Stop must cancel the acceleration pass instead of waiting for its sampling budget")
}

func TestStart_DisabledFastPathRunsNoPulse(t *testing.T) {
	store := newConfirmationFakeStore()
	dispatchedAt := fastPathTestNow.Add(-10 * time.Second)
	item := seedDispatchedWork(store, 10, "miner-1", models.DesiredStateCurtailed, "batch-a", dispatchedAt)
	store.setItems(item)

	r := New(Config{
		TickInterval:         time.Hour,
		MaxRetries:           3,
		CurtailMaxRetries:    3,
		DriftThresholdFactor: 0.5,
		// ConfirmationFastPathEnabled deliberately false; no sampler needed.
	}, store, &fakeDispatcher{})
	require.NoError(t, r.Start(context.Background()))
	defer func() { require.NoError(t, r.Stop(context.Background())) }()

	// Wakes are inert when disabled: nothing consumes them and no
	// eligibility read ever runs.
	r.wakeConfirmation()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, store.eligibleCalls(),
		"disabled fast path must never touch the eligibility read")
	assert.Equal(t, models.TargetStateDispatched, store.targetState(10, "miner-1"))
}
