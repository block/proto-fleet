package curtailment

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
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
	store.updateOperatorFieldsErr = interfaces.ErrCurtailmentEventStateRaceLoss
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

// TestService_Update_EmitsAuditRowOnRealChange: a successful Update with
// at least one field changing emits exactly one curtailment_updated row;
// metadata's `fields` lists only the fields that actually changed, not
// the no-op slots. Pins both the audit-event type registration and the
// effective-patch metadata shape.
func TestService_Update_EmitsAuditRowOnRealChange(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	currentReason := "initial"
	currentInterval := int32(60)
	currentBatch := int32(20)
	currentMax := int32(3600)
	persisted := &models.Event{
		ID:                      99,
		EventUUID:               eventUUID,
		OrgID:                   orgID,
		State:                   models.EventStateActive,
		Reason:                  currentReason,
		RestoreBatchIntervalSec: currentInterval,
		RestoreBatchSize:        currentBatch,
		MaxDurationSeconds:      &currentMax,
	}
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.eventsByUUID[eventUUID] = persisted
	store.updateOperatorFieldsResult = persisted // store returns the persisted row post-update for assertion simplicity
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	// Patch changes Reason only; echo the persisted values for the other
	// fields so the effective patch contains only the real change.
	newReason := "operator update"
	_, err := svc.Update(t.Context(), UpdateRequest{
		OrgID:                   orgID,
		EventUUID:               eventUUID,
		Reason:                  &newReason,
		RestoreBatchIntervalSec: &currentInterval, // no-op echo
		RestoreBatchSize:        &currentBatch,    // no-op echo
		MaxDurationSeconds:      &currentMax,      // no-op echo
	})
	require.NoError(t, err)

	events := audit.snapshot()
	require.Len(t, events, 1, "exactly one curtailment_updated row on a real change")
	assert.Equal(t, ActivityTypeUpdated, events[0].Type)
	assert.Equal(t, activitymodels.CategoryCurtailment, events[0].Category)
	assert.Equal(t, activitymodels.ResultSuccess, events[0].Result)
	assert.Equal(t, activitymodels.ActorUser, events[0].ActorType)
	require.NotNil(t, events[0].Metadata)
	assert.Equal(t, eventUUID.String(), events[0].Metadata["event_uuid"])
	fields, ok := events[0].Metadata["fields"].([]string)
	require.True(t, ok, "fields metadata key must be a []string")
	assert.Equal(t, []string{"reason"}, fields,
		"only the actually-changed field appears in `fields` metadata; echoed values are excluded")
	assert.Equal(t, "operator update", events[0].Metadata["reason"])
}

// TestService_Update_NoAuditRowOnSameValueEcho: a patch where every field
// matches the persisted value collapses to a no-op — no store call, no
// audit row, no updated_at bump. Pins effectiveUpdatePatch's empty-patch
// early return.
func TestService_Update_NoAuditRowOnSameValueEcho(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	currentReason := "initial"
	currentInterval := int32(60)
	currentMax := int32(3600)
	persisted := &models.Event{
		ID:                      99,
		EventUUID:               eventUUID,
		OrgID:                   orgID,
		State:                   models.EventStateActive,
		Reason:                  currentReason,
		RestoreBatchIntervalSec: currentInterval,
		MaxDurationSeconds:      &currentMax,
	}
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.eventsByUUID[eventUUID] = persisted
	audit := &recordingAuditLogger{}
	svc := NewService(store, WithAuditLogger(audit))

	got, err := svc.Update(t.Context(), UpdateRequest{
		OrgID:                   orgID,
		EventUUID:               eventUUID,
		Reason:                  &currentReason,
		RestoreBatchIntervalSec: &currentInterval,
		MaxDurationSeconds:      &currentMax,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, persisted, got, "no-op echo returns the pre-update event verbatim")
	assert.Equal(t, 0, store.updateOperatorFieldsCalls,
		"no-op patch must not call UpdateOperatorFields (would bump updated_at)")
	assert.Empty(t, audit.snapshot(), "no-op patch must not emit an audit row")
}

// TestService_Update_AllowsNonAdminEchoOfAdminElevatedMaxDuration: a
// non-admin patch that echoes the persisted admin-elevated value as part
// of an unrelated change (e.g. updating reason on a form re-submission)
// must succeed — the no-op collapse drops the elevated value from the
// effective patch before the admin gate runs.
func TestService_Update_AllowsNonAdminEchoOfAdminElevatedMaxDuration(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	// Admin previously elevated max_duration above the org default.
	elevatedMax := int32(14400)
	orgDefaultMax := int32(7200)
	persisted := &models.Event{
		ID:                 99,
		EventUUID:          eventUUID,
		OrgID:              orgID,
		State:              models.EventStateActive,
		Reason:             "initial",
		MaxDurationSeconds: &elevatedMax,
	}
	store := newFakeStore()
	cfg := defaultOrgConfig(orgID)
	cfg.MaxDurationDefaultSec = orgDefaultMax
	store.orgConfigByOrg[orgID] = cfg
	store.eventsByUUID[eventUUID] = persisted
	store.updateOperatorFieldsResult = persisted
	svc := NewService(store)

	newReason := "non-admin updates reason only"
	_, err := svc.Update(t.Context(), UpdateRequest{
		OrgID:               orgID,
		EventUUID:           eventUUID,
		Reason:              &newReason,
		MaxDurationSeconds:  &elevatedMax, // echo of the admin-elevated value
		CanUseAdminControls: false,
	})
	require.NoError(t, err, "non-admin echo of admin-elevated max_duration must not trip the gate")
}

// TestService_Update_AllowsNonAdminEchoOfAdminElevatedRestoreInterval:
// symmetric to the max_duration_seconds test above for restore_batch_interval_sec.
// Without the gate-placement mirror in service.go, this case previously
// rejected with Forbidden.
func TestService_Update_AllowsNonAdminEchoOfAdminElevatedRestoreInterval(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	elevatedInterval := int32(600) // above nonAdminRestoreBatchIntervalMax (300)
	persisted := &models.Event{
		ID:                      99,
		EventUUID:               eventUUID,
		OrgID:                   orgID,
		State:                   models.EventStateActive,
		Reason:                  "initial",
		RestoreBatchIntervalSec: elevatedInterval,
	}
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.eventsByUUID[eventUUID] = persisted
	store.updateOperatorFieldsResult = persisted
	svc := NewService(store)

	newReason := "non-admin updates reason only"
	_, err := svc.Update(t.Context(), UpdateRequest{
		OrgID:                   orgID,
		EventUUID:               eventUUID,
		Reason:                  &newReason,
		RestoreBatchIntervalSec: &elevatedInterval,
		CanUseAdminControls:     false,
	})
	require.NoError(t, err, "non-admin echo of admin-elevated restore_batch_interval_sec must not trip the gate")
}

// TestService_Update_RejectsMultiByteReasonAbove256Runes: the rune-count
// fix means a 256-rune multi-byte string passes (was previously rejected
// as exceeding the byte-count cap). A 257-rune string still rejects.
// Pins the boundary so a regression back to len() would trip.
func TestService_Update_RejectsMultiByteReasonAbove256Runes(t *testing.T) {
	t.Parallel()
	const orgID = int64(1)
	eventUUID := uuid.New()
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.eventsByUUID[eventUUID] = &models.Event{
		ID: 1, EventUUID: eventUUID, OrgID: orgID, State: models.EventStateActive,
		Reason: "initial",
	}
	store.updateOperatorFieldsResult = store.eventsByUUID[eventUUID]
	svc := NewService(store)

	// 256 Korean Hangul syllables: each is 3 bytes in UTF-8 (768 bytes total)
	// but 256 runes. Must pass.
	atCap := strings.Repeat("한", 256)
	_, err := svc.Update(t.Context(), UpdateRequest{
		OrgID: orgID, EventUUID: eventUUID, Reason: &atCap,
	})
	require.NoError(t, err, "256 multi-byte runes must pass rune-count validation")

	// 257 runes — one over the cap.
	overCap := strings.Repeat("한", 257)
	_, err = svc.Update(t.Context(), UpdateRequest{
		OrgID: orgID, EventUUID: eventUUID, Reason: &overCap,
	})
	require.Error(t, err, "257 runes must reject")
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
	assert.Contains(t, err.Error(), "characters", "error message uses character vocabulary, not byte vocabulary")
}
