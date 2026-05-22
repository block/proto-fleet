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

// TestService_Update_HappyPath: pending/active states accept the patch,
// the store sees the params verbatim, and the post-update event echoes
// back to the caller.
func TestService_Update_HappyPath(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	persisted := &models.Event{
		ID:        99,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateActive,
	}
	updated := *persisted
	updated.Reason = "operator changed mind"

	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.eventsByUUID[eventUUID] = persisted
	store.updateOperatorFieldsResult = &updated
	svc := NewService(store)

	newReason := "operator changed mind"
	newCap := int32(1800)
	got, err := svc.Update(t.Context(), UpdateRequest{
		OrgID:              orgID,
		EventUUID:          eventUUID,
		Reason:             &newReason,
		MaxDurationSeconds: &newCap,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "operator changed mind", got.Reason)
	assert.Equal(t, int64(99), store.lastUpdateOperatorFieldsID, "store sees the persisted id, not the uuid")
	assert.Equal(t, &newReason, store.lastUpdateOperatorFieldsArgs.Reason)
	assert.Equal(t, &newCap, store.lastUpdateOperatorFieldsArgs.MaxDurationSeconds)
}

// TestService_Update_RejectsRestoringState: the conservative state policy
// — Update is operator-safe field changes, not in-flight restore tuning;
// AdminTerminate is the recovery path for restoring events.
func TestService_Update_RejectsRestoringState(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID:        1,
		EventUUID: eventUUID,
		OrgID:     orgID,
		State:     models.EventStateRestoring,
	}
	svc := NewService(store)

	newReason := "updated"
	_, err := svc.Update(t.Context(), UpdateRequest{OrgID: orgID, EventUUID: eventUUID, Reason: &newReason})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "restoring")
	assert.Equal(t, 0, store.updateOperatorFieldsCalls, "store must not be touched after the pre-read rejects")
}

// TestService_Update_RejectsTerminalState pins the same guard for the
// terminal states (Completed, Cancelled, Failed, etc.) since a terminal
// event has no operator-actionable surface left.
func TestService_Update_RejectsTerminalState(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	for _, state := range []models.EventState{
		models.EventStateCompleted,
		models.EventStateCompletedWithFailures,
		models.EventStateCancelled,
		models.EventStateFailed,
	} {
		eventUUID := uuid.New()
		store := newFakeStore()
		store.eventsByUUID[eventUUID] = &models.Event{
			ID:        1,
			EventUUID: eventUUID,
			OrgID:     orgID,
			State:     state,
		}
		svc := NewService(store)

		newReason := "updated"
		_, err := svc.Update(t.Context(), UpdateRequest{OrgID: orgID, EventUUID: eventUUID, Reason: &newReason})
		require.Error(t, err, "state %s must reject Update", state)
		assert.True(t, fleeterror.IsFailedPreconditionError(err), "state %s must surface FailedPrecondition", state)
	}
}

// TestService_Update_NotFoundOnUnknownUUID: cross-tenant exposure or a
// stale-cursor scenario both surface as NotFound — never an empty
// success response.
func TestService_Update_NotFoundOnUnknownUUID(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := NewService(store)

	newReason := "updated"
	_, err := svc.Update(t.Context(), UpdateRequest{OrgID: 1, EventUUID: uuid.New(), Reason: &newReason})
	require.Error(t, err)
	assert.True(t, fleeterror.IsNotFoundError(err))
}

// TestService_Update_RejectsMissingOrg pins the org guard.
func TestService_Update_RejectsMissingOrg(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore())

	_, err := svc.Update(t.Context(), UpdateRequest{OrgID: 0, EventUUID: uuid.New()})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

// TestService_Update_RejectsAbsoluteCapViolations: the same bounds Start
// enforces apply here, so a misconfigured Update can't tunnel past the
// proto validator and hit a DB CHECK.
func TestService_Update_RejectsAbsoluteCapViolations(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID: 1, EventUUID: eventUUID, OrgID: orgID, State: models.EventStateActive,
	}
	svc := NewService(store)

	cases := []struct {
		name string
		req  UpdateRequest
		msg  string
	}{
		{
			name: "max_duration above absolute ceiling",
			req: UpdateRequest{
				OrgID:              orgID,
				EventUUID:          eventUUID,
				MaxDurationSeconds: int32Ptr(maxFiniteDurationSeconds + 1),
			},
			msg: "max_duration_seconds must be <=",
		},
		{
			name: "restore_batch_interval above absolute ceiling",
			req: UpdateRequest{
				OrgID:                   orgID,
				EventUUID:               eventUUID,
				RestoreBatchIntervalSec: int32Ptr(restoreBatchIntervalUpperBoundSec + 1),
				CanUseAdminControls:     true,
			},
			msg: "restore_batch_interval_sec must be <=",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.Update(t.Context(), tc.req)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Contains(t, err.Error(), tc.msg)
		})
	}
}

// TestService_Update_RejectsNonAdminLargeInterval: non-admin callers
// cannot set restore_batch_interval_sec above the non-admin cap, even
// if they stay below the absolute ceiling. Mirrors Start's gate.
func TestService_Update_RejectsNonAdminLargeInterval(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID: 1, EventUUID: eventUUID, OrgID: orgID, State: models.EventStateActive,
	}
	svc := NewService(store)

	_, err := svc.Update(t.Context(), UpdateRequest{
		OrgID:                   orgID,
		EventUUID:               eventUUID,
		RestoreBatchIntervalSec: int32Ptr(nonAdminRestoreBatchIntervalMax + 1),
		CanUseAdminControls:     false,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
	assert.Contains(t, err.Error(), "restore_batch_interval_sec")
}

// TestService_Update_RejectsNonAdminMaxDurationAboveOrgDefault mirrors
// Start's admin gate on max_duration_seconds. Without this check a
// non-admin could Start at the org default then Update the same event
// far above it, bypassing the privilege boundary Start enforces.
func TestService_Update_RejectsNonAdminMaxDurationAboveOrgDefault(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID) // MaxDurationDefaultSec = 14400
	store.eventsByUUID[eventUUID] = &models.Event{
		ID: 1, EventUUID: eventUUID, OrgID: orgID, State: models.EventStateActive,
	}
	svc := NewService(store)

	// Non-admin requesting 1 day (86400s) > org default 14400s → Forbidden.
	_, err := svc.Update(t.Context(), UpdateRequest{
		OrgID:               orgID,
		EventUUID:           eventUUID,
		MaxDurationSeconds:  int32Ptr(86400),
		CanUseAdminControls: false,
	})
	require.Error(t, err)
	assert.True(t, fleeterror.IsForbiddenError(err))
	assert.Contains(t, err.Error(), "max_duration_seconds")
}

// TestService_Update_AllowsAdminMaxDurationAboveOrgDefault: admins can
// bypass the org-default gate as long as the value stays under the
// absolute ceiling.
func TestService_Update_AllowsAdminMaxDurationAboveOrgDefault(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.eventsByUUID[eventUUID] = &models.Event{
		ID: 1, EventUUID: eventUUID, OrgID: orgID, State: models.EventStateActive,
	}
	store.updateOperatorFieldsResult = &models.Event{
		ID: 1, EventUUID: eventUUID, OrgID: orgID, State: models.EventStateActive,
	}
	svc := NewService(store)

	_, err := svc.Update(t.Context(), UpdateRequest{
		OrgID:               orgID,
		EventUUID:           eventUUID,
		MaxDurationSeconds:  int32Ptr(86400),
		CanUseAdminControls: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, store.updateOperatorFieldsCalls)
}

// TestService_Update_RejectsEmptyPatch: an Update that sets no patchable
// field would still bump updated_at via COALESCE on the SQL side,
// producing a misleading freshness signal for clients tracking the
// column. Reject loudly at the service boundary instead.
func TestService_Update_RejectsEmptyPatch(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID: 1, EventUUID: eventUUID, OrgID: orgID, State: models.EventStateActive,
	}
	svc := NewService(store)

	_, err := svc.Update(t.Context(), UpdateRequest{OrgID: orgID, EventUUID: eventUUID})
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "at least one")
	assert.Equal(t, 0, store.updateOperatorFieldsCalls,
		"empty patches must reject before any store call")
}

// TestService_Update_RaceLossSurfacesFailedPrecondition: the SQL-layer
// race-loss sentinel maps to FailedPrecondition so a client retry hits
// the same RPC instead of degrading to Internal.
func TestService_Update_RaceLossSurfacesFailedPrecondition(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID: 1, EventUUID: eventUUID, OrgID: orgID, State: models.EventStateActive,
	}
	store.updateOperatorFieldsErr = interfaces.ErrCurtailmentUpdateStateRaceLoss
	svc := NewService(store)

	newReason := "updated"
	_, err := svc.Update(t.Context(), UpdateRequest{OrgID: orgID, EventUUID: eventUUID, Reason: &newReason})
	require.Error(t, err)
	assert.True(t, fleeterror.IsFailedPreconditionError(err))
	assert.Contains(t, err.Error(), "state advanced")
}

// TestService_Update_PropagatesStoreError: unrelated store errors
// surface unchanged so wrapped fleeterror types stay intact.
func TestService_Update_PropagatesStoreError(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.eventsByUUID[eventUUID] = &models.Event{
		ID: 1, EventUUID: eventUUID, OrgID: orgID, State: models.EventStateActive,
	}
	store.updateOperatorFieldsErr = errors.New("db down")
	svc := NewService(store)

	newReason := "updated"
	_, err := svc.Update(t.Context(), UpdateRequest{OrgID: orgID, EventUUID: eventUUID, Reason: &newReason})
	require.Error(t, err)
	assert.ErrorContains(t, err, "db down")
}

func int32Ptr(v int32) *int32 { return &v }
