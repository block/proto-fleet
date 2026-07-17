package reconciler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
)

func TestReconciler_ActiveFanOffWaitsForDelayAndConfirmedTargetsThenReasserts(t *testing.T) {
	store := newFakeStore()
	dispatcher := &fakeDispatcher{}
	fans := &fakeFanController{}
	r := newReconcilerWithFansForTest(store, dispatcher, fans)

	startedAt := r.now().Add(-29 * time.Second)
	event := &models.Event{
		ID:                   81,
		EventUUID:            uuid.New(),
		OrgID:                1,
		State:                models.EventStateActive,
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
		BaselinePowerW:     ptrFloat64(3000),
	}}
	store.candidates = []*models.Candidate{{
		DeviceIdentifier: "miner-1",
		LatestPowerW:     ptrFloat64(50),
		LatestHashRateHS: ptrFloat64(0),
	}}

	r.runTick(context.Background())
	assert.Empty(t, fans.powers)
	assert.Nil(t, event.FanOffSentAt)

	startedAt = r.now().Add(-30 * time.Second)
	r.runTick(context.Background())
	require.Equal(t, []driver.PowerMode{driver.PowerOff}, fans.powers)
	require.NotNil(t, event.FanOffSentAt)

	r.runTick(context.Background())
	assert.Equal(t, []driver.PowerMode{driver.PowerOff, driver.PowerOff}, fans.powers)
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
