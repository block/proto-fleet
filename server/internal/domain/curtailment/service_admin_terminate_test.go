package curtailment

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

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
