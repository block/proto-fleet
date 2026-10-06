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
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
)

func TestReconciler_ClosedLoopFanOffDelayStartsAtFirstTargetConfirmation(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)

	startedAt := r.now().Add(-time.Hour)
	confirmedAt := r.now().Add(-29 * time.Second)
	event := &models.Event{
		ID:                   81,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		Mode:                 models.ModeFullFleet,
		LoopType:             models.LoopTypeClosed,
		ScopeType:            models.ScopeTypeWholeOrg,
		StartedAt:            &startedAt,
		FacilityFanDeviceIDs: []int64{31},
		FanOffDelaySec:       30,
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-1",
		DesiredState:       models.DesiredStateCurtailed,
		State:              models.TargetStateConfirmed,
		ConfirmedAt:        &confirmedAt,
		BaselinePowerW:     ptrFloat64(3000),
	}}
	metricsAt := r.now()
	store.candidates = []*models.Candidate{{
		DeviceIdentifier: "miner-1",
		LatestMetricsAt:  &metricsAt,
		LatestPowerW:     ptrFloat64(50),
		LatestHashRateHS: ptrFloat64(0),
	}}

	r.runTick(context.Background())
	assert.Empty(t, fans.powers)
	assert.Nil(t, event.FanOffSentAt)

	confirmedAt = r.now().Add(-30 * time.Second)
	r.runTick(context.Background())
	require.Equal(t, []driver.PowerMode{driver.PowerOff}, fans.powers)
	require.NotNil(t, event.FanOffSentAt)

	r.runTick(context.Background())
	assert.Equal(t, []driver.PowerMode{driver.PowerOff, driver.PowerOff}, fans.powers)
}

func TestReconciler_LostFanOffResultStillRequiresAirflowRecovery(t *testing.T) {
	store := newFakeStore()
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, &fakeDispatcher{}, fans)
	confirmedAt := r.now().Add(-time.Minute)
	event := &models.Event{
		ID:                   83,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		FacilityFanDeviceIDs: []int64{31},
	}
	target := &models.Target{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-1",
		DesiredState:       models.DesiredStateCurtailed,
		State:              models.TargetStateConfirmed,
		ConfirmedAt:        &confirmedAt,
	}
	store.events = []*models.Event{event}
	store.failFanUpdateCall = 2 // Lose the write after the physical OFF succeeds.

	assert.False(t, r.reconcileActiveFans(t.Context(), event, []*models.Target{target}))
	assert.Equal(t, []driver.PowerMode{driver.PowerOff}, fans.powers)
	require.NotNil(t, event.FanOffSentAt,
		"the durable pre-command intent must preserve the possibility that fans are off")

	target.State = models.TargetStateDispatching
	assert.True(t, r.reconcileActiveFans(t.Context(), event, []*models.Target{target}))
	assert.Equal(t, []driver.PowerMode{driver.PowerOff, driver.PowerOn}, fans.powers)
}

func TestReconciler_ActiveFanReassertionReopensOnCandidateQueryFailure(t *testing.T) {
	store := newFakeStore()
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, &fakeDispatcher{}, fans)
	confirmedAt := r.now().Add(-time.Minute)
	fanOffAt := r.now().Add(-time.Minute)
	event := &models.Event{
		ID:                   91,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		FacilityFanDeviceIDs: []int64{31},
		FanOffSentAt:         &fanOffAt,
		FanOffDelaySec:       30,
	}
	target := &models.Target{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-1",
		DesiredState:       models.DesiredStateCurtailed,
		State:              models.TargetStateConfirmed,
		ConfirmedAt:        &confirmedAt,
		BaselinePowerW:     ptrFloat64(3000),
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{target}
	store.listCandidatesErr = errors.New("candidate query failed")

	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	require.NotNil(t, event.FanAirflowReopenedAt)
	reopenedAt := *event.FanAirflowReopenedAt

	freshAt := reopenedAt.Add(5 * time.Second)
	r.now = func() time.Time { return freshAt }
	store.listCandidatesErr = nil
	metricsAt := freshAt
	store.candidates = []*models.Candidate{{
		DeviceIdentifier: "miner-1",
		LatestMetricsAt:  &metricsAt,
		LatestPowerW:     ptrFloat64(50),
		LatestHashRateHS: ptrFloat64(0),
	}}

	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	require.NotNil(t, target.ConfirmedAt)
	assert.Equal(t, freshAt, *target.ConfirmedAt)

	r.now = func() time.Time { return freshAt.Add(30 * time.Second) }
	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn, driver.PowerOff}, fans.powers)
	assert.Nil(t, event.FanAirflowReopenedAt)
}

func TestReconciler_ActiveFanReassertionReopensOnStaleTelemetry(t *testing.T) {
	store := newFakeStore()
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, &fakeDispatcher{}, fans)
	confirmedAt := r.now().Add(-time.Minute)
	fanOffAt := r.now().Add(-time.Minute)
	event := &models.Event{
		ID:                   92,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		FacilityFanDeviceIDs: []int64{31},
		FanOffSentAt:         &fanOffAt,
		FanOffDelaySec:       30,
	}
	target := &models.Target{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-1",
		DesiredState:       models.DesiredStateCurtailed,
		State:              models.TargetStateConfirmed,
		ConfirmedAt:        &confirmedAt,
		BaselinePowerW:     ptrFloat64(3000),
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{target}
	store.candidates = []*models.Candidate{{
		DeviceIdentifier: "miner-1",
	}}

	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	require.NotNil(t, event.FanAirflowReopenedAt)
	assert.Equal(t, confirmedAt, *target.ConfirmedAt)
}

func TestReconciler_ActiveFanOffDoesNotUseTargetlessClosedLoopWatcher(t *testing.T) {
	store := newFakeStore()
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, &fakeDispatcher{}, fans)
	startedAt := r.now().Add(-time.Minute)
	event := &models.Event{
		ID:                   82,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		Mode:                 models.ModeFullFleet,
		LoopType:             models.LoopTypeClosed,
		ScopeType:            models.ScopeTypeWholeOrg,
		StartedAt:            &startedAt,
		FacilityFanDeviceIDs: []int64{31},
	}
	store.events = []*models.Event{event}

	r.runTick(context.Background())

	assert.Empty(t, fans.powers)
	assert.Nil(t, event.FanOffSentAt)
}

func TestDriveTopologyRestoresUsesFansThatWereNeverTurnedOff(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)
	event := &models.Event{
		ID:                   84,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		FacilityFanDeviceIDs: []int64{31},
		FanRestoreDelaySec:   30,
	}
	target := &models.Target{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "departed-miner",
		DesiredState:       models.DesiredStateActive,
		State:              models.TargetStatePending,
		RestorePhase: &models.TargetPhaseSummary{
			Phase: models.TargetPhaseRestore,
			State: models.TargetStatePending,
		},
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{target}

	assert.True(t, r.driveTopologyRestores(t.Context(), event, []*models.Target{target}))
	assert.Empty(t, fans.powers, "fans that were never turned off do not need an ON command")
	assert.Equal(t, 1, dispatcher.uncurtailCalls)
	assert.Equal(t, models.TargetStateDispatched, target.State)
}

func TestDriveTopologyRestoresPreservesFanRestoreDelayBoundary(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)
	fanOffAt := r.now().Add(-time.Minute)
	event := &models.Event{
		ID:                   89,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		FacilityFanDeviceIDs: []int64{31},
		FanOffSentAt:         &fanOffAt,
		FanRestoreDelaySec:   30,
	}
	target := &models.Target{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "departed-miner",
		DesiredState:       models.DesiredStateActive,
		State:              models.TargetStatePending,
		RestorePhase: &models.TargetPhaseSummary{
			Phase: models.TargetPhaseRestore,
			State: models.TargetStatePending,
		},
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{target}

	assert.True(t, r.driveTopologyRestores(t.Context(), event, []*models.Target{target}))
	require.NotNil(t, event.FanAirflowReopenedAt)
	reopenedAt := *event.FanAirflowReopenedAt
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Zero(t, dispatcher.uncurtailCalls)

	r.now = func() time.Time { return reopenedAt.Add(15 * time.Second) }
	assert.True(t, r.driveTopologyRestores(t.Context(), event, []*models.Target{target}))
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Zero(t, dispatcher.uncurtailCalls)

	r.now = func() time.Time { return reopenedAt.Add(30 * time.Second) }
	assert.True(t, r.driveTopologyRestores(t.Context(), event, []*models.Target{target}))
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Equal(t, 1, dispatcher.uncurtailCalls)
}

func TestReconciler_PartialRestoreFailureKeepsFacilityFansOn(t *testing.T) {
	store := newFakeStore()
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, &fakeDispatcher{}, fans)
	fanOffAt := r.now().Add(-time.Minute)
	event := &models.Event{
		ID:                   85,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		FacilityFanDeviceIDs: []int64{31},
		FanOffSentAt:         &fanOffAt,
	}
	targets := []*models.Target{
		{
			CurtailmentEventID: event.ID,
			DeviceIdentifier:   "in-scope-miner",
			DesiredState:       models.DesiredStateCurtailed,
			State:              models.TargetStateConfirmed,
		},
		{
			CurtailmentEventID: event.ID,
			DeviceIdentifier:   "departed-miner",
			DesiredState:       models.DesiredStateActive,
			State:              models.TargetStateRestoreFailed,
		},
	}
	store.events = []*models.Event{event}

	assert.True(t, r.reconcileActiveFans(t.Context(), event, targets))
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	require.NotNil(t, event.FanAirflowReopenedAt)
}

func TestReconciler_ClosedLoopReopensFansBeforeDispatchingNewTarget(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	fanError := "fan ON failed"
	fans := &fakeFanController{err: &fanError}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)

	startedAt := r.now().Add(-time.Minute)
	fanOffAt := r.now().Add(-30 * time.Second)
	event := &models.Event{
		ID:                   86,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		Mode:                 models.ModeFullFleet,
		LoopType:             models.LoopTypeClosed,
		ScopeType:            models.ScopeTypeWholeOrg,
		StartedAt:            &startedAt,
		FacilityFanDeviceIDs: []int64{31},
		FanOffSentAt:         &fanOffAt,
		FanOffDelaySec:       30,
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-existing",
		DesiredState:       models.DesiredStateCurtailed,
		State:              models.TargetStateConfirmed,
		ConfirmedAt:        &startedAt,
		BaselinePowerW:     ptrFloat64(3000),
	}}
	driverName := "antminer"
	metricsAt := r.now()
	newCandidate := &models.Candidate{
		DeviceIdentifier: "miner-new",
		DriverName:       &driverName,
		DeviceStatus:     "ACTIVE",
		PairingStatus:    "PAIRED",
		LatestMetricsAt:  &metricsAt,
		LatestPowerW:     ptrFloat64(3000),
		LatestHashRateHS: ptrFloat64(100),
		AvgEfficiencyJH:  ptrFloat64(40),
	}
	store.candidates = []*models.Candidate{
		{
			DeviceIdentifier: "miner-existing",
			DriverName:       &driverName,
			DeviceStatus:     "ACTIVE",
			PairingStatus:    "PAIRED",
			LatestMetricsAt:  &metricsAt,
			LatestPowerW:     ptrFloat64(50),
			LatestHashRateHS: ptrFloat64(0),
			AvgEfficiencyJH:  ptrFloat64(40),
		},
		newCandidate,
	}
	dispatcher.curtailHook = func(ids []string) {
		assert.Equal(t, []driver.PowerMode{driver.PowerOn, driver.PowerOn}, fans.powers,
			"airflow must reopen before the newly admitted miner is dispatched")
		assert.Equal(t, []string{"miner-new"}, ids)
	}

	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Zero(t, dispatcher.curtailCalls, "a failed fan ON must hold dynamic dispatch")
	require.Len(t, store.targetsByEventID[event.ID], 2)
	assert.Equal(t, models.TargetStateDispatching, store.targetsByEventID[event.ID][1].State)

	fans.err = nil
	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn, driver.PowerOn}, fans.powers)
	assert.Equal(t, 1, dispatcher.curtailCalls)
	assert.Equal(t, models.TargetStateDispatched, store.targetsByEventID[event.ID][1].State)

	newCandidate.LatestPowerW = ptrFloat64(50)
	newCandidate.LatestHashRateHS = ptrFloat64(0)
	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn, driver.PowerOn}, fans.powers,
		"confirmation must start a fresh cooling delay before fans turn off")
	assert.Equal(t, models.TargetStateConfirmed, store.targetsByEventID[event.ID][1].State)
	require.NotNil(t, event.FanAirflowReopenedAt)
	reopenedAt := *event.FanAirflowReopenedAt
	r.now = func() time.Time { return reopenedAt.Add(30 * time.Second) }

	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn, driver.PowerOn, driver.PowerOff}, fans.powers)
	assert.Nil(t, event.FanAirflowReopenedAt, "the active-airflow marker clears after fans turn off again")
}

func TestReconciler_TopologyAdmissionWaitsForAirflowDelay(t *testing.T) {
	for _, allPaired := range []bool{false, true} {
		name := "selected miners"
		if allPaired {
			name = "all paired miners"
		}
		t.Run(name, func(t *testing.T) {
			store, event := topologyAdmissionTestFixture(topologyAdmissionTestBuildingScope)
			dispatcher := &fakeDispatcher{}
			fans := &fakeFanController{}
			r := newReconcilerWithFansForTest(
				store,
				dispatcher,
				fans,
				WithDispatchPermissionResolver(staticDispatchPermissionResolver{
					effective: topologyDispatchManagePermission(),
				}),
			)

			startedAt := r.now().Add(-time.Minute)
			fanOffAt := r.now().Add(-30 * time.Second)
			lastCurtailBatchAt := r.now()
			curtailBatchSize := int32(1)
			event.StartedAt = &startedAt
			event.FacilityFanDeviceIDs = []int64{31}
			event.FanOffSentAt = &fanOffAt
			event.FanOffDelaySec = 30
			event.FanRestoreDelaySec = 30
			event.ForceIncludeAllPairedMiners = allPaired
			event.CurtailBatchSize = &curtailBatchSize
			event.CurtailBatchIntervalSec = 20
			event.LastCurtailPendingDispatchAt = &lastCurtailBatchAt
			store.targetsByEventID[event.ID] = []*models.Target{{
				CurtailmentEventID: event.ID,
				DeviceIdentifier:   "miner-existing",
				DesiredState:       models.DesiredStateCurtailed,
				State:              models.TargetStateConfirmed,
				ConfirmedAt:        &startedAt,
				BaselinePowerW:     ptrFloat64(3000),
			}}
			driverName := "antminer"
			metricsAt := r.now()
			store.candidates = []*models.Candidate{
				{
					DeviceIdentifier: "miner-existing",
					DriverName:       &driverName,
					DeviceStatus:     "ACTIVE",
					PairingStatus:    "PAIRED",
					LatestMetricsAt:  &metricsAt,
					LatestPowerW:     ptrFloat64(50),
					LatestHashRateHS: ptrFloat64(0),
					AvgEfficiencyJH:  ptrFloat64(40),
				},
				{
					DeviceIdentifier: "miner-new",
					DriverName:       &driverName,
					DeviceStatus:     "ACTIVE",
					PairingStatus:    "PAIRED",
					LatestMetricsAt:  &metricsAt,
					LatestPowerW:     ptrFloat64(3000),
					LatestHashRateHS: ptrFloat64(100),
					AvgEfficiencyJH:  ptrFloat64(40),
				},
			}

			r.runTick(t.Context())

			assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
			assert.True(t, r.now().Sub(lastCurtailBatchAt) < 20*time.Second,
				"airflow must reopen even while topology admission pacing is active")
			assert.Zero(t, store.claimTargetsCalls, "the target must remain unclaimed while airflow starts")
			assert.Zero(t, store.claimAllPairedCalls)
			assert.Zero(t, dispatcher.curtailCalls)
			reopenedAt := *event.FanAirflowReopenedAt

			r.now = func() time.Time { return reopenedAt.Add(29 * time.Second) }
			r.runTick(t.Context())

			assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
			assert.Zero(t, store.claimTargetsCalls)
			assert.Zero(t, store.claimAllPairedCalls)
			assert.Zero(t, dispatcher.curtailCalls)

			r.now = func() time.Time { return reopenedAt.Add(30 * time.Second) }
			r.runTick(t.Context())

			assert.NotContains(t, fans.powers, driver.PowerOff,
				"admission must not turn fans off before the new target is observed")
			if allPaired {
				assert.Equal(t, 1, store.claimAllPairedCalls)
				assert.Zero(t, dispatcher.curtailCalls, "all-paired rows dispatch on the following pass")
				r.now = func() time.Time { return reopenedAt.Add(31 * time.Second) }
				r.runTick(t.Context())
			} else {
				assert.Equal(t, 1, store.claimTargetsCalls)
			}

			assert.Equal(t, 1, dispatcher.curtailCalls)
			assert.Equal(t, []string{"miner-new"}, dispatcher.curtailLastIDs)
		})
	}
}

func TestReconciler_DriftRedispatchWaitsForSuccessfulFanOn(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	fanError := "fan ON failed"
	fans := &fakeFanController{err: &fanError}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)
	fanOffAt := r.now().Add(-time.Minute)
	event := &models.Event{
		ID:                   87,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		FacilityFanDeviceIDs: []int64{31},
		FanOffSentAt:         &fanOffAt,
	}
	target := &models.Target{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-drifted",
		DesiredState:       models.DesiredStateCurtailed,
		State:              models.TargetStateConfirmed,
		BaselinePowerW:     ptrFloat64(3000),
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{target}
	metricsAt := r.now()
	store.candidates = []*models.Candidate{{
		DeviceIdentifier: "miner-drifted",
		LatestMetricsAt:  &metricsAt,
		LatestPowerW:     ptrFloat64(3000),
		LatestHashRateHS: ptrFloat64(100),
	}}

	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Zero(t, dispatcher.curtailCalls)
	assert.Equal(t, models.TargetStateDrifted, target.State)
	require.NotNil(t, event.FanAirflowReopenedAt)
	firstAttemptAt := *event.FanAirflowReopenedAt

	secondAttemptAt := firstAttemptAt.Add(30 * time.Second)
	r.now = func() time.Time { return secondAttemptAt }
	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn, driver.PowerOn}, fans.powers)
	assert.Zero(t, dispatcher.curtailCalls)
	assert.Equal(t, firstAttemptAt, *event.FanAirflowReopenedAt,
		"failed retries must preserve the first alert deadline")

	fans.err = nil
	successAt := firstAttemptAt.Add(time.Minute)
	r.now = func() time.Time { return successAt }
	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn, driver.PowerOn, driver.PowerOn}, fans.powers)
	assert.Equal(t, 1, dispatcher.curtailCalls)
	assert.Equal(t, models.TargetStateDispatched, target.State)
	assert.Equal(t, successAt, *event.FanAirflowReopenedAt,
		"successful airflow reopening starts the cooling delay")
}

func TestReconciler_RecurtailedPendingEventRetriesFanOnBeforeMinerDispatch(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	fanError := "fan ON failed"
	fans := &fakeFanController{err: &fanError}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)
	fanOffAt := r.now().Add(-2 * time.Minute)
	reopenedAt := r.now().Add(-time.Minute)
	event := &models.Event{
		ID:                   88,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStatePending,
		FacilityFanDeviceIDs: []int64{31},
		FanOffSentAt:         &fanOffAt,
		FanAirflowReopenedAt: &reopenedAt,
		FanLastError:         &fanError,
	}
	target := &models.Target{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-recurtailed",
		DesiredState:       models.DesiredStateCurtailed,
		State:              models.TargetStatePending,
		BaselinePowerW:     ptrFloat64(3000),
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{target}

	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Zero(t, dispatcher.curtailCalls)
	assert.Equal(t, &reopenedAt, event.FanAirflowReopenedAt,
		"failed retries must preserve the original alert gate")

	fans.err = nil
	successAt := r.now().Add(30 * time.Second)
	r.now = func() time.Time { return successAt }
	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn, driver.PowerOn}, fans.powers)
	assert.Equal(t, 1, dispatcher.curtailCalls)
	assert.Nil(t, event.FanLastError)
	assert.Equal(t, &successAt, event.FanAirflowReopenedAt,
		"successful pending recovery starts a fresh cooling delay")
}

func TestReconciler_PendingTopologyFanRecoveryPrecedesAdmissionPreflight(t *testing.T) {
	store, event := topologyAdmissionTestFixture(topologyAdmissionTestBuildingScope)
	store.topologyCoverageErr = errors.New("topology unavailable")
	event.State = models.EventStatePending
	event.FacilityFanDeviceIDs = []int64{31}
	fanOffAt := time.Now().Add(-time.Minute)
	event.FanOffSentAt = &fanOffAt
	fanError := "fan ON failed"
	event.FanLastError = &fanError
	target := &models.Target{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "topology-miner",
		DesiredState:       models.DesiredStateCurtailed,
		State:              models.TargetStatePending,
	}
	store.targetsByEventID[event.ID] = []*models.Target{target}
	dispatcher := &fakeDispatcher{}
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)

	r.dispatchPending(t.Context(), event)

	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Nil(t, event.FanLastError)
	assert.Zero(t, dispatcher.curtailCalls)
}

func TestReconciler_RestoringFanTimeoutReservesTimeForMinerRestore(t *testing.T) {
	store := newFakeStore()
	store.rejectExpiredFanUpdate = true
	dispatcher := &fakeDispatcher{}
	fans := &fakeFanController{waitForCancellation: true}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)
	event := &models.Event{
		ID:                   89,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateRestoring,
		RestoreBatchSize:     1,
		FacilityFanDeviceIDs: []int64{31},
	}
	target := &models.Target{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-restore",
		DesiredState:       models.DesiredStateActive,
		State:              models.TargetStatePending,
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{target}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	r.observeRestoring(ctx, event)

	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	require.NotNil(t, event.FanOnSentAt, "fan timeout evidence must persist before the hardware context expires")
	require.NotNil(t, event.FanLastError)
	assert.Equal(t, "fan command timed out", *event.FanLastError)
	assert.Equal(t, 1, dispatcher.uncurtailCalls,
		"the fan timeout must leave event-budget time for fail-open miner restoration")
}

func TestReconciler_ActiveFanOffWaitsWhenUnavailableMinerMayStillBeHashing(t *testing.T) {
	store := newFakeStore()
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, &fakeDispatcher{}, fans)
	startedAt := r.now().Add(-time.Minute)
	event := &models.Event{
		ID:                   85,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
		StartedAt:            &startedAt,
		FacilityFanDeviceIDs: []int64{31},
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{
		{CurtailmentEventID: event.ID, DeviceIdentifier: "confirmed", DesiredState: models.DesiredStateCurtailed, State: models.TargetStateConfirmed},
		{CurtailmentEventID: event.ID, DeviceIdentifier: "unavailable", DesiredState: models.DesiredStateCurtailed, State: models.TargetStateUnavailable},
	}

	r.runTick(context.Background())

	assert.Empty(t, fans.powers)
	assert.Nil(t, event.FanOffSentAt)
}

func TestReconciler_RestoreTurnsFansOnBeforeMinerDelayAndReasserts(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)

	event := &models.Event{
		ID:                   83,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateRestoring,
		RestoreBatchSize:     1,
		FacilityFanDeviceIDs: []int64{31},
		FanRestoreDelaySec:   60,
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-1",
		DesiredState:       models.DesiredStateActive,
		State:              models.TargetStatePending,
	}}

	r.runTick(context.Background())
	require.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	require.NotNil(t, event.FanOnSentAt)
	assert.Zero(t, dispatcher.uncurtailCalls)

	firstFanOn := *event.FanOnSentAt
	r.now = func() time.Time { return firstFanOn.Add(60 * time.Second) }
	r.runTick(context.Background())

	assert.Equal(t, []driver.PowerMode{driver.PowerOn, driver.PowerOn}, fans.powers)
	assert.Equal(t, 1, dispatcher.uncurtailCalls)
}

func TestReconciler_RestoreCoolingDelayStartsAtFirstSuccessfulFanOn(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	failure := "device 31: command failed"
	fans := &fakeFanController{err: &failure}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)
	firstAttemptAt := r.now()
	oldActiveAirflowAt := firstAttemptAt.Add(-time.Minute)

	event := &models.Event{
		ID:                   90,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateRestoring,
		RestoreBatchSize:     1,
		FacilityFanDeviceIDs: []int64{31},
		FanRestoreDelaySec:   60,
		FanAirflowReopenedAt: &oldActiveAirflowAt,
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-1",
		DesiredState:       models.DesiredStateActive,
		State:              models.TargetStatePending,
	}}

	r.runTick(context.Background())
	require.Equal(t, &firstAttemptAt, event.FanOnSentAt)
	assert.Nil(t, event.FanAirflowReopenedAt, "a failed restore must clear active-phase airflow evidence")
	assert.Zero(t, dispatcher.uncurtailCalls)

	successAt := firstAttemptAt.Add(59 * time.Second)
	r.now = func() time.Time { return successAt }
	fans.err = nil
	r.runTick(context.Background())
	require.Equal(t, &successAt, event.FanAirflowReopenedAt)
	assert.Zero(t, dispatcher.uncurtailCalls)

	r.now = func() time.Time { return firstAttemptAt.Add(60 * time.Second) }
	r.runTick(context.Background())
	assert.Zero(t, dispatcher.uncurtailCalls,
		"the original fail-open deadline must not bypass cooling after airflow recovers")
	assert.Equal(t, &successAt, event.FanAirflowReopenedAt,
		"successful reassertions must preserve the first confirmed airflow timestamp")

	r.now = func() time.Time { return successAt.Add(60 * time.Second) }
	r.runTick(context.Background())
	assert.Equal(t, 1, dispatcher.uncurtailCalls)
}

func TestReconciler_RestoreAlertsWhenFanOnFailureReachesMinerGateAndClearsOnRecovery(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	failure := "device 31: command failed"
	fans := &fakeFanController{err: &failure}
	alert := &fakeFanAlertEmitter{}
	r := newReconcilerWithFanAlertForTest(store, dispatcher, fans, alert)

	event := &models.Event{
		ID:                   84,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateRestoring,
		RestoreBatchSize:     1,
		FacilityFanDeviceIDs: []int64{31},
		FanRestoreDelaySec:   60,
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-1",
		DesiredState:       models.DesiredStateActive,
		State:              models.TargetStatePending,
	}}

	r.runTick(context.Background())
	require.NotNil(t, event.FanOnSentAt)
	assert.Empty(t, alert.values)

	r.now = func() time.Time { return event.FanOnSentAt.Add(60 * time.Second) }
	r.runTick(context.Background())
	assert.Equal(t, []bool{true}, alert.values)
	assert.Equal(t, 1, dispatcher.uncurtailCalls, "fan failure remains fail-open after the configured delay")

	fans.err = nil
	r.runTick(context.Background())
	assert.Equal(t, []bool{true, false}, alert.values)
}

func TestReconciler_RestoreAlertDelayStartsAtFirstSuccessfulFanOn(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	failure := "device 31: command failed"
	fans := &fakeFanController{err: &failure}
	alert := &fakeFanAlertEmitter{}
	r := newReconcilerWithFanAlertForTest(store, dispatcher, fans, alert)
	firstAttemptAt := r.now()
	oldActiveAirflowAt := firstAttemptAt.Add(-time.Minute)

	event := &models.Event{
		ID:                   91,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateRestoring,
		RestoreBatchSize:     1,
		FacilityFanDeviceIDs: []int64{31},
		FanRestoreDelaySec:   60,
		FanAirflowReopenedAt: &oldActiveAirflowAt,
	}
	store.events = []*models.Event{event}
	store.targetsByEventID[event.ID] = []*models.Target{{
		CurtailmentEventID: event.ID,
		DeviceIdentifier:   "miner-1",
		DesiredState:       models.DesiredStateActive,
		State:              models.TargetStatePending,
	}}

	r.runTick(context.Background())
	require.Equal(t, &firstAttemptAt, event.FanOnSentAt)
	assert.Nil(t, event.FanAirflowReopenedAt, "a failed restore must clear active-phase airflow evidence")
	assert.Empty(t, alert.values)
	assert.Zero(t, dispatcher.uncurtailCalls)

	successAt := firstAttemptAt.Add(59 * time.Second)
	r.now = func() time.Time { return successAt }
	fans.err = nil
	r.runTick(context.Background())
	require.Equal(t, &successAt, event.FanAirflowReopenedAt)
	assert.Equal(t, []bool{false}, alert.values)
	assert.Zero(t, dispatcher.uncurtailCalls)

	fans.err = &failure
	r.now = func() time.Time { return firstAttemptAt.Add(60 * time.Second) }
	r.runTick(context.Background())
	assert.Equal(t, []bool{false}, alert.values,
		"the original failed attempt must not trigger alerting before the successful airflow delay")
	assert.Zero(t, dispatcher.uncurtailCalls)

	r.now = func() time.Time { return successAt.Add(60 * time.Second) }
	r.runTick(context.Background())
	assert.Equal(t, []bool{false, true}, alert.values)
	assert.Equal(t, 1, dispatcher.uncurtailCalls)
}
