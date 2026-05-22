package curtailment

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
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
