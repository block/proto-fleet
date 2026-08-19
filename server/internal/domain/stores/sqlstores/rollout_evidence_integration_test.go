package sqlstores_test

import (
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/rollout/betweenchannel"
	rolloutEvidence "github.com/block/proto-fleet/server/internal/domain/rollout/evidence"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
)

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
	)
	evaluator.RunOnce(t.Context())

	persisted, err := rolloutStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	first := persisted.Batches[0]
	assert.Equal(t, rolloutDomain.EvidenceStatusObserving, first.EvidenceStatus)
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
		ReleaseTargets:    []betweenchannel.ReleaseTarget{testLaneTarget("1.0.0", "evidence-source")},
		DeviceIdentifiers: identifiers,
		IdempotencyKey:    "evidence-automatic-lane",
		ActorUserID:       actorID,
	})
	require.NoError(t, err)
	started, err := laneService.StartRollout(t.Context(), betweenchannel.StartRolloutRequest{
		OrgID:          orgID,
		LaneID:         lane.ID,
		Name:           "Evidence automatic rollout",
		ReleaseTargets: []betweenchannel.ReleaseTarget{testLaneTarget("2.0.0", "evidence-target")},
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
		SET window_start = $2 - INTERVAL '30 minutes',
		    window_end = $2,
		    observed_at = $2,
		    avg_hashrate_hs = 100,
		    sample_count = 1
		WHERE member_id = $1
		  AND phase = 'baseline'
	`, canary.ID, completedAt)
	require.NoError(t, err)
	insertHashrate(t, db, identifiers[0], completedAt.Add(5*time.Second), 100)

	evaluator := rolloutEvidence.NewEvaluator(
		rolloutEvidence.Config{BatchSize: 10},
		sqlstores.NewSQLRolloutEvidenceStore(db),
		rolloutService,
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

	persisted, err := rolloutService.Get(t.Context(), orgID, started.Rollout.ID)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.StateRunning, persisted.State)
	assert.Equal(t, rolloutDomain.BatchStateAdmitted, persisted.Batches[1].State)

	var controlCount, causeCount int64
	var actorType string
	var actorCredential sql.NullString
	var accountableUser int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*), MIN(actor_type), MIN(actor_credential_id), MIN(created_by_user_id)
		FROM firmware_rollout_control
		WHERE rollout_id = $1
		  AND idempotency_key = $2
	`, started.Rollout.ID, fmt.Sprintf(
		"rollout-evidence-auto-continue-batch-%d",
		started.Rollout.Batches[0].ID,
	)).Scan(&controlCount, &actorType, &actorCredential, &accountableUser))
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT COUNT(*)
		FROM firmware_rollout_cause
		WHERE rollout_id = $1
		  AND operation = 'continue'
	`, started.Rollout.ID).Scan(&causeCount))
	assert.Equal(t, int64(1), controlCount)
	assert.Equal(t, int64(1), causeCount)
	assert.Equal(t, string(rolloutDomain.ActorTypeSystem), actorType)
	assert.False(t, actorCredential.Valid)
	assert.Equal(t, actorID, accountableUser)
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

	store := sqlstores.NewSQLRolloutEvidenceStore(db)
	bounded, err := store.ListCandidates(t.Context(), 3)
	require.NoError(t, err)
	require.Len(t, bounded, 3)

	restartedStore := sqlstores.NewSQLRolloutEvidenceStore(db)
	reconstructed, err := restartedStore.ListCandidates(t.Context(), 20)
	require.NoError(t, err)
	require.Len(t, reconstructed, len(validStates))
	for _, candidate := range reconstructed {
		_, expected := validBatchIDs[candidate.BatchID]
		assert.True(t, expected, "unexpected candidate batch %d", candidate.BatchID)
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
