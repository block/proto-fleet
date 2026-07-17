package curtailment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/infrastructure/driver"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

type fakeTerminalFanController struct {
	powers    []driver.PowerMode
	lastEvent *models.Event
	err       *string
}

func (f *fakeTerminalFanController) SetState(_ context.Context, event *models.Event, power driver.PowerMode) *string {
	f.powers = append(f.powers, power)
	f.lastEvent = event
	return f.err
}

// TestService_AdminTerminate_HappyPathForwardsToStore: the service hands
// off to the store with the operator-chosen terminal state and reason,
// and returns the store's result verbatim.
func TestService_AdminTerminate_HappyPathForwardsToStore(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.adminTerminateResult = &models.Event{
		ID:        99,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
	}
	svc := NewService(store)

	got, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       orgID,
		EventUUID:   eventUUID,
		TargetState: models.EventStateCancelled,
		Reason:      "operator escalation",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, models.EventStateCancelled, got.State)
	assert.Equal(t, 1, store.adminTerminateCalls)
	assert.Equal(t, eventUUID, store.lastAdminTerminateUUID)
	assert.Equal(t, models.EventStateCancelled, store.lastAdminTerminateState)
	assert.Equal(t, "operator escalation", store.lastAdminTerminateReason)
}

func TestService_AdminTerminate_RestoringEventTurnsFansOnBeforeTerminal(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	fanOffAt := time.Now().UTC()
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID:                   88,
		EventUUID:            eventUUID,
		OrgID:                orgID,
		State:                models.EventStateRestoring,
		FacilityFanDeviceIDs: []int64{501, 502},
		FanOffSentAt:         &fanOffAt,
	}
	store.adminTerminateResult = &models.Event{
		ID:        88,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
	}
	fans := &fakeTerminalFanController{}
	svc := NewService(store, WithFacilityFanController(fans))

	got, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       orgID,
		EventUUID:   eventUUID,
		TargetState: models.EventStateCancelled,
		Reason:      "operator escalation",
	})

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	require.NotNil(t, fans.lastEvent)
	assert.Equal(t, []int64{501, 502}, fans.lastEvent.FacilityFanDeviceIDs)
	assert.Equal(t, 1, store.updateFanStateCalls)
	assert.Equal(t, int64(88), store.lastUpdateFanStateID)
	assert.Equal(t, models.EventStateRestoring, store.lastUpdateFanStateParams.ExpectedEventState)
	assert.NotNil(t, store.lastUpdateFanStateParams.FanOnSentAt)
	assert.Nil(t, store.lastUpdateFanStateParams.LastError)
	assert.Equal(t, 1, store.adminTerminateCalls)
}

func TestService_AdminTerminate_RecurtailedPendingEventRetriesFansOnBeforeTerminal(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	fanOffAt := time.Now().UTC().Add(-2 * time.Minute)
	priorError := "restore fan ON failed"
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID:                   87,
		EventUUID:            eventUUID,
		OrgID:                orgID,
		State:                models.EventStatePending,
		FacilityFanDeviceIDs: []int64{501},
		FanOffSentAt:         &fanOffAt,
		FanLastError:         &priorError,
	}
	store.adminTerminateResult = &models.Event{
		ID:        87,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
	}
	fans := &fakeTerminalFanController{}

	_, err := NewService(store, WithFacilityFanController(fans)).AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       orgID,
		EventUUID:   eventUUID,
		TargetState: models.EventStateCancelled,
		Reason:      "operator escalation",
	})

	require.NoError(t, err)
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Equal(t, 1, store.updateFanStateCalls)
	assert.Equal(t, models.EventStatePending, store.lastUpdateFanStateParams.ExpectedEventState)
	assert.NotNil(t, store.lastUpdateFanStateParams.FanOnSentAt)
	assert.Nil(t, store.lastUpdateFanStateParams.LastError)
	assert.Equal(t, 1, store.adminTerminateCalls)
}

func TestService_ForceRelease_HappyPathForwardsToStore(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.forceReleaseResult = &models.Event{
		ID:        99,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
	}
	store.forceReleaseSweptTargets = 52
	store.forceReleaseAutomationDisabled = true
	svc := NewService(store)

	got, err := svc.ForceRelease(t.Context(), ForceReleaseRequest{
		OrgID:     orgID,
		EventUUID: eventUUID,
		Reason:    "operator release",
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Event)
	assert.Equal(t, models.EventStateCancelled, got.Event.State)
	assert.Equal(t, int64(52), got.ReleasedTargetCount)
	assert.True(t, got.OwnershipReleased)
	assert.True(t, got.AutomationDisabled)
	assert.Equal(t, 1, store.forceReleaseCalls)
	assert.Equal(t, eventUUID, store.lastForceReleaseUUID)
	assert.Equal(t, "operator release", store.lastForceReleaseReason)
}

func TestService_ForceRelease_TerminalizesBeforeTurningNonTerminalEventFansOn(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	fanOffAt := time.Now().UTC()
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID:                   89,
		EventUUID:            eventUUID,
		OrgID:                orgID,
		State:                models.EventStateActive,
		FacilityFanDeviceIDs: []int64{601},
		FanOffSentAt:         &fanOffAt,
	}
	store.forceReleaseResult = &models.Event{
		ID:        89,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
	}
	fans := &fakeTerminalFanController{}
	svc := NewService(store, WithFacilityFanController(fans))

	got, err := svc.ForceRelease(t.Context(), ForceReleaseRequest{
		OrgID:     orgID,
		EventUUID: eventUUID,
		Reason:    "operator release",
	})

	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Event)
	assert.NotNil(t, got.Event.FanOnSentAt)
	assert.Nil(t, got.Event.FanLastError)
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Equal(t, 1, store.updateFanStateCalls)
	assert.Equal(t, int64(89), store.lastUpdateFanStateID)
	assert.Equal(t, models.EventStateCancelled, store.lastUpdateFanStateParams.ExpectedEventState)
	assert.NotNil(t, store.lastUpdateFanStateParams.FanOnSentAt)
	assert.Nil(t, store.lastUpdateFanStateParams.LastError)
	assert.Equal(t, 1, store.forceReleaseCalls)
	assert.Equal(t, []string{"force release", "terminal fan recovery"}, store.operatorFanCallOrder)
}

func TestService_ForceRelease_RestoresFansWithoutPersistedOffTimestamp(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID:                   95,
		EventUUID:            eventUUID,
		OrgID:                orgID,
		State:                models.EventStateActive,
		FacilityFanDeviceIDs: []int64{601},
	}
	store.forceReleaseResult = &models.Event{
		ID:        95,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
	}
	fans := &fakeTerminalFanController{}

	_, err := NewService(store, WithFacilityFanController(fans)).ForceRelease(t.Context(), ForceReleaseRequest{
		OrgID:     orgID,
		EventUUID: eventUUID,
		Reason:    "operator release",
	})

	require.NoError(t, err)
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Equal(t, 1, store.updateFanStateCalls)
	assert.Equal(t, models.EventStateCancelled, store.lastUpdateFanStateParams.ExpectedEventState)
	assert.NotNil(t, store.lastUpdateFanStateParams.FanOnSentAt)
	assert.Equal(t, []string{"force release", "terminal fan recovery"}, store.operatorFanCallOrder)
}

func TestService_ForceRelease_RetriesFansOnAfterEarlierFailedAttempt(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	fanOffAt := time.Now().UTC().Add(-2 * time.Minute)
	firstFanOnAt := time.Now().UTC().Add(-time.Minute)
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID:                   90,
		EventUUID:            eventUUID,
		OrgID:                orgID,
		State:                models.EventStateRestoring,
		FacilityFanDeviceIDs: []int64{701},
		FanOffSentAt:         &fanOffAt,
		FanOnSentAt:          &firstFanOnAt,
	}
	store.forceReleaseResult = &models.Event{
		ID:        90,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateCancelled,
	}
	fanErr := "first ON command failed"
	fans := &fakeTerminalFanController{err: &fanErr}
	svc := NewService(store, WithFacilityFanController(fans))

	got, err := svc.ForceRelease(t.Context(), ForceReleaseRequest{
		OrgID:     orgID,
		EventUUID: eventUUID,
		Reason:    "operator release",
	})

	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Event)
	assert.Equal(t, &firstFanOnAt, got.Event.FanOnSentAt)
	require.NotNil(t, got.Event.FanLastError)
	assert.Equal(t, fanErr, *got.Event.FanLastError)
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Equal(t, 1, store.updateFanStateCalls)
	assert.Nil(t, store.lastUpdateFanStateParams.FanOnSentAt,
		"the original first-attempt timestamp must remain unchanged")
	assert.Equal(t, models.EventStateCancelled, store.lastUpdateFanStateParams.ExpectedEventState)
	require.NotNil(t, store.lastUpdateFanStateParams.LastError)
	assert.Equal(t, fanErr, *store.lastUpdateFanStateParams.LastError)
}

func TestService_ForceRelease_RetriesAndClearsTerminalFanFailure(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	fanOffAt := time.Now().UTC().Add(-2 * time.Minute)
	firstFanOnAt := time.Now().UTC().Add(-time.Minute)
	lastError := "device 701: command failed"
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID:                   91,
		EventUUID:            eventUUID,
		OrgID:                orgID,
		State:                models.EventStateCompletedWithFailures,
		FacilityFanDeviceIDs: []int64{701},
		FanOffSentAt:         &fanOffAt,
		FanOnSentAt:          &firstFanOnAt,
		FanLastError:         &lastError,
	}
	store.forceReleaseResult = store.eventsByUUID[eventUUID]
	fans := &fakeTerminalFanController{}
	svc := NewService(store, WithFacilityFanController(fans))

	_, err := svc.ForceRelease(t.Context(), ForceReleaseRequest{
		OrgID:     orgID,
		EventUUID: eventUUID,
		Reason:    "fan control path repaired",
	})

	require.NoError(t, err)
	assert.Equal(t, []driver.PowerMode{driver.PowerOn}, fans.powers)
	assert.Equal(t, 1, store.updateFanStateCalls)
	assert.Equal(t, models.EventStateCompletedWithFailures, store.lastUpdateFanStateParams.ExpectedEventState)
	assert.Nil(t, store.lastUpdateFanStateParams.FanOnSentAt)
	assert.Nil(t, store.lastUpdateFanStateParams.LastError)
	assert.Equal(t, 1, store.forceReleaseCalls)
}

func TestService_ForceRelease_DoesNotReassertRecoveredTerminalFanState(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	fanOffAt := time.Now().UTC().Add(-2 * time.Minute)
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID: 93, EventUUID: eventUUID, OrgID: 1, State: models.EventStateCompleted,
		FacilityFanDeviceIDs: []int64{701}, FanOffSentAt: &fanOffAt,
	}
	store.forceReleaseResult = store.eventsByUUID[eventUUID]
	fans := &fakeTerminalFanController{}

	_, err := NewService(store, WithFacilityFanController(fans)).ForceRelease(t.Context(), ForceReleaseRequest{
		OrgID: 1, EventUUID: eventUUID, Reason: "idempotent operator check",
	})

	require.NoError(t, err)
	assert.Empty(t, fans.powers)
	assert.Zero(t, store.updateFanStateCalls)
	assert.Equal(t, 1, store.forceReleaseCalls)
}

func TestService_ForceRelease_DoesNotOverrideNewerTerminalFanClaim(t *testing.T) {
	t.Parallel()
	eventUUID := uuid.New()
	fanOffAt := time.Now().UTC().Add(-2 * time.Minute)
	lastError := "device 701: command failed"
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID: 94, EventUUID: eventUUID, OrgID: 1, State: models.EventStateCompletedWithFailures,
		FacilityFanDeviceIDs: []int64{701}, FanOffSentAt: &fanOffAt, FanLastError: &lastError,
	}
	store.terminalFanRecoveryErr = fleeterror.NewFailedPreconditionError("facility fan has a newer owner")
	fans := &fakeTerminalFanController{}

	_, err := NewService(store, WithFacilityFanController(fans)).ForceRelease(t.Context(), ForceReleaseRequest{
		OrgID: 1, EventUUID: eventUUID, Reason: "retry stale event",
	})

	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Empty(t, fans.powers)
	assert.Zero(t, store.forceReleaseCalls)
}

func TestService_OperatorTerminalPathsStopWhenFanRecoveryStateCannotBeLoaded(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		run  func(*Service) error
	}{
		{
			name: "admin terminate",
			run: func(svc *Service) error {
				_, err := svc.AdminTerminate(context.Background(), AdminTerminateRequest{
					OrgID: 1, EventUUID: uuid.New(), TargetState: models.EventStateFailed, Reason: "operator recovery",
				})
				return err
			},
		},
		{
			name: "force release",
			run: func(svc *Service) error {
				_, err := svc.ForceRelease(context.Background(), ForceReleaseRequest{
					OrgID: 1, EventUUID: uuid.New(), Reason: "operator recovery",
				})
				return err
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore()
			store.getEventByUUIDErr = errors.New("temporary event lookup failure")
			err := testCase.run(NewService(store, WithFacilityFanController(&fakeTerminalFanController{})))
			require.ErrorContains(t, err, "temporary event lookup failure")
			assert.Zero(t, store.adminTerminateCalls)
			assert.Zero(t, store.forceReleaseCalls)
		})
	}
}

func TestService_OperatorTerminalPathsStopWhenFanRecoveryStateCannotBePersisted(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name             string
		state            models.EventState
		wantAdminCalls   int
		wantReleaseCalls int
		run              func(*Service, uuid.UUID) error
	}{
		{
			name:           "admin terminate",
			state:          models.EventStateRestoring,
			wantAdminCalls: 0,
			run: func(svc *Service, eventUUID uuid.UUID) error {
				_, err := svc.AdminTerminate(context.Background(), AdminTerminateRequest{
					OrgID: 1, EventUUID: eventUUID, TargetState: models.EventStateFailed, Reason: "operator recovery",
				})
				return err
			},
		},
		{
			name:             "force release",
			state:            models.EventStateActive,
			wantReleaseCalls: 1,
			run: func(svc *Service, eventUUID uuid.UUID) error {
				_, err := svc.ForceRelease(context.Background(), ForceReleaseRequest{
					OrgID: 1, EventUUID: eventUUID, Reason: "operator recovery",
				})
				return err
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			eventUUID := uuid.New()
			fanOffAt := time.Now().UTC()
			store := newFakeStore()
			store.eventsByUUID[eventUUID] = &models.Event{
				ID: 92, EventUUID: eventUUID, OrgID: 1, State: testCase.state,
				FacilityFanDeviceIDs: []int64{701}, FanOffSentAt: &fanOffAt,
			}
			store.forceReleaseResult = &models.Event{
				ID: 92, EventUUID: eventUUID, OrgID: 1, State: models.EventStateCancelled,
			}
			store.updateFanStateErr = errors.New("temporary fan state write failure")
			err := testCase.run(NewService(store, WithFacilityFanController(&fakeTerminalFanController{})), eventUUID)
			require.ErrorContains(t, err, "temporary fan state write failure")
			assert.Equal(t, testCase.wantAdminCalls, store.adminTerminateCalls)
			assert.Equal(t, testCase.wantReleaseCalls, store.forceReleaseCalls)
		})
	}
}

func TestService_ForceRelease_RejectsMissingReason(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())

	_, err := svc.ForceRelease(t.Context(), ForceReleaseRequest{
		OrgID:     1,
		EventUUID: uuid.New(),
		Reason:    "   ",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "reason")
}

// TestService_AdminTerminate_RejectsNonAllowedTargetStates: only
// CANCELLED and FAILED are valid; COMPLETED, RESTORING, etc. are
// rejected. The proto validator already restricts; the service repeats
// the check as defense in depth.
func TestService_AdminTerminate_RejectsNonAllowedTargetStates(t *testing.T) {
	t.Parallel()
	for _, state := range []models.EventState{
		models.EventStatePending,
		models.EventStateActive,
		models.EventStateRestoring,
		models.EventStateCompleted,
		models.EventStateCompletedWithFailures,
		"",
	} {
		svc := NewService(newFakeStore())
		_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
			OrgID:       1,
			EventUUID:   uuid.New(),
			TargetState: state,
			Reason:      "test",
		})
		require.Error(t, err, "state %s must be rejected", state)
		assert.True(t, fleeterror.IsInvalidArgumentError(err))
	}
}

// TestService_AdminTerminate_RejectsMissingReason: per-target last_error
// is operator-attributable; an empty reason corrupts the audit trail.
func TestService_AdminTerminate_RejectsMissingReason(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())

	_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       1,
		EventUUID:   uuid.New(),
		TargetState: models.EventStateCancelled,
		Reason:      "   ",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "reason")
}

// TestService_AdminTerminate_RejectsOversizedReason: reason is fanned out
// into every swept target's last_error column, so an unbounded value
// amplifies into thousands of rows. Service backstop mirrors the proto
// validator's max_len=256.
func TestService_AdminTerminate_RejectsOversizedReason(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())

	huge := make([]byte, startTextFieldMaxLen+1)
	for i := range huge {
		huge[i] = 'x'
	}
	_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       1,
		EventUUID:   uuid.New(),
		TargetState: models.EventStateCancelled,
		Reason:      string(huge),
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "reason must be at most")
}

// TestService_AdminTerminate_StateConflictMapsFailedPrecondition: a
// terminal event in a different state surfaces a clean FailedPrecondition
// carrying the typed service code so machine callers can branch without
// string-matching the debug message.
func TestService_AdminTerminate_StateConflictMapsFailedPrecondition(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.adminTerminateErr = interfaces.ErrCurtailmentAdminTerminateStateConflict
	svc := NewService(store)

	_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       1,
		EventUUID:   uuid.New(),
		TargetState: models.EventStateFailed,
		Reason:      "test",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "different state")
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr,
		"FailedPrecondition must carry a FleetError envelope so the service-specific code reaches the wire")
	assert.Equal(t, fleeterror.ErrorCodeTypeService, fleetErr.FleetErrorCodeType,
		"state-conflict precondition must use the Service code variant, not Common/Unspecified")
	assert.Equal(t, FleetErrorCodeAdminTerminateStateConflict, fleetErr.FleetErrorCode,
		"state-conflict precondition must carry FleetErrorCodeAdminTerminateStateConflict so machine callers branch on it")
}

func TestService_AdminTerminate_ActiveEventRequiresStopFirst(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.adminTerminateErr = interfaces.ErrCurtailmentAdminTerminateActiveEvent
	svc := NewService(store)

	_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       1,
		EventUUID:   uuid.New(),
		TargetState: models.EventStateFailed,
		Reason:      "test",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "in-flight curtail commands")
	var fleetErr fleeterror.FleetError
	require.ErrorAs(t, err, &fleetErr,
		"FailedPrecondition must carry a FleetError envelope so the service-specific code reaches the wire")
	assert.Equal(t, fleeterror.ErrorCodeTypeService, fleetErr.FleetErrorCodeType,
		"in-flight precondition must use the Service code variant, not Common/Unspecified")
	assert.Equal(t, FleetErrorCodeAdminTerminateInFlightCommands, fleetErr.FleetErrorCode,
		"in-flight precondition must carry FleetErrorCodeAdminTerminateInFlightCommands so machine callers can route 'call Stop first' recovery without parsing the debug message")
}

// TestService_AdminTerminate_PropagatesStoreError: unrelated store errors
// surface unchanged so wrapped fleeterror types stay intact.
func TestService_AdminTerminate_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.adminTerminateErr = errors.New("db down")
	svc := NewService(store)

	_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       1,
		EventUUID:   uuid.New(),
		TargetState: models.EventStateCancelled,
		Reason:      "test",
	})
	require.Error(t, err)
	assert.ErrorContains(t, err, "db down")
}

// TestService_AdminTerminate_RejectsMissingOrg / MissingUUID pin the
// front-line guards.
func TestService_AdminTerminate_RejectsMissingOrgAndUUID(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())

	_, err := svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       0,
		EventUUID:   uuid.New(),
		TargetState: models.EventStateCancelled,
		Reason:      "test",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))

	_, err = svc.AdminTerminate(t.Context(), AdminTerminateRequest{
		OrgID:       1,
		EventUUID:   uuid.Nil,
		TargetState: models.EventStateCancelled,
		Reason:      "test",
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}
