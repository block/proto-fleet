package sqlstores_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	rolloutEvidence "github.com/block/proto-fleet/server/internal/domain/rollout/evidence"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

type blockingSummaryStore struct {
	rolloutEvidence.Store
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingSummaryStore) UpdateSummary(
	ctx context.Context,
	summary rolloutEvidence.Summary,
) (bool, error) {
	s.once.Do(func() { close(s.reached) })
	select {
	case <-s.release:
	case <-ctx.Done():
		return false, fmt.Errorf("wait to update summary: %w", ctx.Err())
	}
	return s.Store.UpdateSummary(ctx, summary)
}

type recordingEvidenceController struct {
	mu       sync.Mutex
	requests []rolloutDomain.AdmitRequest
	err      error
	delegate *rolloutDomain.Service
}

func (c *recordingEvidenceController) Continue(
	ctx context.Context,
	req rolloutDomain.AdmitRequest,
) (*rolloutDomain.Rollout, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	if c.delegate != nil {
		result, err := c.delegate.Continue(ctx, req)
		if err != nil {
			return result, err
		}
		if c.err != nil {
			return result, c.err
		}
		return result, nil
	}
	return nil, c.err
}

func (c *recordingEvidenceController) requestCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func TestRolloutEvidenceRefreshesOnlyFrozenBatchWithEqualMemberWeighting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupCollectionTestData(t, 3)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	req := rolloutCreateRequest(
		t,
		db,
		orgID,
		"post-evidence-batch-scope",
		[][]string{{identifiers[0], identifiers[1]}, {identifiers[2]}},
	)
	req.HashratePolicy = &rolloutDomain.HashratePolicy{
		MaxDropBasisPoints:     100,
		HealthyDurationSeconds: 30,
	}
	created, err := rolloutStore.Create(t.Context(), req)
	require.NoError(t, err)

	completedAt := time.Now().UTC().Add(-15 * time.Second).Truncate(time.Microsecond)
	setRolloutAndBatchState(
		t,
		db,
		created.Rollout.ID.String(),
		created.Rollout.Batches[0].ID,
		"review",
		completedAt,
	)
	seedBaseline(t, db, created.Rollout.Batches[0].Members[0], completedAt, 100)
	seedBaseline(t, db, created.Rollout.Batches[0].Members[1], completedAt, 200)
	insertHashrate(t, db, identifiers[0], completedAt.Add(time.Second), 80)
	insertHashrate(t, db, identifiers[0], completedAt.Add(2*time.Second), 100)
	insertHashrate(t, db, identifiers[0], completedAt.Add(12*time.Second), 120)
	insertHashrate(t, db, identifiers[1], completedAt.Add(time.Second), 200)
	insertHashrate(t, db, identifiers[2], completedAt.Add(time.Second), 999)

	evaluator := rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 10},
		sqlstores.NewSQLRolloutEvidenceStore(db),
		nil,
	)
	evaluator.RunOnce(t.Context())

	persisted, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	first := persisted.Batches[0]
	assert.Equal(t, rolloutDomain.EvidenceStatusHeld, first.EvidenceStatus)
	assert.Equal(t, int64(2), first.EvidenceTotalCount)
	assert.Equal(t, int64(2), first.EvidencePairedCount)
	require.NotNil(t, first.CumulativeBaselineHashrateHS)
	require.NotNil(t, first.CumulativeCurrentHashrateHS)
	require.NotNil(t, first.CumulativeDeltaBasisPoints)
	assert.InDelta(t, 150, *first.CumulativeBaselineHashrateHS, 0.001)
	assert.InDelta(t, 150, *first.CumulativeCurrentHashrateHS, 0.001)
	assert.Equal(t, int32(0), *first.CumulativeDeltaBasisPoints)
	require.NotNil(t, first.LatestPolicyBucketHashrateHS)
	require.NotNil(t, first.LatestPolicyBucketDeltaBasisPoints)
	require.NotNil(t, first.LastPolicyBucketBoundary)
	assert.InDelta(t, 145, *first.LatestPolicyBucketHashrateHS, 0.001)
	assert.Equal(t, int32(-333), *first.LatestPolicyBucketDeltaBasisPoints)
	assert.True(t, first.LastPolicyBucketBoundary.Equal(completedAt.Add(10*time.Second)))

	assert.Equal(t, int64(3), postEvidenceSampleCount(t, first.Members[0]))
	assert.Equal(t, int64(1), postEvidenceSampleCount(t, first.Members[1]))

	insertHashrate(t, db, identifiers[0], completedAt.Add(14*time.Second), 150)
	evaluator.RunOnce(t.Context())
	refreshed, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	require.NotNil(t, refreshed.Batches[0].CumulativeCurrentHashrateHS)
	require.NotNil(t, refreshed.Batches[0].CumulativeDeltaBasisPoints)
	assert.InDelta(t, 156.25, *refreshed.Batches[0].CumulativeCurrentHashrateHS, 0.001)
	assert.Equal(t, int32(417), *refreshed.Batches[0].CumulativeDeltaBasisPoints)
	assert.Equal(t, int64(4), postEvidenceSampleCount(t, refreshed.Batches[0].Members[0]))
	assert.True(t, refreshed.Batches[0].LastPolicyBucketBoundary.Equal(completedAt.Add(10*time.Second)))

	var firstBatchPostCount int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM firmware_rollout_evidence evidence
		JOIN firmware_rollout_member member ON member.id = evidence.member_id
		WHERE member.batch_id = $1
		  AND evidence.phase = 'post'
	`, created.Rollout.Batches[0].ID).Scan(&firstBatchPostCount))
	assert.Equal(t, int64(2), firstBatchPostCount)

	var secondBatchPostCount int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM firmware_rollout_evidence evidence
		JOIN firmware_rollout_member member ON member.id = evidence.member_id
		WHERE member.batch_id = $1
		  AND evidence.phase = 'post'
	`, created.Rollout.Batches[1].ID).Scan(&secondBatchPostCount))
	assert.Zero(t, secondBatchPostCount)
}

func TestRolloutEvidenceSkipsPolicyBucketsForManualCandidates(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupCollectionTestData(t, 1)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	created, err := rolloutStore.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		"manual-evidence-without-policy-buckets",
		[][]string{{identifiers[0]}},
	))
	require.NoError(t, err)

	completedAt := time.Now().UTC().Add(-15 * time.Second).Truncate(time.Microsecond)
	setRolloutAndBatchState(
		t,
		db,
		created.Rollout.ID.String(),
		created.Rollout.Batches[0].ID,
		"review",
		completedAt,
	)
	seedBaseline(t, db, created.Rollout.Batches[0].Members[0], completedAt, 100)
	insertHashrate(t, db, identifiers[0], completedAt.Add(time.Second), 95)

	rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 10},
		sqlstores.NewSQLRolloutEvidenceStore(db),
		nil,
	).RunOnce(t.Context())

	persisted, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	batch := persisted.Batches[0]
	assert.Equal(t, rolloutDomain.EvidenceStatusObserving, batch.EvidenceStatus)
	require.NotNil(t, batch.CumulativeCurrentHashrateHS)
	assert.InDelta(t, 95, *batch.CumulativeCurrentHashrateHS, 0.001)
	assert.Nil(t, batch.LatestPolicyBucketHashrateHS)
	assert.Nil(t, batch.LatestPolicyBucketDeltaBasisPoints)
	assert.Nil(t, batch.LastPolicyBucketBoundary)
}

func TestRolloutEvidenceAutoContinueIsExactlyOnceThroughRealStrategy(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupRolloutLaneTestData(t, 2)
	actorID := testOrganizationUserID(t, db, orgID)
	laneStore := sqlstores.NewSQLRolloutLaneStore(db)
	laneService := betweenchannel.NewService(laneStore, nil)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	rolloutService := rolloutDomain.NewService(
		rolloutStore,
		betweenchannel.NewStrategy(laneStore),
	)
	lane, err := laneService.CreateLane(t.Context(), betweenchannel.CreateLaneRequest{
		OrgID:             orgID,
		Label:             "Evidence automatic lane",
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "a")},
		DeviceIdentifiers: identifiers,
		IdempotencyKey:    "evidence-automatic-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Evidence automatic rollout",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "b")},
		Batches: []rolloutDomain.CreateBatch{
			{Label: "canary", Members: []rolloutDomain.CreateMember{{DeviceIdentifier: identifiers[0]}}},
			{Label: "fleet", Members: []rolloutDomain.CreateMember{{DeviceIdentifier: identifiers[1]}}},
		},
		HashratePolicy: &rolloutDomain.HashratePolicy{
			MaxDropBasisPoints:     100,
			HealthyDurationSeconds: 10,
		},
		IdempotencyKey: "evidence-automatic-start",
		Reason:         "integration test",
		ActorUserID:    actorID,
	})
	require.NoError(t, err)
	_, err = rolloutService.Admit(t.Context(), rolloutDomain.AdmitRequest{
		OrgID:            orgID,
		RolloutID:        started.Rollout.ID,
		BatchID:          started.Rollout.Batches[0].ID,
		ExpectedRevision: started.Rollout.Revision,
		IdempotencyKey:   "evidence-automatic-admit",
		Reason:           "integration test",
		ActorUserID:      actorID,
	})
	require.NoError(t, err)
	admitted, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	canary := memberByIdentifier(t, admitted, identifiers[0])
	require.NotNil(t, canary.EnforcementID)
	confirmEnforcement(t, db, *canary.EnforcementID)
	finalizations, err := laneStore.ListFinalizations(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, finalizations, 1)
	_, err = laneStore.Finalize(t.Context(), finalizations[0])
	require.NoError(t, err)

	completedAt := time.Now().UTC().Add(-12 * time.Second).Truncate(time.Microsecond)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET completed_at = $2
		WHERE id = $1
	`, started.Rollout.Batches[0].ID, completedAt)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_evidence
		SET window_start = $2::timestamptz - INTERVAL '30 minutes',
		    window_end = $2::timestamptz,
		    observed_at = $2::timestamptz,
		    avg_hashrate_hs = 100,
		    sample_count = 1
		WHERE member_id = $1
		  AND phase = 'baseline'
	`, canary.ID, completedAt)
	require.NoError(t, err)
	insertHashrate(t, db, identifiers[0], completedAt.Add(5*time.Second), 100)

	evidenceStore := sqlstores.NewSQLRolloutEvidenceStore(db)
	candidates, err := evidenceStore.ListCandidates(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	candidate := candidates[0]
	assert.Equal(t, started.Rollout.Batches[0].ID, candidate.BatchID)
	assert.True(t, candidate.CompletedAt.Equal(completedAt))
	assert.True(t, candidate.PolicyEnabled)
	assert.Equal(t, int32(100), candidate.MaxDropBasisPoints)
	assert.Equal(t, int32(10), candidate.HealthyDurationSeconds)
	assert.Equal(t, rolloutDomain.StateReview, candidate.RolloutState)
	assert.True(t, candidate.IsCurrentReviewBatch)
	assert.True(t, candidate.HasPendingBatch)

	ambiguousController := &recordingEvidenceController{
		delegate: rolloutService,
		err:      errors.New("simulated response timeout"),
	}
	evaluator := rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 10},
		evidenceStore,
		ambiguousController,
	)
	var passes sync.WaitGroup
	passes.Add(2)
	for range 2 {
		go func() {
			defer passes.Done()
			evaluator.RunOnce(t.Context())
		}()
	}
	passes.Wait()
	requestsAfterAmbiguousResponse := ambiguousController.requestCount()
	evaluator.RunOnce(t.Context())

	persisted, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.StateRunning, persisted.State)
	assert.Equal(t, rolloutDomain.BatchStateAdmitted, persisted.Batches[1].State)
	assert.Equal(t, rolloutDomain.EvidenceStatusHealthy, persisted.Batches[0].EvidenceStatus)
	require.NotNil(t, persisted.Batches[0].HealthySince)
	assert.True(t, persisted.Batches[0].HealthySince.Equal(completedAt))
	require.NotNil(t, persisted.Batches[0].LastPolicyBucketBoundary)
	assert.True(
		t,
		persisted.Batches[0].LastPolicyBucketBoundary.Equal(completedAt.Add(10*time.Second)),
	)

	var controlCount, causeCount int64
	var actorType, controlStatus string
	var actorCredential sql.NullString
	var accountableUser int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*),
		       MIN(status),
		       MIN(actor_type),
		       MIN(actor_credential_id),
		       MIN(created_by_user_id)
		FROM firmware_rollout_control
		WHERE rollout_id = $1
		  AND idempotency_key = $2
	`, started.Rollout.ID, fmt.Sprintf(
		"rollout-evidence-auto-continue-batch-%d",
		started.Rollout.Batches[0].ID,
	)).Scan(
		&controlCount,
		&controlStatus,
		&actorType,
		&actorCredential,
		&accountableUser,
	))
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM firmware_rollout_cause
		WHERE rollout_id = $1
		  AND operation = 'continue'
	`, started.Rollout.ID).Scan(&causeCount))
	assert.Equal(t, int64(1), controlCount)
	assert.Equal(t, int64(1), causeCount)
	assert.Equal(t, string(rolloutDomain.ControlStatusSucceeded), controlStatus)
	assert.Equal(t, string(rolloutDomain.ActorTypeSystem), actorType)
	assert.False(t, actorCredential.Valid)
	assert.Equal(t, actorID, accountableUser)
	assert.Equal(t, requestsAfterAmbiguousResponse, ambiguousController.requestCount())
	assert.LessOrEqual(t, requestsAfterAmbiguousResponse, 2)
}

func TestRolloutEvidenceConcurrentSummaryCASRejectsStaleWriter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupCollectionTestData(t, 2)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	req := rolloutCreateRequest(
		t,
		db,
		orgID,
		"summary-cas-race",
		[][]string{{identifiers[0]}, {identifiers[1]}},
	)
	req.HashratePolicy = &rolloutDomain.HashratePolicy{
		MaxDropBasisPoints:     100,
		HealthyDurationSeconds: 10,
	}
	created, err := rolloutStore.Create(t.Context(), req)
	require.NoError(t, err)
	completedAt := time.Now().UTC().Add(-12 * time.Second).Truncate(time.Microsecond)
	setRolloutAndBatchState(
		t,
		db,
		created.Rollout.ID.String(),
		created.Rollout.Batches[0].ID,
		"review",
		completedAt,
	)
	seedBaseline(t, db, created.Rollout.Batches[0].Members[0], completedAt, 100)
	insertHashrate(t, db, identifiers[0], completedAt.Add(5*time.Second), 100)

	firstSQLStore := sqlstores.NewSQLRolloutEvidenceStore(db)
	blockedStore := &blockingSummaryStore{
		Store:   firstSQLStore,
		reached: make(chan struct{}),
		release: make(chan struct{}),
	}
	controller := &recordingEvidenceController{}
	olderEvaluator := rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 1},
		blockedStore,
		controller,
	)
	olderDone := make(chan struct{})
	go func() {
		defer close(olderDone)
		olderEvaluator.RunOnce(t.Context())
	}()

	select {
	case <-blockedStore.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("older evaluator did not reach summary update")
	}

	insertHashrate(t, db, identifiers[0], completedAt.Add(6*time.Second), 0)
	newerEvaluator := rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 1},
		sqlstores.NewSQLRolloutEvidenceStore(db),
		controller,
	)
	newerEvaluator.RunOnce(t.Context())
	close(blockedStore.release)
	select {
	case <-olderDone:
	case <-time.After(5 * time.Second):
		t.Fatal("older evaluator did not finish")
	}

	persisted, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	batch := persisted.Batches[0]
	assert.Equal(t, rolloutDomain.EvidenceStatusHeld, batch.EvidenceStatus)
	require.NotNil(t, batch.LatestPolicyBucketDeltaBasisPoints)
	assert.Less(t, *batch.LatestPolicyBucketDeltaBasisPoints, int32(-100))
	assert.Nil(t, batch.HealthySince)
	assert.Zero(t, controller.requestCount(), "lost CAS must suppress automatic continue")
}

func TestRolloutEvidencePersistsPartialAndMissingBaselineCoverage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupCollectionTestData(t, 3)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	created, err := rolloutStore.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		"partial-post-evidence",
		[][]string{{identifiers[0], identifiers[1]}, {identifiers[2]}},
	))
	require.NoError(t, err)
	completedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	for _, batch := range created.Rollout.Batches {
		setRolloutAndBatchState(
			t,
			db,
			created.Rollout.ID.String(),
			batch.ID,
			"review",
			completedAt,
		)
	}
	seedBaseline(t, db, created.Rollout.Batches[0].Members[0], completedAt, 100)
	seedBaseline(t, db, created.Rollout.Batches[0].Members[1], completedAt, 200)
	insertHashrate(t, db, identifiers[0], completedAt.Add(30*time.Second), 90)
	insertHashrate(t, db, identifiers[2], completedAt.Add(30*time.Second), 300)

	evaluator := rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 10},
		sqlstores.NewSQLRolloutEvidenceStore(db),
		nil,
	)
	evaluator.RunOnce(t.Context())

	persisted, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	partial := persisted.Batches[0]
	assert.Equal(t, rolloutDomain.EvidenceStatusStale, partial.EvidenceStatus)
	assert.Equal(t, int64(2), partial.EvidenceTotalCount)
	assert.Equal(t, int64(1), partial.EvidencePairedCount)

	missingBaseline := persisted.Batches[1]
	assert.Equal(t, rolloutDomain.EvidenceStatusUnavailable, missingBaseline.EvidenceStatus)
	assert.Equal(t, int64(1), missingBaseline.EvidenceTotalCount)
	assert.Zero(t, missingBaseline.EvidencePairedCount)
}

func TestRolloutEvidenceFinalizesCompletedRolloutWindowsAndStopsSelectingThem(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupCollectionTestData(t, 2)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	created, err := rolloutStore.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		"final-post-evidence",
		[][]string{{identifiers[0]}, {identifiers[1]}},
	))
	require.NoError(t, err)
	completedAt := time.Now().UTC().Add(-31 * time.Minute).Truncate(time.Microsecond)
	for _, batch := range created.Rollout.Batches {
		setRolloutAndBatchState(
			t,
			db,
			created.Rollout.ID.String(),
			batch.ID,
			"completed",
			completedAt,
		)
	}
	seedBaseline(t, db, created.Rollout.Batches[0].Members[0], completedAt, 100)
	seedBaseline(t, db, created.Rollout.Batches[1].Members[0], completedAt, 200)
	insertHashrate(t, db, identifiers[0], completedAt.Add(5*time.Minute), 95)

	store := sqlstores.NewSQLRolloutEvidenceStore(db)
	candidates, err := store.ListCandidates(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, candidates, 2, "final completed rollouts remain candidates until finalization")

	rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 10},
		store,
		nil,
	).RunOnce(t.Context())

	persisted, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.EvidenceStatusFinalized, persisted.Batches[0].EvidenceStatus)
	assert.True(t, persisted.Batches[0].PostWindowFinalized)
	assert.Equal(t, rolloutDomain.EvidenceStatusUnavailable, persisted.Batches[1].EvidenceStatus)
	assert.True(t, persisted.Batches[1].PostWindowFinalized)
	for _, batch := range persisted.Batches {
		require.NotNil(t, batch.PostWindowFinalizedAt)
		assert.True(t, batch.PostWindowFinalizedAt.Equal(completedAt.Add(30*time.Minute)))
	}

	restartedStore := sqlstores.NewSQLRolloutEvidenceStore(db)
	candidates, err = restartedStore.ListCandidates(t.Context(), 10)
	require.NoError(t, err)
	assert.Empty(t, candidates)
}

func TestRolloutEvidenceProcessesClosingPolicyBucketBeforeFinalization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupCollectionTestData(t, 2)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	tests := []struct {
		name       string
		hashrate   float64
		wantStatus rolloutDomain.EvidenceStatus
	}{
		{name: "full 1800 second dwell", hashrate: 100, wantStatus: rolloutDomain.EvidenceStatusHealthy},
		{name: "closing violation", hashrate: 90, wantStatus: rolloutDomain.EvidenceStatusHeld},
	}
	for index, test := range tests {
		created, err := rolloutStore.Create(t.Context(), func() rolloutDomain.CreateRequest {
			req := rolloutCreateRequest(
				t,
				db,
				orgID,
				"closing-policy-bucket-"+test.name,
				[][]string{{identifiers[index]}},
			)
			req.HashratePolicy = &rolloutDomain.HashratePolicy{
				MaxDropBasisPoints:     100,
				HealthyDurationSeconds: 1800,
			}
			return req
		}())
		require.NoError(t, err)
		completedAt := time.Now().UTC().
			Add(-30*time.Minute - 100*time.Millisecond).
			Truncate(time.Microsecond)
		windowEnd := completedAt.Add(30 * time.Minute)
		setRolloutAndBatchState(
			t,
			db,
			created.Rollout.ID.String(),
			created.Rollout.Batches[0].ID,
			"completed",
			completedAt,
		)
		_, err = db.ExecContext(t.Context(), `
			UPDATE firmware_rollout_batch
			SET evidence_status = 'observing',
			    healthy_since = $2,
			    last_policy_bucket_boundary = $3,
			    evaluated_at = $4
			WHERE id = $1
		`,
			created.Rollout.Batches[0].ID,
			completedAt,
			windowEnd.Add(-10*time.Second),
			windowEnd.Add(-5*time.Second),
		)
		require.NoError(t, err)
		seedBaseline(t, db, created.Rollout.Batches[0].Members[0], completedAt, 100)
		insertHashrate(t, db, identifiers[index], windowEnd.Add(-5*time.Second), test.hashrate)
	}

	rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 10},
		sqlstores.NewSQLRolloutEvidenceStore(db),
		nil,
	).RunOnce(t.Context())

	rollouts, err := rolloutStore.List(t.Context(), orgID, []rolloutDomain.State{
		rolloutDomain.StateCompleted,
	})
	require.NoError(t, err)
	require.Len(t, rollouts, len(tests))
	statuses := make(map[string]rolloutDomain.Batch)
	for _, item := range rollouts {
		statuses[item.Name] = item.Batches[0]
	}
	for _, test := range tests {
		batch := statuses["closing-policy-bucket-"+test.name]
		assert.Equal(t, test.wantStatus, batch.EvidenceStatus)
		assert.True(t, batch.PostWindowFinalized)
		require.NotNil(t, batch.LastPolicyBucketBoundary)
		require.NotNil(t, batch.PostWindowFinalizedAt)
		assert.True(t, batch.LastPolicyBucketBoundary.Equal(*batch.PostWindowFinalizedAt))
	}
}

func TestRolloutEvidenceOutageCheckpointsBucketsAndRequiresFutureDwell(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupCollectionTestData(t, 1)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	req := rolloutCreateRequest(
		t,
		db,
		orgID,
		"evaluator-outage-checkpoint",
		[][]string{{identifiers[0]}},
	)
	req.HashratePolicy = &rolloutDomain.HashratePolicy{
		MaxDropBasisPoints:     100,
		HealthyDurationSeconds: 20,
	}
	created, err := rolloutStore.Create(t.Context(), req)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	completedAt := now.Add(-55 * time.Second)
	setRolloutAndBatchState(
		t,
		db,
		created.Rollout.ID.String(),
		created.Rollout.Batches[0].ID,
		"review",
		completedAt,
	)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET evidence_status = 'observing',
		    healthy_since = $2,
		    last_policy_bucket_boundary = $3,
		    evaluated_at = $4
		WHERE id = $1
	`,
		created.Rollout.Batches[0].ID,
		completedAt,
		completedAt.Add(10*time.Second),
		now.Add(-30*time.Second),
	)
	require.NoError(t, err)
	seedBaseline(t, db, created.Rollout.Batches[0].Members[0], completedAt, 100)
	for _, offset := range []int{15, 25, 35} {
		insertHashrate(
			t,
			db,
			identifiers[0],
			completedAt.Add(time.Duration(offset)*time.Second),
			100,
		)
	}
	evaluator := rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 10},
		sqlstores.NewSQLRolloutEvidenceStore(db),
		nil,
	)

	evaluator.RunOnce(t.Context())
	afterOutage, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	batch := afterOutage.Batches[0]
	assert.Equal(t, rolloutDomain.EvidenceStatusStale, batch.EvidenceStatus)
	assert.Nil(t, batch.HealthySince)
	require.NotNil(t, batch.LastPolicyBucketBoundary)
	assert.True(t, batch.LastPolicyBucketBoundary.Equal(completedAt.Add(40*time.Second)))

	evaluator.RunOnce(t.Context())
	withoutFutureBucket, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(
		t,
		rolloutDomain.EvidenceStatusStale,
		withoutFutureBucket.Batches[0].EvidenceStatus,
	)
	assert.Nil(t, withoutFutureBucket.Batches[0].HealthySince)

	insertHashrate(t, db, identifiers[0], completedAt.Add(45*time.Second), 100)
	evaluator.RunOnce(t.Context())
	recovered, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	batch = recovered.Batches[0]
	assert.Equal(t, rolloutDomain.EvidenceStatusObserving, batch.EvidenceStatus)
	require.NotNil(t, batch.HealthySince)
	assert.True(t, batch.HealthySince.Equal(completedAt.Add(40*time.Second)))
	require.NotNil(t, batch.LastPolicyBucketBoundary)
	assert.True(t, batch.LastPolicyBucketBoundary.Equal(completedAt.Add(50*time.Second)))
}

func TestRolloutEvidenceRejectsNonFinitePersistedTelemetry(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupCollectionTestData(t, 2)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	tests := []struct {
		name             string
		baselineHashrate float64
		postHashrate     float64
		wantStatus       rolloutDomain.EvidenceStatus
	}{
		{
			name:             "non-finite baseline",
			baselineHashrate: math.NaN(),
			postHashrate:     100,
			wantStatus:       rolloutDomain.EvidenceStatusUnavailable,
		},
		{
			name:             "non-finite post and bucket",
			baselineHashrate: 100,
			postHashrate:     math.Inf(1),
			wantStatus:       rolloutDomain.EvidenceStatusStale,
		},
	}
	rolloutIDs := make(map[string]uuid.UUID)
	for index, test := range tests {
		req := rolloutCreateRequest(
			t,
			db,
			orgID,
			"invalid-telemetry-"+test.name,
			[][]string{{identifiers[index]}},
		)
		req.HashratePolicy = &rolloutDomain.HashratePolicy{
			MaxDropBasisPoints:     100,
			HealthyDurationSeconds: 10,
		}
		created, err := rolloutStore.Create(t.Context(), req)
		require.NoError(t, err)
		completedAt := time.Now().UTC().Add(-21 * time.Second).Truncate(time.Microsecond)
		setRolloutAndBatchState(
			t,
			db,
			created.Rollout.ID.String(),
			created.Rollout.Batches[0].ID,
			"review",
			completedAt,
		)
		seedBaseline(
			t,
			db,
			created.Rollout.Batches[0].Members[0],
			completedAt,
			test.baselineHashrate,
		)
		insertHashrate(t, db, identifiers[index], time.Now().UTC().Add(-time.Second), test.postHashrate)
		rolloutIDs[test.name] = created.Rollout.ID
	}

	rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 10},
		sqlstores.NewSQLRolloutEvidenceStore(db),
		nil,
	).RunOnce(t.Context())

	for _, test := range tests {
		persisted, err := rolloutStore.Get(t.Context(), orgID, rolloutIDs[test.name])
		require.NoError(t, err)
		batch := persisted.Batches[0]
		assert.Equal(t, test.wantStatus, batch.EvidenceStatus)
		assert.NotEqual(t, rolloutDomain.EvidenceStatusHealthy, batch.EvidenceStatus)
		assert.Nil(t, batch.HealthySince)
	}
}

func TestRolloutEvidenceAutomationErrorStillRefreshesAndFinalizes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupCollectionTestData(t, 1)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	req := rolloutCreateRequest(
		t,
		db,
		orgID,
		"automation-error-evidence-refresh",
		[][]string{{identifiers[0]}},
	)
	req.HashratePolicy = &rolloutDomain.HashratePolicy{
		MaxDropBasisPoints:     100,
		HealthyDurationSeconds: 10,
	}
	created, err := rolloutStore.Create(t.Context(), req)
	require.NoError(t, err)
	completedAt := time.Now().UTC().Add(-31 * time.Minute).Truncate(time.Microsecond)
	setRolloutAndBatchState(
		t,
		db,
		created.Rollout.ID.String(),
		created.Rollout.Batches[0].ID,
		"review",
		completedAt,
	)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET evidence_status = 'automation_error',
		    evidence_error_message = 'operator-safe failure'
		WHERE id = $1
	`, created.Rollout.Batches[0].ID)
	require.NoError(t, err)
	seedBaseline(t, db, created.Rollout.Batches[0].Members[0], completedAt, 100)
	insertHashrate(t, db, identifiers[0], completedAt.Add(29*time.Minute), 95)

	store := sqlstores.NewSQLRolloutEvidenceStore(db)
	candidates, err := store.ListCandidates(t.Context(), 10)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	controller := &recordingEvidenceController{}
	rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 10},
		store,
		controller,
	).RunOnce(t.Context())

	persisted, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	batch := persisted.Batches[0]
	assert.Equal(t, rolloutDomain.EvidenceStatusAutomationError, batch.EvidenceStatus)
	assert.True(t, batch.PostWindowFinalized)
	require.NotNil(t, batch.CumulativeCurrentHashrateHS)
	assert.InDelta(t, 95, *batch.CumulativeCurrentHashrateHS, 0.001)
	assert.Zero(t, controller.requestCount())
	candidates, err = store.ListCandidates(t.Context(), 10)
	require.NoError(t, err)
	assert.Empty(t, candidates)
}

func TestRolloutEvidenceCandidatesAreBoundedStateAwareAndRestartSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, identifiers := setupCollectionTestData(t, 8)
	rolloutStore := sqlstores.NewSQLRolloutStore(db)
	baseCompletedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	validStates := []string{
		"running",
		"paused",
		"review",
		"completed",
		"completed_with_failures",
	}
	validBatchIDs := make(map[int64]struct{}, len(validStates))
	for i, state := range validStates {
		created, err := rolloutStore.Create(t.Context(), rolloutCreateRequest(
			t,
			db,
			orgID,
			fmt.Sprintf("candidate-%d", i),
			[][]string{{identifiers[i]}},
		))
		require.NoError(t, err)
		completedAt := baseCompletedAt.Add(time.Duration(i) * time.Second)
		setRolloutAndBatchState(
			t,
			db,
			created.Rollout.ID.String(),
			created.Rollout.Batches[0].ID,
			state,
			completedAt,
		)
		validBatchIDs[created.Rollout.Batches[0].ID] = struct{}{}
	}

	legacy := createCandidateFixture(t, db, rolloutStore, orgID, identifiers[5], "legacy-null")
	_, err := db.ExecContext(t.Context(), `
		UPDATE firmware_rollout SET state = 'completed' WHERE id = $1
	`, legacy.Rollout.ID)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET state = 'completed', completed_at = NULL
		WHERE id = $1
	`, legacy.Rollout.Batches[0].ID)
	require.NoError(t, err)

	finalized := createCandidateFixture(t, db, rolloutStore, orgID, identifiers[6], "already-finalized")
	setRolloutAndBatchState(
		t,
		db,
		finalized.Rollout.ID.String(),
		finalized.Rollout.Batches[0].ID,
		"completed",
		baseCompletedAt,
	)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET evidence_status = 'finalized',
		    post_window_finalized = TRUE,
		    post_window_finalized_at = $2
		WHERE id = $1
	`, finalized.Rollout.Batches[0].ID, baseCompletedAt.Add(30*time.Minute))
	require.NoError(t, err)

	automationError := createCandidateFixture(
		t,
		db,
		rolloutStore,
		orgID,
		identifiers[7],
		"automation-error",
	)
	setRolloutAndBatchState(
		t,
		db,
		automationError.Rollout.ID.String(),
		automationError.Rollout.Batches[0].ID,
		"review",
		baseCompletedAt,
	)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET evidence_status = 'automation_error'
		WHERE id = $1
	`, automationError.Rollout.Batches[0].ID)
	require.NoError(t, err)
	validBatchIDs[automationError.Rollout.Batches[0].ID] = struct{}{}

	store := sqlstores.NewSQLRolloutEvidenceStore(db)
	bounded, err := store.ListCandidates(t.Context(), 3)
	require.NoError(t, err)
	require.Len(t, bounded, 3)

	restartedStore := sqlstores.NewSQLRolloutEvidenceStore(db)
	reconstructed, err := restartedStore.ListCandidates(t.Context(), 20)
	require.NoError(t, err)
	require.Len(t, reconstructed, len(validBatchIDs))
	for _, candidate := range reconstructed {
		_, expected := validBatchIDs[candidate.BatchID]
		assert.True(t, expected, "unexpected candidate batch %d", candidate.BatchID)
	}

	evaluator := rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 2},
		restartedStore,
		nil,
	)
	for range 3 {
		evaluator.RunOnce(t.Context())
	}
	for batchID := range validBatchIDs {
		var evaluatedAt sql.NullTime
		require.NoError(t, db.QueryRowContext(t.Context(), `
			SELECT evaluated_at
			FROM firmware_rollout_batch
			WHERE id = $1
		`, batchID).Scan(&evaluatedAt))
		assert.True(t, evaluatedAt.Valid, "candidate batch %d was starved", batchID)
	}
}

func createCandidateFixture(
	t *testing.T,
	db *sql.DB,
	store *sqlstores.SQLRolloutStore,
	orgID int64,
	identifier string,
	name string,
) rolloutDomain.CreateResult {
	t.Helper()
	created, err := store.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		name,
		[][]string{{identifier}},
	))
	require.NoError(t, err)
	return created
}

func setRolloutAndBatchState(
	t *testing.T,
	db *sql.DB,
	rolloutID string,
	batchID int64,
	rolloutState string,
	completedAt time.Time,
) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		UPDATE firmware_rollout SET state = $2 WHERE id = $1
	`, rolloutID, rolloutState)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		UPDATE firmware_rollout_batch
		SET state = 'completed', completed_at = $2
		WHERE rollout_id = $1 AND id = $3
	`, rolloutID, completedAt, batchID)
	require.NoError(t, err)
}

func seedBaseline(
	t *testing.T,
	db *sql.DB,
	member rolloutDomain.Member,
	completedAt time.Time,
	hashrate float64,
) {
	t.Helper()
	_, err := db.ExecContext(t.Context(), `
		INSERT INTO firmware_rollout_evidence (
		    rollout_id,
		    member_id,
		    org_id,
		    phase,
		    window_start,
		    window_end,
		    observed_at,
		    avg_hashrate_hs,
		    sample_count
		)
		VALUES ($1, $2, $3, 'baseline', $4, $5, $5, $6, 1)
	`, member.RolloutID, member.ID, member.OrgID, completedAt.Add(-30*time.Minute), completedAt, hashrate)
	require.NoError(t, err)
}

func insertHashrate(
	t *testing.T,
	db *sql.DB,
	identifier string,
	observedAt time.Time,
	hashrate float64,
) {
	t.Helper()
	require.NoError(t, sqlc.New(db).InsertDeviceMetrics(
		t.Context(),
		sqlc.InsertDeviceMetricsParams{
			Time:             observedAt,
			DeviceIdentifier: identifier,
			HashRateHs: sql.NullFloat64{
				Float64: hashrate,
				Valid:   true,
			},
		},
	))
}

func postEvidenceSampleCount(t *testing.T, member rolloutDomain.Member) int64 {
	t.Helper()
	for _, item := range member.Evidence {
		if item.Phase == rolloutDomain.EvidencePhasePost {
			require.NotNil(t, item.SampleCount)
			return *item.SampleCount
		}
	}
	t.Fatalf("post evidence not found for member %d", member.ID)
	return 0
}
