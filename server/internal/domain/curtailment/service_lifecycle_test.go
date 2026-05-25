package curtailment

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
)

// End-to-end operator-surface walk against the in-memory fake.
// Reconciler ticks are covered piecewise in reconciler_test.go /
// restore_test.go.
//
//	Preview → Start (+audit+metrics) → Stop → AdminTerminate
//
// A consumer wiring the SDK against the real grpc surface should see the
// same sequence of state transitions and audit emissions verified here.
func TestService_Lifecycle_PreviewStartStopAdminTerminate(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)

	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("alpha", 4000, 120, 50),
		minerWithEff("beta", 3500, 100, 35),
		minerWithEff("gamma", 3000, 80, 20),
	}
	audit := &recordingAuditLogger{}
	metrics := newRecordingMetrics()
	svc := NewService(store, WithAuditLogger(audit), WithServiceMetrics(metrics))

	// ---- Preview: plan calculation, zero persistence side-effects ----
	previewReq := validRequest(orgID)
	previewReq.TargetKW = 7 // alpha (4 kW) + beta (3.5 kW) = 7.5 kW; selects 2
	plan, err := svc.Preview(t.Context(), previewReq)
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.Equal(t, 0, store.insertEventCalls, "Preview must not persist")
	assert.Empty(t, audit.snapshot(), "Preview must not emit audit rows")
	require.Len(t, plan.Selected, 2)
	assert.Equal(t, "alpha", plan.Selected[0].DeviceIdentifier)
	assert.Equal(t, "beta", plan.Selected[1].DeviceIdentifier)

	// ---- Start: persistence + audit + selector-exclusion metrics ----
	startReq := validStartRequest(orgID)
	startReq.TargetKW = 7
	startReq.Reason = "lifecycle test — energy event"

	plan, err = svc.Start(t.Context(), startReq)
	require.NoError(t, err)
	require.NotNil(t, plan.EventUUID)
	assert.Equal(t, 1, store.insertEventCalls)
	eventUUID := *plan.EventUUID

	// Audit: one curtailment_started row, no override-specific rows.
	events := audit.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, ActivityTypeStarted, events[0].Type)
	assert.Equal(t, activitymodels.CategoryCurtailment, events[0].Category)
	assert.Equal(t, eventUUID.String(), events[0].Metadata["event_uuid"])

	// The store recorded a PENDING event with the expected scope + reason.
	persistedEvent := store.lastInsertEvent
	assert.Equal(t, models.EventStatePending, persistedEvent.State)
	assert.Equal(t, "lifecycle test — energy event", persistedEvent.Reason)
	assert.Len(t, store.lastInsertTargets, 2)

	// ---- Stop: PENDING/ACTIVE → RESTORING via BeginRestoreTransition ----
	// Seed the event into the fake's UUID-keyed map so Service.Stop can
	// re-read it (real store persists, fake doesn't auto-cross-populate).
	store.eventsByUUID[eventUUID] = &models.Event{
		ID:        1,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateActive, // imagine the reconciler advanced it
	}
	stoppedEvent, err := svc.Stop(t.Context(), StopRequest{OrgID: orgID, EventUUID: eventUUID})
	require.NoError(t, err)
	assert.Equal(t, models.EventStateRestoring, stoppedEvent.State)
	assert.Equal(t, 1, store.beginRestoreCalls)

	// ---- AdminTerminate: RESTORING → FAILED (operator-forced) ----
	// Mirror the persistence the real store would do for the AdminTerminate
	// result: a returned event in the requested terminal state.
	store.adminTerminateResult = &models.Event{
		ID:        1,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateFailed,
	}
	final, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       orgID,
		EventUUID:   eventUUID,
		TargetState: models.EventStateFailed,
		Reason:      "lifecycle test — operator force terminate",
	})
	require.NoError(t, err)
	assert.Equal(t, models.EventStateFailed, final.State)
	assert.Equal(t, 1, store.adminTerminateCalls)
	assert.Equal(t, "lifecycle test — operator force terminate", store.lastAdminTerminateReason)
}

// TestService_Lifecycle_StartReplayShortCircuitsSecondCall pins the
// webhook-idempotency contract at the lifecycle boundary: a duplicate
// Start with the same idempotency_key returns the same event UUID
// without re-running selection or emitting another audit row.
func TestService_Lifecycle_StartReplayShortCircuitsSecondCall(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)

	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("alpha", 4000, 120, 50),
	}
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	startReq := validStartRequest(orgID)
	startReq.TargetKW = 2
	key := "webhook-retry-key-7"
	startReq.IdempotencyKey = &key

	// First call: real persistence + audit.
	first, err := svc.Start(t.Context(), startReq)
	require.NoError(t, err)
	require.NotNil(t, first.EventUUID)
	originalUUID := *first.EventUUID
	require.Len(t, audit.snapshot(), 1)

	// Seed the replay channel so the second call sees the prior event.
	store.eventsByIdempotencyKey = map[string]*models.Event{
		key: {ID: 1, EventUUID: originalUUID, OrgID: orgID, State: models.EventStatePending},
	}

	// Second call: same key, must short-circuit.
	second, err := svc.Start(t.Context(), startReq)
	require.NoError(t, err)
	require.NotNil(t, second.EventUUID)
	assert.Equal(t, originalUUID, *second.EventUUID)
	assert.Equal(t, 1, store.insertEventCalls,
		"replay must not double-insert")
	assert.Len(t, audit.snapshot(), 1,
		"replay must not double-emit the audit trail")
}

// Idempotent replay path emits a *_replay audit row carrying this
// caller's actor + reason — without it a race-loser's attribution
// would be silently dropped.
func TestService_AdminTerminate_IdempotentReplay_EmitsReplayAuditRow(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)

	store := newFakeStore()
	eventUUID := uuid.New()
	store.adminTerminateResult = &models.Event{
		ID:        1,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
	}
	store.adminTerminateIdempotentReplay = true

	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	final, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       orgID,
		EventUUID:   eventUUID,
		TargetState: models.EventStateCancelled,
		Reason:      "race-loser reason",
	})
	require.NoError(t, err)
	assert.Equal(t, models.EventStateCancelled, final.State)
	assert.Equal(t, 1, store.adminTerminateCalls)

	events := audit.snapshot()
	require.Len(t, events, 1, "idempotent replay must emit exactly one replay-type audit row")
	assert.Equal(t, ActivityTypeAdminTerminatedReplay, events[0].Type,
		"replay path must use the replay-type, not the primary terminated type")
	require.NotNil(t, events[0].Metadata)
	assert.Equal(t, "race-loser reason", events[0].Metadata["reason"],
		"replay row must capture THIS caller's reason, not the winner's, so audit reconstruction surfaces every operator's attempt")
}

// TestService_AdminTerminate_Transition_EmitsAudit is the positive
// counterpart of the idempotent-replay test: when the store reports a
// real transition, audit emission still fires. Together they pin both
// arms of the new transitioned-flag gate.
func TestService_AdminTerminate_Transition_EmitsAudit(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)

	store := newFakeStore()
	eventUUID := uuid.New()
	store.adminTerminateResult = &models.Event{
		ID:        1,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateFailed,
	}
	// adminTerminateIdempotentReplay defaults to false → transitioned=true.

	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       orgID,
		EventUUID:   eventUUID,
		TargetState: models.EventStateFailed,
		Reason:      "operator force terminate",
	})
	require.NoError(t, err)

	events := audit.snapshot()
	require.Len(t, events, 1, "real transition must emit one audit row")
	assert.Equal(t, ActivityTypeAdminTerminated, events[0].Type)
}

// Two concurrent AdminTerminates: A wins (emits AdminTerminated),
// B lands after and emits AdminTerminatedReplay. Both rows carry the
// caller's distinct reason.
func TestService_AdminTerminate_RaceLoserAttributionPreserved(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	eventUUID := uuid.New()
	terminated := &models.Event{
		ID:        1,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
	}

	store := newFakeStore()
	store.adminTerminateResult = terminated
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	// Call 1: operator A wins the transition. transitioned=true.
	_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       orgID,
		EventUUID:   eventUUID,
		TargetState: models.EventStateCancelled,
		Reason:      "power emergency",
	})
	require.NoError(t, err)

	// Call 2: operator B's call lands after A's transition. The store
	// returns the same row with transitioned=false (idempotent echo).
	store.adminTerminateIdempotentReplay = true
	_, err = svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       orgID,
		EventUUID:   eventUUID,
		TargetState: models.EventStateCancelled,
		Reason:      "operational fatfinger",
	})
	require.NoError(t, err)

	events := audit.snapshot()
	require.Len(t, events, 2, "both calls must emit one row each (primary + replay)")

	assert.Equal(t, ActivityTypeAdminTerminated, events[0].Type,
		"first call wins the transition → primary terminate row")
	assert.Equal(t, "power emergency", events[0].Metadata["reason"])

	assert.Equal(t, ActivityTypeAdminTerminatedReplay, events[1].Type,
		"second call races → replay row preserves the loser's attribution")
	assert.Equal(t, "operational fatfinger", events[1].Metadata["reason"],
		"replay row must capture the loser's distinct reason; without this the audit feed would only show the winner")
}

// TestService_Lifecycle_ListEventsReturnsTerminalRow demonstrates the
// read path: after a Start + AdminTerminate cycle, the operator's
// history-view query returns the terminal event with the expected
// state filter applied.
func TestService_Lifecycle_ListEventsReturnsTerminalRow(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)

	store := newFakeStore()
	// Seed a finished event into the history slice directly (the fake's
	// ListEvents reads from this slice; the lifecycle above already
	// covers the persistence-side wiring).
	terminalUUID := uuid.New()
	store.eventsHistory = []*models.Event{
		{
			ID:        1,
			EventUUID: terminalUUID,
			OrgID:     orgID,
			State:     models.EventStateFailed,
			Mode:      models.ModeFixedKw,
			Strategy:  models.StrategyLeastEfficientFirst,
			Level:     models.LevelFull,
			Priority:  models.PriorityNormal,
			Reason:    "lifecycle test — operator force terminate",
		},
	}
	svc := NewService(store)

	got, _, err := svc.ListEvents(t.Context(), ListEventsRequest{
		OrgID:       orgID,
		PageSize:    20,
		StateFilter: models.EventStateFailed,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, terminalUUID, got[0].EventUUID)
	assert.Equal(t, models.EventStateFailed, got[0].State)
}
