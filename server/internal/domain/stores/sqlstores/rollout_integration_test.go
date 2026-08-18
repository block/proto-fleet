package sqlstores_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/block/proto-fleet/server/generated/sqlc"
	channelDomain "github.com/block/proto-fleet/server/internal/domain/channel"
	rolloutDomain "github.com/block/proto-fleet/server/internal/domain/rollout"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/infrastructure/cryptohash"
)

func TestRolloutStoreFreezesBatchesAndReconstructsAfterRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 3)
	collectionStore := newCollectionStore(db)
	releaseSet := createTestReleaseSet(t, collectionStore, orgID, "frozen")
	source := createTestChannel(t, collectionStore, orgID, releaseSet.Id, "Source")
	_, err := collectionStore.AddDevicesToCollection(
		t.Context(),
		orgID,
		source.Id,
		deviceIdentifiers[:2],
	)
	require.NoError(t, err)

	store := sqlstores.NewSQLRolloutStore(db)
	created, err := store.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		"frozen-rollout",
		[][]string{{deviceIdentifiers[0]}, {deviceIdentifiers[1]}},
	))
	require.NoError(t, err)
	require.False(t, created.Replayed)
	require.Len(t, created.Rollout.Batches, 2)
	require.Equal(t, deviceIdentifiers[0], created.Rollout.Batches[0].Members[0].DeviceIdentifier)
	require.Equal(t, deviceIdentifiers[1], created.Rollout.Batches[1].Members[0].DeviceIdentifier)

	_, err = collectionStore.RemoveDevicesFromCollection(
		t.Context(),
		orgID,
		source.Id,
		[]string{deviceIdentifiers[0]},
	)
	require.NoError(t, err)
	_, err = collectionStore.AddDevicesToCollection(
		t.Context(),
		orgID,
		source.Id,
		[]string{deviceIdentifiers[2]},
	)
	require.NoError(t, err)

	restartedStore := sqlstores.NewSQLRolloutStore(db)
	reconstructed, err := restartedStore.Get(t.Context(), orgID, created.Rollout.ID)
	require.NoError(t, err)
	require.Len(t, reconstructed.Members, 2)
	assert.Equal(t, []string{deviceIdentifiers[0], deviceIdentifiers[1]}, []string{
		reconstructed.Members[0].DeviceIdentifier,
		reconstructed.Members[1].DeviceIdentifier,
	})
	assert.Equal(t, int32(0), reconstructed.Members[0].Position)
	assert.Equal(t, int32(1), reconstructed.Members[1].Position)

	_, err = restartedStore.Get(t.Context(), orgID+999, created.Rollout.ID)
	require.ErrorIs(t, err, rolloutDomain.ErrNotFound)
}

func TestRolloutStoreListHydratesRolloutDetails(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 1)
	store := sqlstores.NewSQLRolloutStore(db)
	created, err := store.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		"list-details",
		[][]string{{deviceIdentifiers[0]}},
	))
	require.NoError(t, err)

	now := time.Now().UTC()
	_, err = store.CaptureEvidence(t.Context(), rolloutDomain.EvidenceRequest{
		OrgID:       orgID,
		RolloutID:   created.Rollout.ID,
		Phase:       rolloutDomain.EvidencePhaseBaseline,
		WindowStart: now.Add(-time.Hour),
		WindowEnd:   now,
		FreshAfter:  now.Add(-5 * time.Minute),
	})
	require.NoError(t, err)

	listed, err := store.List(t.Context(), orgID, []rolloutDomain.State{
		rolloutDomain.StateCreated,
	})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, created.Rollout.ID, listed[0].ID)
	require.Len(t, listed[0].Batches, 1)
	require.Len(t, listed[0].Members, 1)
	require.Len(t, listed[0].Batches[0].Members, 1)
	require.Len(t, listed[0].Members[0].Evidence, 1)
	require.Len(t, listed[0].Causes, 1)
	assert.Equal(t, deviceIdentifiers[0], listed[0].Members[0].DeviceIdentifier)
	assert.Equal(t, "create", string(listed[0].Causes[0].Operation))
}

func TestRolloutStorePersistsAPIKeyControlAndCauseIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 1)
	store := sqlstores.NewSQLRolloutStore(db)
	credentialID := "apikey:rollout-key-77" //nolint:gosec // Opaque audit identifier, not a secret.
	createRequest := rolloutCreateRequest(
		t,
		db,
		orgID,
		"api-key-attribution",
		[][]string{{deviceIdentifiers[0]}},
	)
	createRequest.ActorType = rolloutDomain.ActorTypeAPIKey
	createRequest.ActorCredentialID = &credentialID
	created, err := store.Create(t.Context(), createRequest)
	require.NoError(t, err)

	controlRequest := rolloutControlRequest(
		created.Rollout,
		rolloutDomain.ControlOperationAdmit,
		"api-key-admit",
	)
	controlRequest.BatchID = created.Rollout.Batches[0].ID
	controlRequest.ActorType = rolloutDomain.ActorTypeAPIKey
	controlRequest.ActorCredentialID = &credentialID
	_, err = store.ApplyControl(t.Context(), controlRequest)
	require.NoError(t, err)

	var controlActorType string
	var controlCredentialID sql.NullString
	var controlUserID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT actor_type, actor_credential_id, created_by_user_id
		FROM firmware_rollout_control
		WHERE rollout_id = $1
		  AND operation = 'admit'
	`, created.Rollout.ID).Scan(
		&controlActorType,
		&controlCredentialID,
		&controlUserID,
	))
	assert.Equal(t, string(rolloutDomain.ActorTypeAPIKey), controlActorType)
	require.True(t, controlCredentialID.Valid)
	assert.Equal(t, credentialID, controlCredentialID.String)
	assert.Equal(t, created.Rollout.CreatedByUserID, controlUserID)

	var causeActorType string
	var causeCredentialID sql.NullString
	var causeUserID int64
	require.NoError(t, db.QueryRowContext(t.Context(), `
		SELECT actor_type, actor_credential_id, actor_user_id
		FROM firmware_rollout_cause
		WHERE rollout_id = $1
		  AND operation = 'admit'
	`, created.Rollout.ID).Scan(
		&causeActorType,
		&causeCredentialID,
		&causeUserID,
	))
	assert.Equal(t, string(rolloutDomain.ActorTypeAPIKey), causeActorType)
	require.True(t, causeCredentialID.Valid)
	assert.Equal(t, credentialID, causeCredentialID.String)
	assert.Equal(t, created.Rollout.CreatedByUserID, causeUserID)
}

func TestRolloutStoreEnforcesOneActiveOwnerPerMiner(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 1)
	store := sqlstores.NewSQLRolloutStore(db)
	_, err := store.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		"owner-one",
		[][]string{{deviceIdentifiers[0]}},
	))
	require.NoError(t, err)

	_, err = store.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		"owner-two",
		[][]string{{deviceIdentifiers[0]}},
	))
	require.ErrorIs(t, err, rolloutDomain.ErrOwnershipConflict)
}

func TestRolloutStoreRevisionIdempotencyAndAbortAuthorityBoundary(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 2)
	collectionStore := newCollectionStore(db)
	releaseSet := createTestReleaseSet(t, collectionStore, orgID, "abort-boundary")
	_, targetIDOne := enforcementFixtureIDs(
		t,
		db,
		deviceIdentifiers[0],
		releaseSet.Id,
	)
	deviceIDTwo, targetIDTwo := enforcementFixtureIDs(
		t,
		db,
		deviceIdentifiers[1],
		releaseSet.Id,
	)
	deviceIDOne, _ := enforcementFixtureIDs(
		t,
		db,
		deviceIdentifiers[0],
		releaseSet.Id,
	)

	store := sqlstores.NewSQLRolloutStore(db)
	created, err := store.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		"abort-boundary",
		[][]string{{deviceIdentifiers[0], deviceIdentifiers[1]}},
	))
	require.NoError(t, err)

	admitRequest := rolloutControlRequest(
		created.Rollout,
		rolloutDomain.ControlOperationAdmit,
		"admit-first",
	)
	admitRequest.BatchID = created.Rollout.Batches[0].ID
	admitted, err := store.ApplyControl(t.Context(), admitRequest)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.StateRunning, admitted.Rollout.State)
	assert.Equal(t, int64(2), admitted.Rollout.Revision)

	replay, err := store.ApplyControl(t.Context(), admitRequest)
	require.NoError(t, err)
	assert.True(t, replay.Replayed)
	assert.Equal(t, admitted.Control.ID, replay.Control.ID)

	stalePause := rolloutControlRequest(
		created.Rollout,
		rolloutDomain.ControlOperationPause,
		"stale-pause",
	)
	_, err = store.ApplyControl(t.Context(), stalePause)
	require.ErrorIs(t, err, rolloutDomain.ErrRevisionConflict)

	pauseRequest := rolloutControlRequest(
		admitted.Rollout,
		rolloutDomain.ControlOperationPause,
		"pause-running",
	)
	paused, err := store.ApplyControl(t.Context(), pauseRequest)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.StatePaused, paused.Rollout.State)

	invalidPause := rolloutControlRequest(
		paused.Rollout,
		rolloutDomain.ControlOperationPause,
		"pause-again",
	)
	_, err = store.ApplyControl(t.Context(), invalidPause)
	require.ErrorIs(t, err, rolloutDomain.ErrInvalidTransition)

	resumeRequest := rolloutControlRequest(
		paused.Rollout,
		rolloutDomain.ControlOperationResume,
		"resume-paused",
	)
	resumed, err := store.ApplyControl(t.Context(), resumeRequest)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.StateRunning, resumed.Rollout.State)

	enforcementStore := sqlstores.NewSQLChannelEnforcementStore(db)
	firstEnforcement, err := enforcementStore.CreateEnforcement(
		t.Context(),
		channelDomain.CreateEnforcementParams{
			OrgID:             orgID,
			DeviceID:          deviceIDOne,
			ReleaseTargetID:   targetIDOne,
			CauseType:         "rollout_admission",
			AuthorityID:       resumed.Rollout.ForwardAuthorityID,
			AuthorityRevision: resumed.Rollout.ForwardAuthorityRevision,
		},
	)
	require.NoError(t, err)
	secondEnforcement, err := enforcementStore.CreateEnforcement(
		t.Context(),
		channelDomain.CreateEnforcementParams{
			OrgID:             orgID,
			DeviceID:          deviceIDTwo,
			ReleaseTargetID:   targetIDTwo,
			CauseType:         "rollout_admission",
			AuthorityID:       resumed.Rollout.ForwardAuthorityID,
			AuthorityRevision: resumed.Rollout.ForwardAuthorityRevision,
		},
	)
	require.NoError(t, err)
	claimed, err := enforcementStore.Claim(
		t.Context(),
		firstEnforcement,
		"claimed-before-abort",
		time.Now(),
	)
	require.NoError(t, err)

	abortRequest := rolloutControlRequest(
		resumed.Rollout,
		rolloutDomain.ControlOperationAbort,
		"abort-running",
	)
	aborted, err := store.ApplyControl(t.Context(), abortRequest)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.StateAborted, aborted.Rollout.State)

	_, err = enforcementStore.Claim(
		t.Context(),
		secondEnforcement,
		"claim-after-abort",
		time.Now(),
	)
	require.ErrorIs(t, err, channelDomain.ErrCASConflict)
	persistedClaim, err := enforcementStore.GetEnforcement(t.Context(), claimed.ID)
	require.NoError(t, err)
	assert.Equal(t, channelDomain.EnforcementStateDispatching, persistedClaim.State)

	abortReplay, err := store.ApplyControl(t.Context(), abortRequest)
	require.NoError(t, err)
	assert.True(t, abortReplay.Replayed)
}

func TestRolloutEvidenceLeavesStaleAndMissingMetricsUnavailable(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 3)
	store := sqlstores.NewSQLRolloutStore(db)
	created, err := store.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		"evidence",
		[][]string{{deviceIdentifiers[0], deviceIdentifiers[1], deviceIdentifiers[2]}},
	))
	require.NoError(t, err)

	now := time.Now().UTC()
	q := sqlc.New(db)
	require.NoError(t, q.InsertDeviceMetrics(t.Context(), sqlc.InsertDeviceMetricsParams{
		Time:             now.Add(-time.Minute),
		DeviceIdentifier: deviceIdentifiers[0],
		HashRateHs:       sql.NullFloat64{Float64: 100, Valid: true},
		PowerW:           sql.NullFloat64{Float64: 3000, Valid: true},
		TempC:            sql.NullFloat64{Float64: 70, Valid: true},
	}))
	require.NoError(t, q.InsertDeviceMetrics(t.Context(), sqlc.InsertDeviceMetricsParams{
		Time:             now.Add(-30 * time.Minute),
		DeviceIdentifier: deviceIdentifiers[1],
		HashRateHs:       sql.NullFloat64{Float64: 90, Valid: true},
		PowerW:           sql.NullFloat64{Float64: 2800, Valid: true},
		TempC:            sql.NullFloat64{Float64: 68, Valid: true},
	}))

	for _, phase := range []rolloutDomain.EvidencePhase{
		rolloutDomain.EvidencePhaseBaseline,
		rolloutDomain.EvidencePhasePost,
	} {
		evidence, captureErr := store.CaptureEvidence(t.Context(), rolloutDomain.EvidenceRequest{
			OrgID:       orgID,
			RolloutID:   created.Rollout.ID,
			Phase:       phase,
			WindowStart: now.Add(-time.Hour),
			WindowEnd:   now,
			FreshAfter:  now.Add(-5 * time.Minute),
		})
		require.NoError(t, captureErr)
		require.Len(t, evidence, 3)

		byMember := make(map[int64]rolloutDomain.Evidence)
		for _, item := range evidence {
			byMember[item.MemberID] = item
		}
		fresh := byMember[created.Rollout.Members[0].ID]
		require.NotNil(t, fresh.ObservedAt)
		require.NotNil(t, fresh.AvgHashrateHS)
		require.NotNil(t, fresh.ErrorCount)
		assert.Equal(t, int64(0), *fresh.ErrorCount)

		stale := byMember[created.Rollout.Members[1].ID]
		assert.Nil(t, stale.ObservedAt)
		assert.Nil(t, stale.AvgHashrateHS)
		assert.Nil(t, stale.ErrorCount)
		assert.Nil(t, stale.SampleCount)

		missing := byMember[created.Rollout.Members[2].ID]
		assert.Nil(t, missing.ObservedAt)
		assert.Nil(t, missing.AvgHashrateHS)
		assert.Nil(t, missing.ErrorCount)
		assert.Nil(t, missing.SampleCount)
	}
}

func TestRolloutStoreUsesSeparateRevertAuthorityAndLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping database integration test in short mode")
	}

	db, orgID, deviceIdentifiers := setupCollectionTestData(t, 1)
	store := sqlstores.NewSQLRolloutStore(db)
	created, err := store.Create(t.Context(), rolloutCreateRequest(
		t,
		db,
		orgID,
		"revert-lifecycle",
		[][]string{{deviceIdentifiers[0]}},
	))
	require.NoError(t, err)

	admitRequest := rolloutControlRequest(
		created.Rollout,
		rolloutDomain.ControlOperationAdmit,
		"revert-admit",
	)
	admitRequest.BatchID = created.Rollout.Batches[0].ID
	admitted, err := store.ApplyControl(t.Context(), admitRequest)
	require.NoError(t, err)
	require.Len(t, admitted.Rollout.Members, 1)

	_, err = store.UpdateMember(t.Context(), rolloutDomain.MemberUpdateRequest{
		OrgID:            orgID,
		RolloutID:        admitted.Rollout.ID,
		MemberID:         admitted.Rollout.Members[0].ID,
		ExpectedRevision: admitted.Rollout.Members[0].Revision,
		State:            rolloutDomain.MemberStateSucceeded,
	})
	require.NoError(t, err)

	completed, err := store.ApplyControl(
		t.Context(),
		rolloutControlRequest(
			admitted.Rollout,
			rolloutDomain.ControlOperationComplete,
			"complete-forward",
		),
	)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.StateCompleted, completed.Rollout.State)

	reverting, err := store.ApplyControl(
		t.Context(),
		rolloutControlRequest(
			completed.Rollout,
			rolloutDomain.ControlOperationRevert,
			"start-revert",
		),
	)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.StateReverting, reverting.Rollout.State)
	require.NotNil(t, reverting.Rollout.RevertAuthorityID)
	assert.NotEqual(t, reverting.Rollout.ForwardAuthorityID, *reverting.Rollout.RevertAuthorityID)
	require.Len(t, reverting.Rollout.Members, 1)
	assert.Equal(t, rolloutDomain.MemberStateReverting, reverting.Rollout.Members[0].State)
	assert.Nil(t, reverting.Rollout.Members[0].OwnerReleasedAt)

	reverted, err := store.ApplyControl(
		t.Context(),
		rolloutControlRequest(
			reverting.Rollout,
			rolloutDomain.ControlOperationComplete,
			"complete-revert",
		),
	)
	require.NoError(t, err)
	assert.Equal(t, rolloutDomain.StateReverted, reverted.Rollout.State)
	require.Len(t, reverted.Rollout.Members, 1)
	assert.Equal(t, rolloutDomain.MemberStateReverted, reverted.Rollout.Members[0].State)
	require.NotNil(t, reverted.Rollout.Members[0].OwnerReleasedAt)
}

func rolloutCreateRequest(
	t *testing.T,
	db queryRower,
	orgID int64,
	key string,
	batches [][]string,
) rolloutDomain.CreateRequest {
	t.Helper()
	inputBatches := make([]rolloutDomain.CreateBatch, 0, len(batches))
	for batchIndex, members := range batches {
		input := rolloutDomain.CreateBatch{
			Label:   "batch-" + key + "-" + string(rune('a'+batchIndex)),
			Members: make([]rolloutDomain.CreateMember, 0, len(members)),
		}
		for _, identifier := range members {
			input.Members = append(input.Members, rolloutDomain.CreateMember{
				DeviceIdentifier: identifier,
				SourceSnapshot:   map[string]any{"firmware": "source"},
				TargetSnapshot:   map[string]any{"firmware": "target"},
				RevertSnapshot:   map[string]any{"firmware": "source"},
			})
		}
		inputBatches = append(inputBatches, input)
	}
	return rolloutDomain.CreateRequest{
		ID:                 uuid.New(),
		OrgID:              orgID,
		Name:               key,
		StrategyKey:        "integration",
		Batches:            inputBatches,
		IdempotencyKey:     key,
		RequestFingerprint: testFingerprint("create:" + key),
		Reason:             "integration test",
		ActorUserID:        testOrganizationUserID(t, db, orgID),
	}
}

func rolloutControlRequest(
	current *rolloutDomain.Rollout,
	operation rolloutDomain.ControlOperation,
	key string,
) rolloutDomain.ControlRequest {
	return rolloutDomain.ControlRequest{
		OrgID:              current.OrgID,
		RolloutID:          current.ID,
		ExpectedRevision:   current.Revision,
		Operation:          operation,
		IdempotencyKey:     key,
		RequestFingerprint: testFingerprint(string(operation) + ":" + key),
		Reason:             "integration test",
		ActorUserID:        current.CreatedByUserID,
	}
}

func testFingerprint(value string) string {
	return cryptohash.Sha256Hex(value)
}
