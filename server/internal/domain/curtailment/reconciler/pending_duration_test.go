package reconciler

import (
	"errors"
	"testing"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestPendingDurationCap_RestoresPartialCurtailmentBeforeFurtherDispatch(t *testing.T) {
	for _, failTransition := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore succeeds", true: "restore retries without dispatch"}[failTransition], func(t *testing.T) {
			store := newFakeStore()
			dispatcher := &fakeDispatcher{}
			cap := int32(60)
			now := time.Now()
			recentDispatch := now.Add(-time.Second)
			event := &models.Event{ID: 1, OrgID: 1, EventUUID: uuid.New(), State: models.EventStatePending,
				CreatedAt: now.Add(-2 * time.Minute), MaxDurationSeconds: &cap}
			store.events = []*models.Event{event}
			store.targetsByEventID[1] = []*models.Target{
				{DeviceIdentifier: "curtailed", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed, LastDispatchedAt: &recentDispatch},
				{DeviceIdentifier: "never-confirmed", State: models.TargetStateDispatched, DesiredState: models.DesiredStateCurtailed, LastDispatchedAt: &recentDispatch},
				{DeviceIdentifier: "not-sent", State: models.TargetStatePending, DesiredState: models.DesiredStateCurtailed},
			}
			if failTransition {
				store.beginRestoreErr = errors.New("database unavailable")
			}
			r := newReconcilerForTest(store, dispatcher)
			r.now = func() time.Time { return now }
			r.dispatchPending(t.Context(), event)
			assert.Equal(t, 1, store.beginRestoreCalls)
			assert.Zero(t, dispatcher.curtailCalls)
			if !failTransition {
				assert.Equal(t, models.EventStateRestoring, event.State)
				assert.Equal(t, models.DesiredStateActive, store.targetsByEventID[1][0].DesiredState)
				assert.Equal(t, models.DesiredStateActive, store.targetsByEventID[1][1].DesiredState)
				assert.Equal(t, models.TargetStateReleased, store.targetsByEventID[1][2].State)
			}
		})
	}
}

func TestPendingDurationCap_RespectsUnboundedAndUnexpiredEvents(t *testing.T) {
	store := newFakeStore()
	r := newReconcilerForTest(store, &fakeDispatcher{})
	now := time.Now()
	r.now = func() time.Time { return now }
	cap := int32(60)
	for _, event := range []*models.Event{
		{State: models.EventStatePending, CreatedAt: now.Add(-time.Second), MaxDurationSeconds: &cap},
		{State: models.EventStatePending, CreatedAt: now.Add(-time.Hour), MaxDurationSeconds: &cap, AllowUnbounded: true},
	} {
		assert.False(t, r.enforceMaxDuration(t.Context(), event, nil))
	}
	assert.Zero(t, store.beginRestoreCalls)
}
