package evidence

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/internal/domain/rollout"
)

type fakeStore struct {
	mu sync.Mutex

	candidates       []Candidate
	snapshots        map[int64]Snapshot
	refreshErrs      map[int64]error
	updateErrs       map[int64]error
	updateLost       map[int64]bool
	automationErrs   map[int64]error
	updates          []Summary
	automationErrors []Summary
	listLimits       []int32
	listCalls        int
}

func (s *fakeStore) ListCandidates(_ context.Context, limit int32) ([]Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	s.listLimits = append(s.listLimits, limit)
	return append([]Candidate(nil), s.candidates...), nil
}

func (s *fakeStore) Refresh(
	_ context.Context,
	candidate Candidate,
	_ time.Time,
) (Snapshot, error) {
	if err := s.refreshErrs[candidate.BatchID]; err != nil {
		return Snapshot{}, err
	}
	return s.snapshots[candidate.BatchID], nil
}

func (s *fakeStore) UpdateSummary(_ context.Context, summary Summary) (bool, error) {
	if err := s.updateErrs[summary.BatchID]; err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = append(s.updates, summary)
	return !s.updateLost[summary.BatchID], nil
}

func (s *fakeStore) MarkAutomationError(_ context.Context, summary Summary) error {
	if err := s.automationErrs[summary.BatchID]; err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.automationErrors = append(s.automationErrors, summary)
	return nil
}

type fakeController struct {
	mu       sync.Mutex
	requests []rollout.AdmitRequest
	err      error
}

func (c *fakeController) Continue(
	_ context.Context,
	req rollout.AdmitRequest,
) (*rollout.Rollout, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	return &rollout.Rollout{ID: req.RolloutID, OrgID: req.OrgID}, c.err
}

func (s *fakeStore) updateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.updates)
}

func (s *fakeStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listCalls
}

func TestEvaluatorStartValidatesDependenciesAndConfiguration(t *testing.T) {
	t.Parallel()

	err := NewEvaluator(Config{}, nil, nil).Start(t.Context())
	require.ErrorContains(t, err, "store is required")

	err = NewEvaluator(
		Config{TickInterval: 500 * time.Millisecond},
		&fakeStore{},
		nil,
	).Start(t.Context())
	require.ErrorContains(t, err, "tick_interval must be at least 1s")
}

func TestEvaluatorLifecycleRunsImmediatelyTicksStopsAndRestarts(t *testing.T) {
	store := &fakeStore{}
	evaluator := NewEvaluator(Config{TickInterval: time.Second, BatchSize: 7}, store, nil)

	require.NoError(t, evaluator.Start(context.Background()))
	require.Eventually(t, func() bool { return store.callCount() >= 1 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return store.callCount() >= 2 }, 1500*time.Millisecond, 10*time.Millisecond)
	require.NoError(t, evaluator.Stop(context.Background()))
	stoppedAt := store.callCount()
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, stoppedAt, store.callCount())

	require.NoError(t, evaluator.Start(context.Background()))
	require.Eventually(t, func() bool { return store.callCount() > stoppedAt }, time.Second, time.Millisecond)
	require.NoError(t, evaluator.Stop(context.Background()))
	require.Equal(t, []int32{7, 7, 7}, store.listLimits)
}

func TestEvaluatorRunOnceIsolatesCandidateErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	first := testCandidate(1, now.Add(-time.Minute))
	second := testCandidate(2, now.Add(-time.Minute))
	store := &fakeStore{
		candidates:  []Candidate{first, second},
		snapshots:   map[int64]Snapshot{second.BatchID: completeSnapshot(now)},
		refreshErrs: map[int64]error{first.BatchID: errors.New("candidate failed")},
	}
	evaluator := NewEvaluator(Config{}, store, nil)
	evaluator.now = func() time.Time { return now }

	evaluator.RunOnce(t.Context())

	require.Len(t, store.updates, 1)
	assert.Equal(t, second.BatchID, store.updates[0].BatchID)
}

func TestBuildSummaryUsesPairedEqualMemberWeighting(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-time.Minute))
	summary := buildSummary(candidate, Snapshot{Members: []MemberEvidence{
		{
			MemberID:           1,
			BaselineHashrateHS: float64Pointer(100),
			PostHashrateHS:     float64Pointer(80),
			PostObservedAt:     timePointer(now.Add(-time.Second)),
		},
		{
			MemberID:           2,
			BaselineHashrateHS: float64Pointer(200),
			PostHashrateHS:     float64Pointer(220),
			PostObservedAt:     timePointer(now.Add(-time.Second)),
		},
		{
			MemberID:           3,
			BaselineHashrateHS: float64Pointer(300),
		},
	}}, now)

	assert.Equal(t, int64(3), summary.TotalCount)
	assert.Equal(t, int64(2), summary.PairedCount)
	require.NotNil(t, summary.CumulativeBaselineHashrateHS)
	require.NotNil(t, summary.CumulativeCurrentHashrateHS)
	require.NotNil(t, summary.CumulativeDeltaBasisPoints)
	assert.InDelta(t, 150, *summary.CumulativeBaselineHashrateHS, 0.001)
	assert.InDelta(t, 150, *summary.CumulativeCurrentHashrateHS, 0.001)
	assert.Equal(t, int32(0), *summary.CumulativeDeltaBasisPoints)
	assert.Equal(t, rollout.EvidenceStatusStale, summary.Status)
}

func TestBuildSummaryMakesMissingOrZeroBaselineUnavailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	for _, baseline := range []*float64{nil, float64Pointer(0)} {
		candidate := testCandidate(1, now.Add(-time.Minute))
		summary := buildSummary(candidate, Snapshot{Members: []MemberEvidence{{
			MemberID:           1,
			BaselineHashrateHS: baseline,
			PostHashrateHS:     float64Pointer(100),
			PostObservedAt:     timePointer(now),
		}}}, now)

		assert.Equal(t, rollout.EvidenceStatusUnavailable, summary.Status)
		assert.Zero(t, summary.PairedCount)
		assert.Nil(t, summary.CumulativeDeltaBasisPoints)
	}
}

func TestBuildSummaryPersistsOnlyNewPolicyBuckets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 0, 30, 0, time.UTC)
	boundary := now.Add(-10 * time.Second)
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	candidate.LastPolicyBucketBoundary = &boundary
	candidate.LatestPolicyBucketHashrateHS = float64Pointer(95)
	candidate.LatestPolicyBucketDeltaBasisPoints = int32Pointer(-500)
	snapshot := completeSnapshot(now)
	snapshot.PolicyBuckets = []PolicyBucket{{
		Boundary: boundary,
		Members: []BucketMember{
			{MemberID: 1, AvgHashrateHS: 90, ObservedAt: now.Add(-11 * time.Second)},
			{MemberID: 2, AvgHashrateHS: 100, ObservedAt: now.Add(-11 * time.Second)},
		},
	}}

	summary := buildSummary(candidate, snapshot, now)

	assert.Equal(t, boundary, *summary.LastPolicyBucketBoundary)
	assert.Equal(t, float64(95), *summary.LatestPolicyBucketHashrateHS)
	assert.Equal(t, int32(-500), *summary.LatestPolicyBucketDeltaBasisPoints)
}

func TestBuildSummaryStoresLatestCompletePolicyBucketWithEqualWeighting(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 0, 30, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	snapshot := completeSnapshot(now)
	snapshot.PolicyBuckets = []PolicyBucket{{
		Boundary: now.Add(-time.Second),
		Members: []BucketMember{
			{MemberID: 1, AvgHashrateHS: 80, ObservedAt: now.Add(-2 * time.Second)},
			{MemberID: 2, AvgHashrateHS: 220, ObservedAt: now.Add(-2 * time.Second)},
		},
	}}

	summary := buildSummary(candidate, snapshot, now)

	require.NotNil(t, summary.LatestPolicyBucketHashrateHS)
	require.NotNil(t, summary.LatestPolicyBucketDeltaBasisPoints)
	assert.InDelta(t, 150, *summary.LatestPolicyBucketHashrateHS, 0.001)
	assert.Equal(t, int32(0), *summary.LatestPolicyBucketDeltaBasisPoints)
}

func TestBuildSummaryPolicyThresholdEqualityStartsHealthyAtBucketStart(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 0, 30, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	candidate.MaxDropBasisPoints = 100
	candidate.HealthyDurationSeconds = 30
	snapshot := completeSnapshot(now)
	snapshot.PolicyBuckets = []PolicyBucket{policyBucket(now, -100)}

	summary := buildSummary(candidate, snapshot, now)

	assert.Equal(t, rollout.EvidenceStatusObserving, summary.Status)
	require.NotNil(t, summary.HealthySince)
	assert.Equal(t, now.Add(-10*time.Second), *summary.HealthySince)
}

func TestBuildSummaryPolicyDwellRequiresExactlyContiguousBuckets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	candidate.MaxDropBasisPoints = 100
	candidate.HealthyDurationSeconds = 30
	candidate.HealthySince = timePointer(now.Add(-30 * time.Second))
	candidate.LastPolicyBucketBoundary = timePointer(now.Add(-10 * time.Second))
	snapshot := completeSnapshot(now)
	snapshot.PolicyBuckets = []PolicyBucket{policyBucket(now, 100)}

	summary := buildSummary(candidate, snapshot, now)
	assert.Equal(t, rollout.EvidenceStatusHealthy, summary.Status)
	assert.Equal(t, now.Add(-30*time.Second), *summary.HealthySince)

	gapped := candidate
	gapped.HealthySince = timePointer(now.Add(-40 * time.Second))
	gapped.LastPolicyBucketBoundary = timePointer(now.Add(-30 * time.Second))
	snapshot.PolicyBuckets = []PolicyBucket{policyBucket(now.Add(-10*time.Second), 0)}
	summary = buildSummary(gapped, snapshot, now)
	assert.Equal(t, rollout.EvidenceStatusObserving, summary.Status)
	assert.Equal(t, now.Add(-20*time.Second), *summary.HealthySince)
}

func TestBuildSummaryPolicyBadBucketHoldsAndLaterHealthyBucketRestartsDwell(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	candidate.MaxDropBasisPoints = 100
	candidate.HealthyDurationSeconds = 20
	candidate.HealthySince = timePointer(now.Add(-30 * time.Second))
	candidate.LastPolicyBucketBoundary = timePointer(now.Add(-20 * time.Second))
	snapshot := completeSnapshot(now)
	snapshot.PolicyBuckets = []PolicyBucket{
		policyBucket(now.Add(-10*time.Second), -101),
		policyBucket(now, 0),
	}

	summary := buildSummary(candidate, snapshot, now)

	assert.Equal(t, rollout.EvidenceStatusObserving, summary.Status)
	assert.Equal(t, now.Add(-10*time.Second), *summary.HealthySince)

	snapshot.PolicyBuckets = snapshot.PolicyBuckets[:1]
	summary = buildSummary(candidate, snapshot, now)
	assert.Equal(t, rollout.EvidenceStatusHeld, summary.Status)
	assert.Nil(t, summary.HealthySince)
}

func TestBuildSummaryPolicyFreshnessCountsOnlyNewFreshBuckets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	candidate.MaxDropBasisPoints = 100
	candidate.HealthyDurationSeconds = 30
	candidate.HealthySince = timePointer(now.Add(-30 * time.Second))
	candidate.LastPolicyBucketBoundary = timePointer(now.Add(-10 * time.Second))
	snapshot := completeSnapshot(now)
	snapshot.PolicyBuckets = []PolicyBucket{policyBucket(now, 0)}

	summary := buildSummary(candidate, snapshot, now)

	assert.Equal(t, rollout.EvidenceStatusHealthy, summary.Status)
	assert.Equal(t, now.Add(-30*time.Second), *summary.HealthySince)

	candidate.Status = summary.Status
	candidate.HealthySince = summary.HealthySince
	candidate.LastPolicyBucketBoundary = summary.LastPolicyBucketBoundary
	candidate.EvaluatedAt = timePointer(now)
	later := now.Add(21 * time.Second)
	summary = buildSummary(candidate, completeSnapshot(later), later)
	assert.Equal(t, rollout.EvidenceStatusStale, summary.Status)
	assert.Nil(t, summary.HealthySince)
}

func TestBuildSummaryPolicyFreshnessDoesNotPreserveHealthyAcrossMismatchedSamples(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	candidate.MaxDropBasisPoints = 100
	candidate.HealthyDurationSeconds = 30
	candidate.Status = rollout.EvidenceStatusHealthy
	candidate.HealthySince = timePointer(now.Add(-40 * time.Second))
	candidate.LastPolicyBucketBoundary = timePointer(now.Add(-21 * time.Second))

	first := buildSummary(candidate, completeSnapshot(now), now)
	assert.Equal(t, rollout.EvidenceStatusStale, first.Status)
	assert.Nil(t, first.HealthySince)

	later := now.Add(5 * time.Second)
	candidate.Status = first.Status
	candidate.HealthySince = first.HealthySince
	second := buildSummary(candidate, completeSnapshot(later), later)
	assert.Equal(t, rollout.EvidenceStatusStale, second.Status)
	assert.Nil(t, second.HealthySince)
}

func TestBuildSummaryPolicyFreshnessRequiresCompleteBucketAfterDeadline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-21*time.Second))
	candidate.PolicyEnabled = true
	candidate.MaxDropBasisPoints = 100
	candidate.HealthyDurationSeconds = 30

	summary := buildSummary(candidate, completeSnapshot(now), now)

	assert.Equal(t, rollout.EvidenceStatusStale, summary.Status)
	assert.Nil(t, summary.HealthySince)
}

func TestBuildSummaryRecentCompleteBucketRecoversStaleWithNewDwell(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	candidate.MaxDropBasisPoints = 100
	candidate.HealthyDurationSeconds = 30
	candidate.Status = rollout.EvidenceStatusStale
	candidate.LastPolicyBucketBoundary = timePointer(now.Add(-30 * time.Second))
	snapshot := completeSnapshot(now)
	snapshot.PolicyBuckets = []PolicyBucket{policyBucket(now, 0)}

	summary := buildSummary(candidate, snapshot, now)

	assert.Equal(t, rollout.EvidenceStatusObserving, summary.Status)
	require.NotNil(t, summary.HealthySince)
	assert.Equal(t, now.Add(-10*time.Second), *summary.HealthySince)
}

func TestBuildSummaryEvaluatorOutageCheckpointsWithoutCountingBuckets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	candidate.MaxDropBasisPoints = 100
	candidate.HealthyDurationSeconds = 20
	candidate.Status = rollout.EvidenceStatusObserving
	candidate.HealthySince = timePointer(now.Add(-40 * time.Second))
	candidate.LastPolicyBucketBoundary = timePointer(now.Add(-30 * time.Second))
	candidate.EvaluatedAt = timePointer(now.Add(-21 * time.Second))
	snapshot := completeSnapshot(now)
	snapshot.PolicyBuckets = []PolicyBucket{
		policyBucket(now.Add(-20*time.Second), 0),
		policyBucket(now.Add(-10*time.Second), 0),
		policyBucket(now, 0),
	}

	afterOutage := buildSummary(candidate, snapshot, now)
	assert.Equal(t, rollout.EvidenceStatusStale, afterOutage.Status)
	assert.Nil(t, afterOutage.HealthySince)
	require.NotNil(t, afterOutage.LastPolicyBucketBoundary)
	assert.Equal(t, now, *afterOutage.LastPolicyBucketBoundary)

	candidate.Status = afterOutage.Status
	candidate.HealthySince = afterOutage.HealthySince
	candidate.LastPolicyBucketBoundary = afterOutage.LastPolicyBucketBoundary
	candidate.EvaluatedAt = timePointer(now)
	withoutFutureBucket := buildSummary(candidate, completeSnapshot(now.Add(5*time.Second)), now.Add(5*time.Second))
	assert.Equal(t, rollout.EvidenceStatusStale, withoutFutureBucket.Status)
	assert.Nil(t, withoutFutureBucket.HealthySince)

	future := now.Add(10 * time.Second)
	recoverySnapshot := completeSnapshot(future)
	recoverySnapshot.PolicyBuckets = []PolicyBucket{policyBucket(future, 0)}
	candidate.EvaluatedAt = timePointer(now.Add(5 * time.Second))
	recovered := buildSummary(candidate, recoverySnapshot, future)
	assert.Equal(t, rollout.EvidenceStatusObserving, recovered.Status)
	require.NotNil(t, recovered.HealthySince)
	assert.Equal(t, now, *recovered.HealthySince)
}

func TestBuildSummaryRejectsInvalidBucketObservationTimes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		observedAt time.Time
	}{
		{name: "zero", observedAt: time.Time{}},
		{name: "future", observedAt: now.Add(time.Second)},
		{name: "older than freshness window", observedAt: now.Add(-21 * time.Second)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			candidate := automaticCandidate(now)
			snapshot := automaticHealthySnapshot(now)
			snapshot.PolicyBuckets[0].Members[0].ObservedAt = test.observedAt

			summary := buildSummary(candidate, snapshot, now)

			assert.Equal(t, rollout.EvidenceStatusStale, summary.Status)
			assert.Nil(t, summary.HealthySince)
		})
	}
}

func TestBuildSummaryRejectsNonFiniteAndNegativeTelemetry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	for _, value := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		baselineCandidate := testCandidate(1, now.Add(-time.Minute))
		baselineSnapshot := completeSnapshot(now)
		baselineSnapshot.Members[0].BaselineHashrateHS = float64Pointer(value)
		baseline := buildSummary(baselineCandidate, baselineSnapshot, now)
		assert.Equal(t, rollout.EvidenceStatusUnavailable, baseline.Status)

		postCandidate := testCandidate(1, now.Add(-time.Minute))
		postSnapshot := completeSnapshot(now)
		postSnapshot.Members[0].PostHashrateHS = float64Pointer(value)
		post := buildSummary(postCandidate, postSnapshot, now)
		assert.Equal(t, rollout.EvidenceStatusStale, post.Status)
		assert.Equal(t, int64(1), post.PairedCount)

		bucketCandidate := automaticCandidate(now)
		bucketSnapshot := automaticHealthySnapshot(now)
		bucketSnapshot.PolicyBuckets[0].Members[0].AvgHashrateHS = value
		bucket := buildSummary(bucketCandidate, bucketSnapshot, now)
		assert.Equal(t, rollout.EvidenceStatusStale, bucket.Status)
		assert.Nil(t, bucket.HealthySince)
	}

	assert.Nil(t, deltaBasisPoints(math.NaN(), 100))
	assert.Nil(t, deltaBasisPoints(100, math.Inf(1)))
	assert.Nil(t, deltaBasisPoints(100, -1))
}

func TestBuildSummaryProcessesClosingPolicyBucketBeforeFinalizing(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	now := completedAt.Add(postWindow)
	for _, test := range []struct {
		name       string
		delta      int32
		wantStatus rollout.EvidenceStatus
	}{
		{name: "full dwell becomes healthy", delta: 0, wantStatus: rollout.EvidenceStatusHealthy},
		{name: "closing violation remains held", delta: -101, wantStatus: rollout.EvidenceStatusHeld},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			candidate := testCandidate(1, completedAt)
			candidate.PolicyEnabled = true
			candidate.MaxDropBasisPoints = 100
			candidate.HealthyDurationSeconds = 1800
			candidate.Status = rollout.EvidenceStatusObserving
			candidate.HealthySince = timePointer(completedAt)
			candidate.LastPolicyBucketBoundary = timePointer(now.Add(-10 * time.Second))
			candidate.EvaluatedAt = timePointer(now.Add(-5 * time.Second))
			snapshot := completeSnapshot(now)
			snapshot.PolicyBuckets = []PolicyBucket{policyBucket(now, test.delta)}

			summary := buildSummary(candidate, snapshot, now)

			assert.Equal(t, test.wantStatus, summary.Status)
			assert.True(t, summary.PostWindowFinalized)
			require.NotNil(t, summary.LastPolicyBucketBoundary)
			assert.Equal(t, now, *summary.LastPolicyBucketBoundary)
		})
	}
}

func TestEvaluatorAutoContinuesOnFinalHealthyPolicyBucket(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	now := completedAt.Add(postWindow)
	candidate := automaticCandidate(now)
	candidate.CompletedAt = completedAt
	candidate.HealthyDurationSeconds = 1800
	candidate.Status = rollout.EvidenceStatusObserving
	candidate.HealthySince = timePointer(completedAt)
	candidate.LastPolicyBucketBoundary = timePointer(now.Add(-10 * time.Second))
	candidate.EvaluatedAt = timePointer(now.Add(-5 * time.Second))
	snapshot := completeSnapshot(now)
	snapshot.PolicyBuckets = []PolicyBucket{policyBucket(now, 0)}
	store := &fakeStore{
		candidates: []Candidate{candidate},
		snapshots:  map[int64]Snapshot{candidate.BatchID: snapshot},
	}
	controller := &fakeController{}
	evaluator := NewEvaluator(Config{}, store, controller)
	evaluator.now = func() time.Time { return now }

	evaluator.RunOnce(t.Context())

	require.Len(t, store.updates, 1)
	assert.True(t, store.updates[0].PostWindowFinalized)
	assert.Equal(t, rollout.EvidenceStatusHealthy, store.updates[0].Status)
	require.Len(t, controller.requests, 1)
	assert.Equal(t, autoContinueIdempotencyKey(candidate.BatchID), controller.requests[0].IdempotencyKey)
}

func TestEvaluatorAutoContinueUsesSystemIdentityAndOriginalStartedRevision(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	for _, test := range []struct {
		name             string
		controlStatus    *rollout.ControlStatus
		expectedRevision int64
		rolloutState     rollout.State
		rolloutRevision  int64
	}{
		{
			name:             "new healthy control",
			expectedRevision: 7,
			rolloutState:     rollout.StateReview,
			rolloutRevision:  7,
		},
		{
			name:             "started control recovery",
			controlStatus:    controlStatusPointer(rollout.ControlStatusStarted),
			expectedRevision: 7,
			rolloutState:     rollout.StateRunning,
			rolloutRevision:  8,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			candidate := automaticCandidate(now)
			candidate.RolloutState = test.rolloutState
			candidate.RolloutRevision = test.rolloutRevision
			candidate.AutoControlStatus = test.controlStatus
			candidate.AutoControlExpectedRevision = int64Pointer(test.expectedRevision)
			if test.controlStatus == nil {
				candidate.AutoControlExpectedRevision = nil
			} else {
				candidate.AutoControlResultingRevision = int64Pointer(test.rolloutRevision)
			}
			store := &fakeStore{
				candidates: []Candidate{candidate},
				snapshots: map[int64]Snapshot{
					candidate.BatchID: automaticHealthySnapshot(now),
				},
			}
			controller := &fakeController{}
			evaluator := NewEvaluator(Config{}, store, controller)
			evaluator.now = func() time.Time { return now }

			evaluator.RunOnce(t.Context())

			require.Len(t, controller.requests, 1)
			req := controller.requests[0]
			assert.Equal(t, test.expectedRevision, req.ExpectedRevision)
			assert.Equal(t, rollout.ActorTypeSystem, req.ActorType)
			assert.Nil(t, req.ActorCredentialID)
			assert.Equal(t, candidate.RolloutCreatedByUserID, req.ActorUserID)
			assert.Equal(t, autoContinueIdempotencyKey(candidate.BatchID), req.IdempotencyKey)
		})
	}
}

func TestEvaluatorAutoContinueSafetyStatesAndControlOutcomes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	t.Run("final review batch", func(t *testing.T) {
		t.Parallel()

		candidate := automaticCandidate(now)
		candidate.HasPendingBatch = false
		store := &fakeStore{
			candidates: []Candidate{candidate},
			snapshots: map[int64]Snapshot{
				candidate.BatchID: automaticHealthySnapshot(now),
			},
		}
		controller := &fakeController{}
		evaluator := NewEvaluator(Config{}, store, controller)
		evaluator.now = func() time.Time { return now }

		evaluator.RunOnce(t.Context())

		assert.Empty(t, controller.requests)
	})

	for _, state := range []rollout.State{
		rollout.StatePaused,
		rollout.StateRunning,
		rollout.StateAborted,
		rollout.StateCompleted,
		rollout.StateReverted,
	} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			candidate := automaticCandidate(now)
			candidate.RolloutState = state
			store := &fakeStore{
				candidates: []Candidate{candidate},
				snapshots: map[int64]Snapshot{
					candidate.BatchID: automaticHealthySnapshot(now),
				},
			}
			controller := &fakeController{}
			evaluator := NewEvaluator(Config{}, store, controller)
			evaluator.now = func() time.Time { return now }

			evaluator.RunOnce(t.Context())

			assert.Empty(t, controller.requests)
		})
	}

	for _, status := range []rollout.ControlStatus{
		rollout.ControlStatusSucceeded,
		rollout.ControlStatusFailed,
	} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			candidate := automaticCandidate(now)
			candidate.AutoControlStatus = controlStatusPointer(status)
			store := &fakeStore{
				candidates: []Candidate{candidate},
				snapshots: map[int64]Snapshot{
					candidate.BatchID: automaticHealthySnapshot(now),
				},
			}
			controller := &fakeController{}
			evaluator := NewEvaluator(Config{}, store, controller)
			evaluator.now = func() time.Time { return now }

			evaluator.RunOnce(t.Context())

			assert.Empty(t, controller.requests)
			if status == rollout.ControlStatusFailed {
				require.Len(t, store.automationErrors, 1)
				require.NotNil(t, store.automationErrors[0].ErrorMessage)
				assert.Equal(t, automaticContinueFailedMessage, *store.automationErrors[0].ErrorMessage)
			} else {
				assert.Empty(t, store.automationErrors)
			}
		})
	}
}

func TestAutoContinueIdempotencyKeyIsScopedToBatch(t *testing.T) {
	t.Parallel()

	assert.NotEqual(t, autoContinueIdempotencyKey(1), autoContinueIdempotencyKey(2))
}

func TestEvaluatorContinueErrorsRemainReconcileable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "revision conflict is benign", err: rollout.ErrRevisionConflict},
		{name: "ambiguous internal error is reconciled later", err: errors.New("raw dispatch timeout")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			candidate := automaticCandidate(now)
			store := &fakeStore{
				candidates: []Candidate{candidate},
				snapshots: map[int64]Snapshot{
					candidate.BatchID: automaticHealthySnapshot(now),
				},
			}
			controller := &fakeController{err: test.err}
			evaluator := NewEvaluator(Config{}, store, controller)
			evaluator.now = func() time.Time { return now }

			evaluator.RunOnce(t.Context())

			assert.Empty(t, store.automationErrors)
		})
	}
}

func TestEvaluatorRetriesAmbiguousNoRowControlWithSameKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	candidate := automaticCandidate(now)
	store := &fakeStore{
		candidates: []Candidate{candidate},
		snapshots: map[int64]Snapshot{
			candidate.BatchID: automaticHealthySnapshot(now),
		},
	}
	controller := &fakeController{err: errors.New("simulated timeout before control persisted")}
	evaluator := NewEvaluator(Config{}, store, controller)
	evaluator.now = func() time.Time { return now }

	evaluator.RunOnce(t.Context())
	evaluator.RunOnce(t.Context())

	require.Len(t, controller.requests, 2)
	assert.Equal(t, controller.requests[0].IdempotencyKey, controller.requests[1].IdempotencyKey)
	assert.Equal(t, controller.requests[0].ExpectedRevision, controller.requests[1].ExpectedRevision)
	assert.Empty(t, store.automationErrors)
}

func TestEvaluatorLostSummaryCASDoesNotAutoContinue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	candidate := automaticCandidate(now)
	store := &fakeStore{
		candidates: []Candidate{candidate},
		snapshots: map[int64]Snapshot{
			candidate.BatchID: automaticHealthySnapshot(now),
		},
		updateLost: map[int64]bool{candidate.BatchID: true},
	}
	controller := &fakeController{}
	evaluator := NewEvaluator(Config{}, store, controller)
	evaluator.now = func() time.Time { return now }

	evaluator.RunOnce(t.Context())

	assert.Empty(t, controller.requests)
}

func TestEvaluatorAutomationErrorStillRefreshesAndFinalizesEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 31, 0, 0, time.UTC)
	candidate := automaticCandidate(now)
	candidate.CompletedAt = now.Add(-31 * time.Minute)
	candidate.Status = rollout.EvidenceStatusAutomationError
	candidate.AutoControlStatus = controlStatusPointer(rollout.ControlStatusFailed)
	store := &fakeStore{
		candidates: []Candidate{candidate},
		snapshots: map[int64]Snapshot{
			candidate.BatchID: completeSnapshot(now),
		},
	}
	controller := &fakeController{}
	evaluator := NewEvaluator(Config{}, store, controller)
	evaluator.now = func() time.Time { return now }

	evaluator.RunOnce(t.Context())

	assert.Empty(t, store.automationErrors)
	require.Len(t, store.updates, 1)
	assert.Equal(t, rollout.EvidenceStatusAutomationError, store.updates[0].Status)
	assert.True(t, store.updates[0].PostWindowFinalized)
	assert.Empty(t, controller.requests)
}

func TestBuildSummaryMarksPolicyEvidenceStaleAndResetsHealthySince(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	healthySince := now.Add(-time.Minute)
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	candidate.Status = rollout.EvidenceStatusHealthy
	candidate.HealthySince = &healthySince
	snapshot := completeSnapshot(now)
	snapshot.Members[1].PostObservedAt = timePointer(now.Add(-21 * time.Second))

	summary := buildSummary(candidate, snapshot, now)

	assert.Equal(t, rollout.EvidenceStatusStale, summary.Status)
	assert.Nil(t, summary.HealthySince)
}

func TestBuildSummaryMarksManualEvidenceStaleAndReturnsToObservingWhenFresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-time.Minute))

	missing := completeSnapshot(now)
	missing.Members[1].PostHashrateHS = nil
	missing.Members[1].PostObservedAt = nil
	assert.Equal(
		t,
		rollout.EvidenceStatusStale,
		buildSummary(candidate, missing, now).Status,
	)

	old := completeSnapshot(now)
	old.Members[1].PostObservedAt = timePointer(now.Add(-21 * time.Second))
	assert.Equal(
		t,
		rollout.EvidenceStatusStale,
		buildSummary(candidate, old, now).Status,
	)

	freshCandidate := candidate
	freshCandidate.Status = rollout.EvidenceStatusStale
	assert.Equal(
		t,
		rollout.EvidenceStatusObserving,
		buildSummary(freshCandidate, completeSnapshot(now), now).Status,
	)
}

func TestBuildSummaryMarksMissingPolicySampleStaleOnlyAfterFreshnessDeadline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	for _, test := range []struct {
		name        string
		completedAt time.Time
		want        rollout.EvidenceStatus
	}{
		{
			name:        "still collecting before deadline",
			completedAt: now.Add(-19 * time.Second),
			want:        rollout.EvidenceStatusCollecting,
		},
		{
			name:        "stale after deadline",
			completedAt: now.Add(-21 * time.Second),
			want:        rollout.EvidenceStatusStale,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			candidate := testCandidate(1, test.completedAt)
			candidate.PolicyEnabled = true
			snapshot := completeSnapshot(now)
			snapshot.Members[1].PostHashrateHS = nil
			snapshot.Members[1].PostObservedAt = nil

			summary := buildSummary(candidate, snapshot, now)

			assert.Equal(t, test.want, summary.Status)
		})
	}
}

func TestBuildSummaryPreservesHealthyAndHeldStatusesWhileFresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 1, 0, 0, time.UTC)
	for _, status := range []rollout.EvidenceStatus{
		rollout.EvidenceStatusHealthy,
		rollout.EvidenceStatusHeld,
	} {
		candidate := testCandidate(1, now.Add(-time.Minute))
		candidate.PolicyEnabled = true
		candidate.Status = status
		candidate.LastPolicyBucketBoundary = timePointer(now.Add(-10 * time.Second))

		summary := buildSummary(candidate, completeSnapshot(now), now)

		assert.Equal(t, status, summary.Status)
	}
}

func TestBuildSummaryFinalizesWindowWithoutOverwritingPolicyVerdict(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 31, 0, 0, time.UTC)
	for _, status := range []rollout.EvidenceStatus{
		rollout.EvidenceStatusHealthy,
		rollout.EvidenceStatusHeld,
	} {
		candidate := testCandidate(1, now.Add(-31*time.Minute))
		candidate.PolicyEnabled = true
		candidate.Status = status

		summary := buildSummary(candidate, completeSnapshot(now), now)

		assert.Equal(t, status, summary.Status)
		assert.True(t, summary.PostWindowFinalized)
	}
}

func TestBuildSummaryFinalizesCompleteWindowAndRejectsIncompleteWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 31, 0, 0, time.UTC)
	candidate := testCandidate(1, now.Add(-31*time.Minute))

	complete := buildSummary(candidate, completeSnapshot(now), now)
	assert.Equal(t, rollout.EvidenceStatusFinalized, complete.Status)
	assert.True(t, complete.PostWindowFinalized)
	require.NotNil(t, complete.PostWindowFinalizedAt)
	assert.Equal(t, candidate.CompletedAt.Add(postWindow), *complete.PostWindowFinalizedAt)

	incompleteSnapshot := completeSnapshot(now)
	incompleteSnapshot.Members[1].PostHashrateHS = nil
	incomplete := buildSummary(candidate, incompleteSnapshot, now)
	assert.Equal(t, rollout.EvidenceStatusUnavailable, incomplete.Status)
	assert.True(t, incomplete.PostWindowFinalized)
}

func testCandidate(batchID int64, completedAt time.Time) Candidate {
	return Candidate{
		RolloutID:   uuid.New(),
		BatchID:     batchID,
		OrgID:       1,
		CompletedAt: completedAt,
		Status:      rollout.EvidenceStatusPending,
	}
}

func automaticCandidate(now time.Time) Candidate {
	candidate := testCandidate(1, now.Add(-time.Minute))
	candidate.PolicyEnabled = true
	candidate.MaxDropBasisPoints = 100
	candidate.HealthyDurationSeconds = 10
	candidate.RolloutState = rollout.StateReview
	candidate.RolloutRevision = 7
	candidate.RolloutCreatedByUserID = 9
	candidate.IsCurrentReviewBatch = true
	candidate.HasPendingBatch = true
	return candidate
}

func automaticHealthySnapshot(now time.Time) Snapshot {
	snapshot := completeSnapshot(now)
	snapshot.PolicyBuckets = []PolicyBucket{policyBucket(now, 0)}
	return snapshot
}

func policyBucket(boundary time.Time, delta int32) PolicyBucket {
	current := 100 * (1 + float64(delta)/10_000)
	return PolicyBucket{
		Boundary: boundary,
		Members: []BucketMember{
			{MemberID: 1, AvgHashrateHS: current, ObservedAt: boundary.Add(-time.Second)},
			{MemberID: 2, AvgHashrateHS: current * 2, ObservedAt: boundary.Add(-time.Second)},
		},
	}
}

func completeSnapshot(now time.Time) Snapshot {
	return Snapshot{Members: []MemberEvidence{
		{
			MemberID:           1,
			BaselineHashrateHS: float64Pointer(100),
			PostHashrateHS:     float64Pointer(90),
			PostObservedAt:     timePointer(now.Add(-time.Second)),
		},
		{
			MemberID:           2,
			BaselineHashrateHS: float64Pointer(200),
			PostHashrateHS:     float64Pointer(210),
			PostObservedAt:     timePointer(now.Add(-time.Second)),
		},
	}}
}

func float64Pointer(value float64) *float64  { return &value }
func int32Pointer(value int32) *int32        { return &value }
func int64Pointer(value int64) *int64        { return &value }
func timePointer(value time.Time) *time.Time { return &value }
func controlStatusPointer(value rollout.ControlStatus) *rollout.ControlStatus {
	return &value
}
