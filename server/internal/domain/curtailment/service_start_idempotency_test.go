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

// TestService_Start_IdempotencyKeyReplayReturnsExistingEvent: a re-issued
// Start with the same idempotency_key returns the original event without
// running the selector or inserting a new row. Mirrors the webhook-retry
// contract — duplicate deliveries reuse the prior decision.
func TestService_Start_IdempotencyKeyReplayReturnsExistingEvent(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	existingUUID := uuid.New()
	existingMaxDur := int32(3600)
	store := newFakeStore()
	store.eventsByIdempotencyKey = map[string]*models.Event{
		"upstream-retry-key-1": {
			ID:                      99,
			EventUUID:               existingUUID,
			OrgID:                   orgID,
			State:                   models.EventStateActive,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 120,
			MaxDurationSeconds:      &existingMaxDur,
		},
	}
	svc := NewService(store)

	req := validStartRequest(orgID)
	key := "upstream-retry-key-1"
	req.IdempotencyKey = &key

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.NotNil(t, plan.EventUUID)
	assert.Equal(t, existingUUID, *plan.EventUUID)
	require.NotNil(t, plan.ReplayEvent)
	assert.Equal(t, models.EventStateActive, plan.ReplayEvent.State)
	assert.Equal(t, int32(120), plan.EffectiveRestoreBatchIntervalSec)
	require.NotNil(t, plan.EffectiveMaxDurationSeconds)
	assert.Equal(t, int32(3600), *plan.EffectiveMaxDurationSeconds)

	assert.Equal(t, 1, store.getByIdempotencyKeyCalls)
	assert.Equal(t, "upstream-retry-key-1", store.lastGetByIdempotencyKey)
	assert.Equal(t, 0, store.listCandidatesCalls,
		"replay must not re-run the selector")
	assert.Equal(t, 0, store.insertEventCalls,
		"replay must not re-insert the event")
}

// TestService_Start_ExternalReferenceReplayReturnsExistingEvent: when
// idempotency_key is absent but (external_source, external_reference)
// match a prior call, the same replay semantics apply.
func TestService_Start_ExternalReferenceReplayReturnsExistingEvent(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	existingUUID := uuid.New()
	store := newFakeStore()
	store.eventsByExternalRef = map[string]*models.Event{
		"opensearch|alert-7788": {
			ID:                      77,
			EventUUID:               existingUUID,
			OrgID:                   orgID,
			State:                   models.EventStateRestoring,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 30,
		},
	}
	svc := NewService(store)

	req := validStartRequest(orgID)
	src := "opensearch"
	ref := "alert-7788"
	req.ExternalSource = &src
	req.ExternalReference = &ref

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan.EventUUID)
	assert.Equal(t, existingUUID, *plan.EventUUID)
	require.NotNil(t, plan.ReplayEvent)
	assert.Equal(t, models.EventStateRestoring, plan.ReplayEvent.State)

	assert.Equal(t, 1, store.getByExternalRefCalls)
	assert.Equal(t, "opensearch", store.lastGetByExternalRefSource)
	assert.Equal(t, "alert-7788", store.lastGetByExternalRefRef)
	assert.Equal(t, 0, store.insertEventCalls)
}

// TestService_Start_IdempotencyKeyMissesFallsThrough: a non-matching key
// proceeds to the normal selector + insert path. The lookup is recorded
// but does not block the insert.
func TestService_Start_IdempotencyKeyMissesFallsThrough(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	svc := NewService(store)

	req := validStartRequest(orgID)
	req.TargetKW = 2 // pick "worst"
	key := "new-key-not-seen-before"
	req.IdempotencyKey = &key

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan.EventUUID, "miss path persists a new event")
	assert.Equal(t, 1, store.getByIdempotencyKeyCalls)
	assert.Equal(t, 1, store.insertEventCalls)
}

// TestService_Start_IdempotencyKeyPrecedesExternalReference: when both
// channels are present, idempotency_key wins. An operator-supplied retry
// handle overrides upstream re-delivery.
func TestService_Start_IdempotencyKeyPrecedesExternalReference(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	keyUUID := uuid.New()
	refUUID := uuid.New()
	store := newFakeStore()
	store.eventsByIdempotencyKey = map[string]*models.Event{
		"key-1": {ID: 1, EventUUID: keyUUID, OrgID: orgID, State: models.EventStateActive},
	}
	store.eventsByExternalRef = map[string]*models.Event{
		"src|ref": {ID: 2, EventUUID: refUUID, OrgID: orgID, State: models.EventStateActive},
	}
	svc := NewService(store)

	req := validStartRequest(orgID)
	key := "key-1"
	src := "src"
	ref := "ref"
	req.IdempotencyKey = &key
	req.ExternalSource = &src
	req.ExternalReference = &ref

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan.EventUUID)
	assert.Equal(t, keyUUID, *plan.EventUUID, "idempotency_key replay must win over external_reference")
	assert.Equal(t, 1, store.getByIdempotencyKeyCalls)
	assert.Equal(t, 0, store.getByExternalRefCalls, "external_reference lookup must short-circuit")
}

// TestService_Start_IdempotencyKeyLookupErrorPropagates: a lookup failure
// surfaces unchanged so transient db errors are visible rather than
// silently falling through to a double-insert attempt.
func TestService_Start_IdempotencyKeyLookupErrorPropagates(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.getByIdempotencyKeyErr = errors.New("db down")
	svc := NewService(store)

	req := validStartRequest(orgID)
	key := "test-key"
	req.IdempotencyKey = &key

	_, err := svc.Start(t.Context(), req)
	require.Error(t, err)
	assert.ErrorContains(t, err, "db down")
	assert.Equal(t, 0, store.insertEventCalls)
}

// TestService_Start_PartialExternalReferenceFieldsSkipLookup: external
// reference is two-of-two — only source set, or only reference set, must
// not trigger a lookup (the partial unique index requires both anyway).
func TestService_Start_PartialExternalReferenceFieldsSkipLookup(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	for _, tc := range []struct {
		name string
		src  *string
		ref  *string
	}{
		{"source only", strPtr("opensearch"), nil},
		{"reference only", nil, strPtr("alert-1")},
		{"both empty strings", strPtr(""), strPtr("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Fresh store per subtest so getByExternalRefCalls stays
			// isolated under t.Parallel() without sharing a mutex.
			store := newFakeStore()
			store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
			store.candidatesByOrg[orgID] = []*models.Candidate{
				minerWithEff("worst", 3000, 100, 50),
			}
			svc := NewService(store)

			req := validStartRequest(orgID)
			req.TargetKW = 2
			req.ExternalSource = tc.src
			req.ExternalReference = tc.ref
			_, err := svc.Start(t.Context(), req)
			require.NoError(t, err)
			assert.Equal(t, 0, store.getByExternalRefCalls,
				"partial external-ref fields must skip the lookup")
		})
	}
}

func strPtr(s string) *string { return &s }

// Two concurrent Starts sharing an idempotency_key: the loser's
// InsertEventWithTargets surfaces ErrCurtailmentReplayRaceLoss; the
// service retries the lookup and returns the winner's persisted event.
func TestService_Start_IdempotencyKeyRaceLoserReplays(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	winnerUUID := uuid.New()
	winnerMaxDur := int32(3600)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	// First lookup miss → insert attempted → race-loss sentinel → second
	// lookup (post-race) sees the winner's row and replays.
	store.insertEventErr = interfaces.ErrCurtailmentReplayRaceLoss
	store.eventsByIdempotencyKeyOnRetry = map[string]*models.Event{
		"shared-key": {
			ID:                      99,
			EventUUID:               winnerUUID,
			OrgID:                   orgID,
			State:                   models.EventStatePending,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 120,
			MaxDurationSeconds:      &winnerMaxDur,
		},
	}
	svc := NewService(store)

	req := validStartRequest(orgID)
	req.TargetKW = 2
	key := "shared-key"
	req.IdempotencyKey = &key

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.NotNil(t, plan.EventUUID)
	assert.Equal(t, winnerUUID, *plan.EventUUID, "race-loser must replay the winner's event")
	require.NotNil(t, plan.ReplayEvent)
	assert.Equal(t, models.EventStatePending, plan.ReplayEvent.State)
	assert.Equal(t, 1, store.insertEventCalls, "loser issues exactly one Insert attempt")
	assert.Equal(t, 2, store.getByIdempotencyKeyCalls, "lookup runs twice: pre-insert miss + post-race retry")
}

// TestService_Start_ExternalReferenceRaceLoserReplays: same race-loser
// contract as the idempotency_key path, but the constraint that fires is the
// (org_id, external_source, external_reference) partial unique index. Verify
// the loser falls into the same replay branch rather than surfacing
// AlreadyExists.
func TestService_Start_ExternalReferenceRaceLoserReplays(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	winnerUUID := uuid.New()
	winnerMaxDur := int32(3600)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	store.insertEventErr = interfaces.ErrCurtailmentReplayRaceLoss
	store.eventsByExternalRefOnRetry = map[string]*models.Event{
		"opensearch|alert-7": {
			ID:                      77,
			EventUUID:               winnerUUID,
			OrgID:                   orgID,
			State:                   models.EventStatePending,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 120,
			MaxDurationSeconds:      &winnerMaxDur,
		},
	}
	svc := NewService(store)

	req := validStartRequest(orgID)
	req.TargetKW = 2
	src := "opensearch"
	ref := "alert-7"
	req.ExternalSource = &src
	req.ExternalReference = &ref

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.NotNil(t, plan.EventUUID)
	assert.Equal(t, winnerUUID, *plan.EventUUID, "race-loser must replay the winner's event")
	assert.Equal(t, 1, store.insertEventCalls)
	assert.Equal(t, 2, store.getByExternalRefCalls, "lookup runs twice: pre-insert miss + post-race retry")
}

// TestService_Start_IdempotencyKeyOrgConflictRaceLoserReplays pins the case
// where PostgreSQL reports the org-level one-non-terminal-event constraint
// before the idempotency partial unique index. The duplicate request is still
// a valid replay and must not surface a generic active-event conflict.
func TestService_Start_IdempotencyKeyOrgConflictRaceLoserReplays(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	winnerUUID := uuid.New()
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	store.insertEventErr = interfaces.ErrCurtailmentNonTerminalEventExists
	store.eventsByIdempotencyKeyOnRetry = map[string]*models.Event{
		"shared-key": {
			ID:                      99,
			EventUUID:               winnerUUID,
			OrgID:                   orgID,
			State:                   models.EventStateActive,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 120,
		},
	}
	svc := NewService(store)

	req := validStartRequest(orgID)
	req.TargetKW = 2
	key := "shared-key"
	req.IdempotencyKey = &key

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.NotNil(t, plan.EventUUID)
	assert.Equal(t, winnerUUID, *plan.EventUUID)
	require.NotNil(t, plan.ReplayEvent)
	assert.Equal(t, models.EventStateActive, plan.ReplayEvent.State)
	assert.Equal(t, 1, store.insertEventCalls)
	assert.Equal(t, 2, store.getByIdempotencyKeyCalls,
		"org-conflict loser must retry replay lookup before falling back to active-event conflict")
}

// TestService_Start_ExternalReferenceOrgConflictRaceLoserReplays mirrors the
// org-level unique-conflict path for upstream webhook references.
func TestService_Start_ExternalReferenceOrgConflictRaceLoserReplays(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	winnerUUID := uuid.New()
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	store.insertEventErr = interfaces.ErrCurtailmentNonTerminalEventExists
	store.eventsByExternalRefOnRetry = map[string]*models.Event{
		"opensearch|alert-7": {
			ID:                      77,
			EventUUID:               winnerUUID,
			OrgID:                   orgID,
			State:                   models.EventStatePending,
			Mode:                    models.ModeFixedKw,
			Strategy:                models.StrategyLeastEfficientFirst,
			Level:                   models.LevelFull,
			Priority:                models.PriorityNormal,
			RestoreBatchSize:        10,
			RestoreBatchIntervalSec: 120,
		},
	}
	svc := NewService(store)

	req := validStartRequest(orgID)
	req.TargetKW = 2
	src := "opensearch"
	ref := "alert-7"
	req.ExternalSource = &src
	req.ExternalReference = &ref

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan)
	require.NotNil(t, plan.EventUUID)
	assert.Equal(t, winnerUUID, *plan.EventUUID)
	require.NotNil(t, plan.ReplayEvent)
	assert.Equal(t, models.EventStatePending, plan.ReplayEvent.State)
	assert.Equal(t, 1, store.insertEventCalls)
	assert.Equal(t, 2, store.getByExternalRefCalls,
		"org-conflict loser must retry external-reference replay before falling back to active-event conflict")
}

// TestService_Start_RaceLoserPostRetryMissSurfacesAlreadyExists: the
// race-loser's retry lookup *can* legitimately return nil if the winning
// transaction rolled back between the unique-violation observation and the
// retry. The loser must then surface AlreadyExists rather than treat the
// 23505 as a generic error.
func TestService_Start_RaceLoserPostRetryMissSurfacesAlreadyExists(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	store.insertEventErr = interfaces.ErrCurtailmentReplayRaceLoss
	// No entry under eventsByIdempotencyKeyOnRetry → retry lookup misses.
	svc := NewService(store)

	req := validStartRequest(orgID)
	req.TargetKW = 2
	key := "shared-key"
	req.IdempotencyKey = &key

	_, err := svc.Start(t.Context(), req)
	require.Error(t, err)
	assert.True(t, fleeterror.IsAlreadyExistsError(err),
		"race-loser with no retry-visible winner surfaces AlreadyExists")
}

// A webhook retry whose key matches a terminal event must NOT return
// the historical row as a replay — once terminal, the partial unique
// index releases the key and a fresh Start fires.
func TestService_Start_IdempotencyKeyTerminalEventTreatedAsFreshStart(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	// Seed a year-old terminal event under the same key. The lookup
	// MUST treat this as not-found so a fresh Start fires.
	store.eventsByIdempotencyKey = map[string]*models.Event{
		"reused-key": {
			ID:        99,
			EventUUID: uuid.New(),
			OrgID:     orgID,
			State:     models.EventStateCompleted,
		},
	}
	svc := NewService(store)

	req := validStartRequest(orgID)
	req.TargetKW = 2
	key := "reused-key"
	req.IdempotencyKey = &key

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan.EventUUID, "terminal event must not block a fresh Start; new event_uuid expected")
	assert.Nil(t, plan.ReplayEvent,
		"replay path must not fire when the matching row is terminal")
	assert.Equal(t, 1, store.insertEventCalls,
		"fresh Start must reach InsertEventWithTargets; a terminal-only match must not short-circuit to replay")
}

// TestService_Start_ExternalReferenceTerminalEventTreatedAsFreshStart
// mirrors the AD2 fix for the (external_source, external_reference)
// webhook-dedupe channel. Symmetric to the idempotency_key case.
func TestService_Start_ExternalReferenceTerminalEventTreatedAsFreshStart(t *testing.T) {
	t.Parallel()
	const orgID = int64(42)
	store := newFakeStore()
	store.orgConfigByOrg[orgID] = defaultOrgConfig(orgID)
	store.candidatesByOrg[orgID] = []*models.Candidate{
		minerWithEff("worst", 3000, 100, 50),
	}
	store.eventsByExternalRef = map[string]*models.Event{
		"opensearch|reused-alert": {
			ID:        77,
			EventUUID: uuid.New(),
			OrgID:     orgID,
			State:     models.EventStateCancelled,
		},
	}
	svc := NewService(store)

	req := validStartRequest(orgID)
	req.TargetKW = 2
	src := "opensearch"
	ref := "reused-alert"
	req.ExternalSource = &src
	req.ExternalReference = &ref

	plan, err := svc.Start(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, plan.EventUUID, "terminal event must not block a fresh Start; new event_uuid expected")
	assert.Nil(t, plan.ReplayEvent,
		"replay path must not fire when the matching row is terminal")
	assert.Equal(t, 1, store.insertEventCalls,
		"fresh Start must reach InsertEventWithTargets")
}
