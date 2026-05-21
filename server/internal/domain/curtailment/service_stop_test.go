package curtailment

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// stopFixture wires a fakeStore + Service with a seeded event + targets so
// each test reads as "given a Stop-eligible event with these properties..."
// rather than fanning the same set-up across every case.
type stopFixture struct {
	store *fakeStore
	svc   *Service
	event *models.Event
}

func newStopFixture(t *testing.T, mutate func(ev *models.Event)) *stopFixture {
	t.Helper()
	store := newFakeStore()
	startedAt := time.Now().Add(-2 * time.Hour)
	ev := &models.Event{
		ID:                      42,
		EventUUID:               uuid.New(),
		OrgID:                   1,
		State:                   models.EventStateActive,
		Mode:                    models.ModeFixedKw,
		Strategy:                models.StrategyLeastEfficientFirst,
		Level:                   models.LevelFull,
		Priority:                models.PriorityNormal,
		RestoreBatchSize:        10,
		RestoreBatchIntervalSec: 120,
		MinCurtailedDurationSec: 0,
		StartedAt:               &startedAt,
		Reason:                  "test stop fixture",
	}
	if mutate != nil {
		mutate(ev)
	}
	store.eventsByUUID[ev.EventUUID] = ev
	store.targetsByEventUUID[ev.EventUUID] = []*models.Target{
		{DeviceIdentifier: "m1", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed},
		{DeviceIdentifier: "m2", State: models.TargetStateConfirmed, DesiredState: models.DesiredStateCurtailed},
	}
	return &stopFixture{store: store, svc: NewService(store), event: ev}
}

func TestService_Stop_ReturnsNotFoundForUnknownUUID(t *testing.T) {
	t.Parallel()
	f := newStopFixture(t, nil)

	_, err := f.svc.Stop(t.Context(), StopRequest{OrgID: 1, EventUUID: uuid.New()})
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, "not_found", fleetErr.GRPCCode.String())
	assert.Equal(t, 0, f.store.beginRestoreCalls)
}

func TestService_Stop_HappyPath(t *testing.T) {
	t.Parallel()
	f := newStopFixture(t, nil)

	got, err := f.svc.Stop(t.Context(), StopRequest{OrgID: 1, EventUUID: f.event.EventUUID})
	require.NoError(t, err)
	assert.Equal(t, models.EventStateRestoring, got.State)
	assert.Equal(t, 1, f.store.beginRestoreCalls)
}

func TestService_Stop_IdempotentWhenAlreadyRestoring(t *testing.T) {
	t.Parallel()
	f := newStopFixture(t, func(ev *models.Event) {
		ev.State = models.EventStateRestoring
	})

	got, err := f.svc.Stop(t.Context(), StopRequest{OrgID: 1, EventUUID: f.event.EventUUID})
	require.NoError(t, err)
	assert.Equal(t, models.EventStateRestoring, got.State)
	assert.Equal(t, 0, f.store.beginRestoreCalls,
		"idempotent re-Stop must not call BeginRestoreTransition")
}

func TestService_Stop_RejectsTerminalEvents(t *testing.T) {
	t.Parallel()
	for _, terminal := range []models.EventState{
		models.EventStateCompleted,
		models.EventStateCompletedWithFailures,
		models.EventStateCancelled,
		models.EventStateFailed,
	} {
		t.Run(string(terminal), func(t *testing.T) {
			t.Parallel()
			f := newStopFixture(t, func(ev *models.Event) {
				ev.State = terminal
			})
			_, err := f.svc.Stop(t.Context(), StopRequest{OrgID: 1, EventUUID: f.event.EventUUID})
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, "failed_precondition", fleetErr.GRPCCode.String())
			assert.Equal(t, 0, f.store.beginRestoreCalls)
		})
	}
}

func TestService_Stop_MinDurationGateBlocksNormalPriority(t *testing.T) {
	t.Parallel()
	startedAt := time.Now().Add(-30 * time.Second)
	f := newStopFixture(t, func(ev *models.Event) {
		ev.MinCurtailedDurationSec = 600 // 10 min; 30s elapsed → blocked
		ev.StartedAt = &startedAt
	})

	_, err := f.svc.Stop(t.Context(), StopRequest{OrgID: 1, EventUUID: f.event.EventUUID})
	require.Error(t, err)
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr)
	assert.Equal(t, "failed_precondition", fleetErr.GRPCCode.String())
	assert.Contains(t, fleetErr.DebugMessage, "min_curtailed_duration_sec not elapsed")
	assert.Equal(t, 0, f.store.beginRestoreCalls)
}

func TestService_Stop_EmergencyBypassesMinDuration(t *testing.T) {
	t.Parallel()
	startedAt := time.Now().Add(-30 * time.Second)
	f := newStopFixture(t, func(ev *models.Event) {
		ev.MinCurtailedDurationSec = 600
		ev.Priority = models.PriorityEmergency
		ev.StartedAt = &startedAt
	})

	_, err := f.svc.Stop(t.Context(), StopRequest{OrgID: 1, EventUUID: f.event.EventUUID})
	require.NoError(t, err)
	assert.Equal(t, 1, f.store.beginRestoreCalls)
}

func TestService_Stop_MinDurationDoesNotGatePendingEvents(t *testing.T) {
	t.Parallel()
	// Pending events have nothing to curtail yet; min_curtailed_duration_sec
	// only counts wall-clock since started_at and the event hasn't transitioned
	// to active. Stop should pass through to BeginRestoreTransition regardless.
	f := newStopFixture(t, func(ev *models.Event) {
		ev.State = models.EventStatePending
		ev.MinCurtailedDurationSec = 600
		ev.StartedAt = nil
	})

	_, err := f.svc.Stop(t.Context(), StopRequest{OrgID: 1, EventUUID: f.event.EventUUID})
	require.NoError(t, err)
	assert.Equal(t, 1, f.store.beginRestoreCalls)
}

func TestService_Stop_RejectsInvalidRequestShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		req  StopRequest
	}{
		{"missing_org", StopRequest{EventUUID: uuid.New()}},
		{"missing_event_uuid", StopRequest{OrgID: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newStopFixture(t, nil)
			_, err := f.svc.Stop(t.Context(), tc.req)
			require.Error(t, err)
			var fleetErr fleeterror.FleetError
			require.ErrorAs(t, err, &fleetErr)
			assert.Equal(t, "invalid_argument", fleetErr.GRPCCode.String())
		})
	}
}

func TestService_Stop_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	f := newStopFixture(t, nil)
	f.store.beginRestoreErr = errors.New("db boom")

	_, err := f.svc.Stop(t.Context(), StopRequest{OrgID: 1, EventUUID: f.event.EventUUID})
	require.Error(t, err)
	assert.ErrorContains(t, err, "db boom")
}

func TestComputeEffectiveBatchSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		restoreBatchSize int32
		nonTerminalCount int32
		want             int32
	}{
		{"small_fleet_floors_to_10", 0, 50, 10},
		{"five_thousand_picks_50", 10, 5000, 50},
		{"ten_thousand_ceilings_at_100", 10, 10_000, 100},
		{"twenty_thousand_still_at_100", 10, 20_000, 100},
		{"restore_batch_size_floors_formula", 60, 1000, 60},
		{"negative_restore_batch_size_floors_to_10", -5, 50, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ComputeEffectiveBatchSize(tc.restoreBatchSize, tc.nonTerminalCount)
			assert.Equal(t, tc.want, got)
		})
	}
}
